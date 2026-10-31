"""Dependency-free archive behavior tests; native compatibility is tested separately.

Run: TMPDIR=/tmp python3 -m unittest discover -s internal/inspection -p '*_test.py'
"""

import asyncio
import errno
import importlib.util
import io
import json
import os
from pathlib import Path
import sys
import tempfile
import time
from types import SimpleNamespace
import unittest
from unittest import mock


class Headers(dict):
    def items(self, multi=False):
        return super().items()


class FlowWriter:
    def __init__(self, output):
        self.output = output

    def add(self, flow):
        self.output.write(flow.payload)


def load_addon():
    ctx = SimpleNamespace(options=SimpleNamespace(), log=SimpleNamespace(warning=mock.Mock()),
                          master=SimpleNamespace(commands=SimpleNamespace(call=mock.Mock())))
    package = SimpleNamespace(ctx=ctx, http=SimpleNamespace(HTTPFlow=object),
                              io=SimpleNamespace(FlowWriter=FlowWriter))
    spec = importlib.util.spec_from_file_location("archive_addon_under_test", Path(__file__).with_name("addon.py"))
    module = importlib.util.module_from_spec(spec)
    with mock.patch.dict(sys.modules, {"mitmproxy": package}):
        spec.loader.exec_module(module)
    return module


addon = load_addon()
MAC = "02:00:00:00:00:01"
OTHER = "02:00:00:00:00:02"


def message(body=b"body", content_type="text/plain; charset=utf-8"):
    return SimpleNamespace(raw_content=body, headers=Headers({"content-type": content_type}),
                           timestamp_start=time.time(), method="POST", pretty_url="https://example.test/path",
                           host="example.test", status_code=200)


def flow(payload=b"native\x00request\x00response", body=b"request", response_body=b"response"):
    return SimpleNamespace(metadata={}, request=message(body), response=message(response_body),
                           websocket=None, error=None, payload=payload,
                           client_conn=SimpleNamespace(peername=("192.0.2.2", 1234)),
                           server_conn=SimpleNamespace(address=("192.0.2.3", 443)))


class ArchiveTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="olr-addon-", dir="/tmp")
        self.addCleanup(self.temporary.cleanup)
        self.directory = Path(self.temporary.name)
        addon.ctx.options = SimpleNamespace(confdir=str(self.directory), olr_device=MAC,
                                            olr_retention_days=30, olr_max_bytes=5*1024**3)
        addon._retained.clear()
        addon._archive = None
        addon._archive_gate = asyncio.Lock()
        addon._last_archive_failure = ""
        addon._archive_failure_count = 0
        addon.ctx.log.warning.reset_mock()

    def archive(self, mac=MAC, size=5*1024**3):
        return addon.HistoryArchive(self.directory, mac, 30, size)

    def seed(self, mac, at, index, size=20):
        directory = self.directory / "history" / mac
        directory.mkdir(parents=True, exist_ok=True)
        identifier = f"{int(at * 1_000_000):020d}-{index:032x}"
        (directory / (identifier + ".mitm")).write_bytes(b"x" * size)
        (directory / (identifier + ".json")).write_text("{}")
        (directory / (identifier + ".meta")).write_text("{}")
        return identifier

    def test_atomic_native_bytes_private_modes_and_previews(self):
        archive = self.archive()
        item = flow(payload=b"complete-native-body\x00\xff", body=b"a" * (64*1024 + 1), response_body=b"\x00\xff")
        archive.persist(item)
        identifier = item.metadata["olr_history_id"]
        native = archive.directory / (identifier + ".mitm")
        metadata = archive.directory / (identifier + ".json")
        self.assertEqual(native.read_bytes(), item.payload)
        entry = json.loads(metadata.read_text())
        self.assertEqual(entry["bytes"], len(item.payload))
        self.assertEqual(len(entry["request"]["body"]), 64*1024)
        self.assertTrue(entry["request"]["body_truncated"])
        self.assertTrue(entry["response"]["body_binary"])
        self.assertEqual(native.stat().st_mode & 0o777, 0o600)
        self.assertEqual(metadata.stat().st_mode & 0o777, 0o600)
        self.assertEqual(archive.directory.stat().st_mode & 0o777, 0o700)
        self.assertFalse(list(archive.directory.glob("*.tmp")))

    def test_global_cap_oldest_first_and_expiry_across_devices(self):
        now = time.time()
        expired = self.seed(OTHER, now - 40*86400, 1)
        oldest = self.seed(OTHER, now - 60, 2, 600*1024)
        newest = self.seed(MAC, now - 10, 3, 600*1024)
        (self.directory / "retention.json").write_text(json.dumps({"days": 30, "max_bytes": 1024**2}))
        archive = self.archive()
        self.assertFalse((archive.root / OTHER / (expired + ".mitm")).exists())
        self.assertFalse((archive.root / OTHER / (oldest + ".mitm")).exists())
        self.assertTrue((archive.root / MAC / (newest + ".mitm")).exists())
        self.assertLessEqual(archive.sweep(), 1024**2)

    def test_dynamic_policy_and_startup_recovery(self):
        archive = self.archive()
        item = flow()
        archive.persist(item)
        identifier = item.metadata["olr_history_id"]
        orphan = archive.directory / ("00000000000000000001-" + "a" * 32 + ".mitm")
        orphan.write_bytes(b"orphan")
        (archive.directory / ".leftover.tmp").write_bytes(b"partial")
        archive.sweep(startup=True)
        self.assertFalse(orphan.exists())
        self.assertFalse((archive.directory / ".leftover.tmp").exists())
        (self.directory / "retention.json").write_text(json.dumps({"days": 1, "max_bytes": 1024**2}))
        archive.sweep()
        self.assertEqual((archive.retention_days, archive.max_bytes), (1, 1024**2))
        self.assertTrue((archive.directory / (identifier + ".mitm")).exists())
        (self.directory / "retention.json").write_text('{"days":0,"max_bytes":1}')
        with self.assertRaises(ValueError):
            archive.sweep()

    def test_disk_full_and_oversized_flows_do_not_escape_hooks(self):
        addon._archive = self.archive()
        with mock.patch.object(addon.io, "FlowWriter", side_effect=OSError(errno.ENOSPC, "disk full")):
            with mock.patch("sys.stdout", new=io.StringIO()):
                asyncio.run(addon.response(flow()))
        status = json.loads((self.directory / "archive-status.json").read_text())
        self.assertIn("disk full", status["error"])
        self.assertFalse(list(addon._archive.directory.glob("*.mitm")))
        self.assertFalse(list(addon._archive.directory.glob("*.tmp")))
        self.assertEqual(len(addon._retained), 1)
        tiny = self.archive(size=1000)
        with self.assertRaises(addon.ArchiveFull):
            tiny.persist(flow(body=b"x"*1001))
        self.assertFalse(list(tiny.directory.glob("*.mitm")))
        addon._archive = self.archive()
        with mock.patch("sys.stdout", new=io.StringIO()):
            asyncio.run(addon.response(flow()))
        self.assertEqual(json.loads((self.directory / "archive-status.json").read_text())["error"], "")

    def test_websocket_rewrite_failure_preserves_committed_snapshot(self):
        archive = self.archive()
        item = flow(payload=b"durable-handshake")
        archive.persist(item)
        identifier = item.metadata["olr_history_id"]
        paths = [archive.directory / (identifier + suffix) for suffix in (".mitm", ".json", ".meta")]
        previous = [path.read_bytes() for path in paths]
        item.websocket = SimpleNamespace(messages=[SimpleNamespace(content=b"later")])
        item.payload = b"replacement-with-websocket-messages"
        atomic = addon._atomic_json
        for failed_suffix in (".json", ".meta"):
            def fail_sidecar(path, value):
                if path.suffix == failed_suffix:
                    raise OSError(errno.ENOSPC, "sidecar storage full")
                return atomic(path, value)
            with self.subTest(failed_suffix=failed_suffix):
                with mock.patch.object(addon, "_atomic_json", side_effect=fail_sidecar):
                    with self.assertRaises(OSError):
                        archive.persist(item)
                self.assertEqual([path.read_bytes() for path in paths], previous)
                self.assertFalse(list(archive.directory.glob("*.rollback")))
        # A tight global limit must reject the rewrite rather than evict its
        # durable predecessor while reserving replacement bytes.
        archive.defaults = (30, sum(map(len, previous)) + 1)
        with self.assertRaises(addon.ArchiveFull):
            archive.persist(item)
        self.assertEqual([path.read_bytes() for path in paths], previous)

    def test_startup_restores_interrupted_websocket_rewrite(self):
        archive = self.archive()
        item = flow(payload=b"durable-handshake")
        archive.persist(item)
        identifier = item.metadata["olr_history_id"]
        transaction = archive.directory / ("." + identifier + ".rollback")
        transaction.mkdir(mode=0o700)
        for suffix in (".mitm", ".json", ".meta"):
            original = archive.directory / (identifier + suffix)
            os.link(original, transaction / ("previous" + suffix))
        replacement = archive.directory / ".replacement.tmp"
        replacement.write_bytes(b"interrupted-update")
        os.replace(replacement, archive.directory / (identifier + ".mitm"))
        self.archive()
        self.assertEqual((archive.directory / (identifier + ".mitm")).read_bytes(), b"durable-handshake")
        self.assertFalse(transaction.exists())

    def test_full_filesystem_status_fallback_and_sticky_failure(self):
        addon._status()
        path = self.directory / "archive-status.json"
        before_inode = path.stat().st_ino
        with mock.patch.object(addon, "_atomic_json", side_effect=OSError(errno.ENOSPC, "no free inodes")):
            addon._status("flow capture failed: disk full")
        status = json.loads(path.read_text())
        self.assertEqual(path.stat().st_ino, before_inode)
        self.assertIn("disk full", status["error"])
        self.assertEqual(status["failure_count"], 1)
        addon._status()
        status = json.loads(path.read_text())
        self.assertEqual(status["error"], "")
        self.assertIn("disk full", status["last_failure"])
        self.assertEqual(status["failure_count"], 1)

    def test_failed_and_websocket_flows_are_retained_and_rewritten(self):
        addon._archive = self.archive()
        failed = flow()
        failed.response = None
        failed.error = "connection refused"
        with mock.patch("sys.stdout", new=io.StringIO()):
            asyncio.run(addon.error(failed))
        identifier = failed.metadata["olr_history_id"]
        metadata = json.loads((addon._archive.directory / (identifier + ".json")).read_text())
        self.assertEqual(metadata["status"], 0)
        self.assertIsNone(metadata["response"])
        self.assertEqual(metadata["error"], "connection refused")
        ws = flow(payload=b"handshake")
        with mock.patch("sys.stdout", new=io.StringIO()):
            asyncio.run(addon.response(ws))
        identifier = ws.metadata["olr_history_id"]
        ws.websocket = SimpleNamespace(messages=[SimpleNamespace(content=b"hello")])
        ws.payload = b"handshake-and-final-websocket-messages"
        asyncio.run(addon.websocket_end(ws))
        self.assertEqual((addon._archive.directory / (identifier + ".mitm")).read_bytes(), ws.payload)
        self.assertEqual(len(addon._retained), 2)
        for _ in range(1005):
            addon.retain(flow())
        self.assertEqual(len(addon._retained), 1000)
        addon.ctx.master.commands.call.assert_called()

    def test_cached_accounting_and_external_deletion_generation(self):
        archive = self.archive()
        first = flow()
        archive.persist(first)
        identifier = first.metadata["olr_history_id"]
        summary = json.loads((archive.directory / (identifier + ".meta")).read_text())
        self.assertNotIn("request", summary)
        self.assertNotIn("response", summary)
        with mock.patch.object(archive, "_files", side_effect=AssertionError("per-flow archive scan")):
            archive.persist(flow())
        # The API marks a generation after clear/retention; the next write
        # refreshes cached totals before reserving more disk space.
        for suffix in (".mitm", ".json", ".meta"):
            (archive.directory / (identifier + suffix)).unlink()
        (archive.root / ".generation").write_text("external change")
        archive.persist(flow())
        actual = sum(size for _, _, _, size in archive._files())
        self.assertEqual(archive._total, actual)

    def test_clear_watermark_prevents_websocket_resurrection(self):
        archive = self.archive()
        item = flow()
        archive.persist(item)
        identifier = item.metadata["olr_history_id"]
        for suffix in (".mitm", ".json", ".meta"):
            (archive.directory / (identifier + suffix)).unlink()
        (archive.directory / ".cleared_before").write_text(f"{int(time.time()*1_000_000):020d}")
        (archive.root / ".generation").write_text("cleared")
        item.websocket = SimpleNamespace(messages=[SimpleNamespace(content=b"late")])
        archive.persist(item)
        self.assertFalse((archive.directory / (identifier + ".meta")).exists())
        archive.persist(flow())
        self.assertEqual(len(list(archive.directory.glob("*.meta"))), 1)

    def test_bounded_event_rotation_preserves_stdout(self):
        with mock.patch("sys.stdout", new=io.StringIO()) as stdout:
            for index in range(8):
                addon.emit({"kind": "request", "index": index, "url": "x" * (256*1024)})
            self.assertEqual(len(stdout.getvalue().splitlines()), 8)
        current = self.directory / "events.jsonl"
        previous = self.directory / "events.jsonl.1"
        self.assertTrue(previous.exists())
        self.assertLessEqual(current.stat().st_size, addon.EVENT_BYTES)
        self.assertLessEqual(previous.stat().st_size, addon.EVENT_BYTES)
        self.assertEqual(current.stat().st_mode & 0o777, 0o600)
        self.assertEqual(len(list(self.directory.glob("events.jsonl*"))), 2)

    def test_symlink_device_rejected_and_not_swept(self):
        archive = self.archive()
        outside = self.directory / "outside"
        outside.mkdir()
        secret = outside / "secret"
        secret.write_text("private")
        (archive.root / OTHER).symlink_to(outside, target_is_directory=True)
        archive.sweep(startup=True)
        self.assertEqual(secret.read_text(), "private")
        with self.assertRaises(OSError):
            self.archive(mac=OTHER)
        with self.assertRaises(ValueError):
            self.archive(mac="../../outside")


class IdleSweepTests(unittest.IsolatedAsyncioTestCase):
    async def test_writer_runs_off_loop(self):
        import threading
        started, release = threading.Event(), threading.Event()
        def persist(_):
            started.set()
            release.wait(timeout=2)
        original = addon._archive
        addon._archive = SimpleNamespace(persist=persist)
        addon._archive_gate = asyncio.Lock()
        self.addCleanup(setattr, addon, "_archive", original)
        with mock.patch.object(addon, "_status"):
            task = asyncio.create_task(addon.archive(flow()))
            try:
                for _ in range(100):
                    if started.is_set():
                        break
                    await asyncio.sleep(0.001)
                self.assertTrue(started.is_set())
                self.assertFalse(task.done())
            finally:
                release.set()
                await task

    async def test_retention_runs_without_any_flow(self):
        original = addon._archive
        archive = SimpleNamespace(sweep=mock.Mock())
        addon._archive = archive
        addon._archive_gate = asyncio.Lock()
        self.addCleanup(setattr, addon, "_archive", original)
        with mock.patch.object(addon, "SWEEP_SECONDS", 0.001):
            task = asyncio.create_task(addon._periodic_sweep())
            try:
                await asyncio.sleep(0.012)
                self.assertGreater(archive.sweep.call_count, 0)
            finally:
                task.cancel()
                with self.assertRaises(asyncio.CancelledError):
                    await task


if __name__ == "__main__":
    unittest.main()
