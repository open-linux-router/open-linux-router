import asyncio
from contextlib import contextmanager
import fcntl
import heapq
import json
import os
from pathlib import Path
import re
import sys
import time
import uuid
import zlib

from mitmproxy import ctx, http, io

MAX_FLOWS = 1000
DEFAULT_RETENTION_DAYS = 30
DEFAULT_MAX_BYTES = 5 * 1024**3
SWEEP_SECONDS = 60
PREVIEW_BYTES = 64 * 1024
EVENT_BYTES = 1024 * 1024
_ARCHIVE_ID = re.compile(r"[0-9]{20}-[0-9a-f]{32}\Z")
_MAC = re.compile(r"[0-9a-f]{2}(?::[0-9a-f]{2}){5}\Z")
_retained = []
_archive = None
_sweep_task = None
_archive_gate = asyncio.Lock()
_last_archive_failure = ""
_archive_failure_count = 0


def _atomic_json(path, value):
    temporary = path.with_name("." + path.name + "." + uuid.uuid4().hex + ".tmp")
    try:
        with temporary.open("xb") as output:
            os.chmod(temporary, 0o600)
            output.write(json.dumps(value, ensure_ascii=True, separators=(",", ":")).encode())
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, path)
        _sync_directory(path.parent)
    finally:
        temporary.unlink(missing_ok=True)


def _status(error=""):
    global _last_archive_failure, _archive_failure_count
    path = Path(ctx.options.confdir).expanduser() / "archive-status.json"
    if error:
        _last_archive_failure = str(error)[:300]
        _archive_failure_count += 1
    value = {"error": str(error)[:300], "updated_at": time.time(),
             "last_failure": _last_archive_failure, "failure_count": _archive_failure_count}
    # Preallocate two blocks while healthy, including space for two escaped
    # error strings. The API still reads an ordinary JSON object.
    size = len(json.dumps(value, ensure_ascii=True, separators=(",", ":")).encode())
    value["_reserved"] = " " * max(0, 8192 - size - 15)
    try:
        _atomic_json(path, value)
        return
    except Exception:
        pass
    try:
        # ENOSPC/EDQUOT can prevent allocating an atomic replacement inode.
        # Overwrite the existing preallocated block without O_TRUNC instead.
        encoded = json.dumps(value, ensure_ascii=True, separators=(",", ":")).encode()
        fd = os.open(path, os.O_WRONLY | os.O_NOFOLLOW)
        with os.fdopen(fd, "r+b", buffering=0) as output:
            size = os.fstat(fd).st_size
            encoded = encoded.ljust(max(size, len(encoded)), b" ")
            output.write(encoded)
            os.fsync(fd)
    except Exception:
        pass  # No filesystem can guarantee writes after all storage fails.


def emit(item):
    line = json.dumps(item, ensure_ascii=True) + "\n"
    if len(line.encode()) > EVENT_BYTES:
        # Full headers remain in the native archive. The event journal is an
        # attribution feed, so retain its compact fields for extreme headers.
        compact = {key: value[:16384] if isinstance(value, str) else value
                   for key, value in item.items()
                   if key not in ("request_headers", "response_headers")}
        line = json.dumps(compact, ensure_ascii=True) + "\n"
    try:
        print(line, end="", file=sys.stdout, flush=True)
    except (OSError, ValueError):
        pass
    try:
        path = Path(ctx.options.confdir).expanduser() / "events.jsonl"
        if len(line.encode()) > EVENT_BYTES:
            raise ValueError("event exceeds event log limit")
        if path.is_symlink() or path.with_name("events.jsonl.1").is_symlink():
            raise OSError("event log cannot be a symlink")
        if path.exists() and path.stat().st_size + len(line.encode()) > EVENT_BYTES:
            os.replace(path, path.with_name("events.jsonl.1"))
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_APPEND | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, "a") as output:
            os.fchmod(output.fileno(), 0o600)
            output.write(line)
    except Exception as error:
        _warn(error)


def _warn(error):
    # Never include request URLs or bodies in operational logs.
    ctx.log.warning("OLR flow archive: " + str(error)[:300])
    _status(error)


def _private_directory(path):
    if path.is_symlink():
        raise OSError("history directory cannot be a symlink")
    path.mkdir(mode=0o700, exist_ok=True)
    os.chmod(path, 0o700)


def _sync_directory(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def _message_preview(message):
    if message is None:
        return None
    raw = message.raw_content or b""
    data = raw[:PREVIEW_BYTES + 1]
    encoding = message.headers.get("content-encoding", "").lower().strip()
    binary = False
    if encoding in ("gzip", "deflate"):
        try:
            decoder = zlib.decompressobj(31 if encoding == "gzip" else 15)
            data = decoder.decompress(raw, PREVIEW_BYTES + 1)
        except zlib.error:
            binary = True
    elif encoding not in ("", "identity"):
        binary = True
    content_type = message.headers.get("content-type", "").lower()
    textual = any(part in content_type for part in ("text/", "json", "xml", "javascript", "x-www-form-urlencoded"))
    if "application/octet-stream" in content_type or b"\0" in data:
        binary = True
    charset = re.search(r"charset\s*=\s*[\"']?([^;\s\"']+)", content_type)
    try:
        text = data[:PREVIEW_BYTES].decode(charset.group(1) if charset else "utf-8")
    except (UnicodeError, LookupError):
        if textual:
            text = data[:PREVIEW_BYTES].decode("utf-8", errors="replace")
        else:
            text, binary = "", True
    encoded_text = text.encode("utf-8")
    truncated = (len(data) > PREVIEW_BYTES or
                 (not encoding and len(raw) > PREVIEW_BYTES) or
                 len(encoded_text) > PREVIEW_BYTES)
    text = encoded_text[:PREVIEW_BYTES].decode("utf-8", errors="ignore")
    return {
        "headers": [{"name": name, "value": value} for name, value in message.headers.items(multi=True)],
        "body": "" if binary else text,
        "body_bytes": len(raw),
        "body_truncated": truncated,
        "body_binary": binary,
    }


class ArchiveFull(OSError):
    pass


class _FlowFile:
    """Bound disk writes even if serialization exceeds the body estimate."""

    def __init__(self, archive, output, metadata_bytes):
        self.archive = archive
        self.output = output
        self.metadata_bytes = metadata_bytes
        self.size = 0

    def write(self, data):
        size = self.size + len(data)
        if size + self.metadata_bytes > self.archive.max_bytes:
            raise ArchiveFull("flow exceeds archive size limit")
        self.archive._reserve(size + self.metadata_bytes)
        self.output.write(data)
        self.size = size


class HistoryArchive:
    def __init__(self, confdir, device, retention_days, max_bytes):
        device = device.strip().lower()
        if not _MAC.fullmatch(device):
            raise ValueError("olr_device must be a six-byte colon-separated MAC")
        if retention_days <= 0 or max_bytes <= 0:
            raise ValueError("archive retention and size limits must be positive")
        self.defaults = (retention_days, max_bytes)
        self.retention_days, self.max_bytes = self.defaults
        self.confdir = Path(confdir).expanduser()
        self.root = self.confdir / "history"
        _private_directory(self.root)
        self.directory = self.root / device
        _private_directory(self.directory)
        self._lock_depth = 0
        self._total = None
        self._generation = None
        self._protected = None
        self.sweep(startup=True)

    @contextmanager
    def _locked(self):
        if self._lock_depth:
            self._lock_depth += 1
            try:
                yield
            finally:
                self._lock_depth -= 1
            return
        fd = os.open(self.root / ".archive.lock", os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
        try:
            fcntl.flock(fd, fcntl.LOCK_EX)
            self._lock_depth = 1
            try:
                yield
            finally:
                self._lock_depth = 0
                fcntl.flock(fd, fcntl.LOCK_UN)
        finally:
            os.close(fd)

    def _policy(self):
        try:
            path = self.confdir / "retention.json"
            if path.is_symlink():
                raise ValueError("retention policy cannot be a symlink")
            with path.open("rb") as source:
                policy = json.loads(source.read(4097))
            days, size = policy["days"], policy["max_bytes"]
            if type(days) is not int or type(size) is not int or not 1 <= days <= 365 or not 1024**2 <= size <= 1024**4:
                raise ValueError("invalid history retention policy")
            self.retention_days, self.max_bytes = days, size
        except FileNotFoundError:
            self.retention_days, self.max_bytes = self.defaults

    def _directories(self):
        with os.scandir(self.root) as directories:
            for entry in directories:
                if _MAC.fullmatch(entry.name) and entry.is_dir(follow_symlinks=False):
                    yield Path(entry.path)

    @staticmethod
    def _delete(directory, identifier):
        for suffix in (".meta", ".json", ".mitm"):
            (directory / (identifier + suffix)).unlink(missing_ok=True)

    @staticmethod
    def _finish_transaction(directory, identifier, transaction, restore):
        for suffix in (".mitm", ".json", ".meta"):
            backup = transaction / ("previous" + suffix)
            if backup.exists():
                if restore:
                    os.replace(backup, directory / (identifier + suffix))
                    # POSIX rename is a no-op when both paths already name
                    # the same inode (a sidecar may not have been replaced).
                    backup.unlink(missing_ok=True)
                else:
                    backup.unlink()
        transaction.rmdir()
        _sync_directory(directory)

    def _recover_transactions(self, directory):
        with os.scandir(directory) as files:
            for entry in files:
                match = re.fullmatch(r"\.([0-9]{20}-[0-9a-f]{32})\.(rollback|committed)", entry.name)
                if match and entry.is_dir(follow_symlinks=False):
                    self._finish_transaction(directory, match.group(1), Path(entry.path),
                                             restore=match.group(2) == "rollback")

    def _files(self):
        for directory in self._directories():
            with os.scandir(directory) as files:
                for entry in files:
                    identifier, suffix = os.path.splitext(entry.name)
                    if suffix not in (".mitm", ".json", ".meta") or not _ARCHIVE_ID.fullmatch(identifier):
                        continue
                    if entry.is_file(follow_symlinks=False):
                        try:
                            yield identifier, directory, suffix, entry.stat(follow_symlinks=False).st_size
                        except FileNotFoundError:
                            pass

    def _current_generation(self):
        try:
            return (self.root / ".generation").stat().st_mtime_ns
        except FileNotFoundError:
            return 0

    def _reserve(self, size):
        if size > self.max_bytes:
            raise ArchiveFull("flow exceeds archive size limit")
        if self._total is None or self._generation != self._current_generation():
            self._total = sum(size for _, _, _, size in self._files())
            self._generation = self._current_generation()
        if self._total + size > self.max_bytes:
            self._sweep(reserve=size)

    def sweep(self, reserve=0, startup=False):
        with self._locked():
            return self._sweep(reserve, startup)

    def _sweep(self, reserve=0, startup=False):
        self._policy()
        cutoff = int((time.time() - self.retention_days * 86400) * 1_000_000)
        # Sidecars are commit markers. Recover crash leftovers without decoding
        # bodies, and never traverse device-directory symlinks.
        if startup:
            for directory in self._directories():
                self._recover_transactions(directory)
                with os.scandir(directory) as files:
                    for entry in files:
                        identifier, suffix = os.path.splitext(entry.name)
                        if entry.name.startswith(".") and entry.name.endswith(".tmp"):
                            if entry.is_file(follow_symlinks=False):
                                os.unlink(entry.path)
                        elif _ARCHIVE_ID.fullmatch(identifier) and suffix in (".mitm", ".json", ".meta"):
                            if not all((directory / (identifier + part)).is_file()
                                       for part in (".mitm", ".json", ".meta")):
                                self._delete(directory, identifier)
        for identifier, directory, _, _ in self._files():
            if int(identifier[:20]) < cutoff:
                self._delete(directory, identifier)
        total = sum(size for _, _, _, size in self._files())
        target = max(0, self.max_bytes - reserve)
        while total > target:
            # A bounded batch avoids growing memory with the archive's size.
            oldest = heapq.nsmallest(128, (
                (identifier, directory) for identifier, directory, suffix, _ in self._files()
                if suffix == ".mitm" and (directory, identifier) != self._protected
            ), key=lambda item: (item[0], str(item[1])))
            if not oldest:
                break
            removed = 0
            for identifier, directory in oldest:
                size = 0
                for suffix in (".mitm", ".json", ".meta"):
                    try:
                        size += (directory / (identifier + suffix)).lstat().st_size
                    except FileNotFoundError:
                        pass
                self._delete(directory, identifier)
                total -= size
                removed += size
                if total <= target:
                    break
            if not removed:
                break
        if total > target:
            raise ArchiveFull("archive has no space for another flow")
        self._total = total
        self._generation = self._current_generation()
        return total

    def persist(self, flow):
        with self._locked():
            try:
                return self._persist(flow)
            finally:
                self._protected = None

    def _persist(self, flow):
        self._policy()
        request = flow.request
        identifier = flow.metadata.get("olr_history_id", "")
        if not isinstance(identifier, str) or not _ARCHIVE_ID.fullmatch(identifier):
            identifier = f"{int(time.time() * 1_000_000):020d}-{uuid.uuid4().hex}"
            flow.metadata["olr_history_id"] = identifier
        if int(identifier[:20]) < int((time.time() - self.retention_days * 86400) * 1_000_000):
            return
        try:
            cleared_before = (self.directory / ".cleared_before").read_text().strip()
            if re.fullmatch(r"[0-9]{20}", cleared_before) and identifier[:20] <= cleared_before:
                return
        except FileNotFoundError:
            pass
        entry = {
            "id": identifier,
            "at": request.timestamp_start if request else time.time(),
            "method": request.method if request else "",
            "url": request.pretty_url if request else "",
            "status": flow.response.status_code if flow.response else 0,
            "bytes": self.max_bytes,
            "request": _message_preview(request),
            "response": _message_preview(flow.response),
        }
        if flow.error:
            entry["error"] = str(flow.error)[:300]
        summary = {key: entry[key] for key in ("id", "at", "method", "url", "status", "bytes")}
        metadata_bytes = sum(len(json.dumps(value, ensure_ascii=True, separators=(",", ":")).encode())
                             for value in (entry, summary))
        body_bytes = sum(len(message.raw_content or b"") for message in (request, flow.response) if message)
        if flow.websocket:
            body_bytes += sum(len(message.content) for message in flow.websocket.messages)
        if body_bytes + metadata_bytes > self.max_bytes:
            raise ArchiveFull("flow bodies exceed archive size limit")
        self._protected = (self.directory, identifier)
        temporary = self.directory / ("." + identifier + "." + uuid.uuid4().hex + ".tmp")
        final = self.directory / (identifier + ".mitm")
        committed = False
        published = False
        transaction = None
        try:
            with temporary.open("xb") as output:
                os.chmod(temporary, 0o600)
                bounded = _FlowFile(self, output, metadata_bytes)
                io.FlowWriter(bounded).add(flow)
                output.flush()
                os.fsync(output.fileno())
                entry["bytes"] = bounded.size
                summary["bytes"] = bounded.size
            old_bytes = 0
            for suffix in (".mitm", ".json", ".meta"):
                try:
                    old_bytes += (self.directory / (identifier + suffix)).stat().st_size
                except FileNotFoundError:
                    pass
            if (self.directory / (identifier + ".meta")).is_file():
                transaction = self.directory / ("." + identifier + ".rollback")
                transaction.mkdir(mode=0o700)
                for suffix in (".mitm", ".json", ".meta"):
                    os.link(self.directory / (identifier + suffix), transaction / ("previous" + suffix))
                _sync_directory(transaction)
                _sync_directory(self.directory)
            os.replace(temporary, final)
            published = True
            _atomic_json(self.directory / (identifier + ".json"), entry)
            _atomic_json(self.directory / (identifier + ".meta"), summary)
            _sync_directory(self.directory)
            if transaction:
                committed_directory = self.directory / ("." + identifier + ".committed")
                os.replace(transaction, committed_directory)
                transaction = committed_directory
                _sync_directory(self.directory)
            committed = True
            new_bytes = sum((self.directory / (identifier + suffix)).stat().st_size
                            for suffix in (".mitm", ".json", ".meta"))
            self._total += new_bytes - old_bytes
        finally:
            temporary.unlink(missing_ok=True)
            if transaction:
                self._finish_transaction(self.directory, identifier, transaction, restore=not committed)
                if not committed:
                    self._total = None
            elif published and not committed:
                self._delete(self.directory, identifier)
                self._total = None


def load(loader):
    loader.add_option("olr_device", str, "", "MAC address owning this flow history")
    loader.add_option("olr_retention_days", int, DEFAULT_RETENTION_DAYS, "Days to retain full flow history")
    loader.add_option("olr_max_bytes", int, DEFAULT_MAX_BYTES, "Maximum total bytes of flow history")


def _open_archive():
    global _archive
    try:
        _archive = HistoryArchive(ctx.options.confdir, ctx.options.olr_device,
                                  ctx.options.olr_retention_days, ctx.options.olr_max_bytes)
        _status()
    except Exception as error:
        _archive = None
        _warn(error)


def configure(updated):
    # Startup initialization runs in running(); subsequent policy changes are
    # read from retention.json by the worker before every write and sweep.
    pass


async def running():
    global _sweep_task
    if ctx.options.olr_device:
        async with _archive_gate:
            await asyncio.to_thread(_open_archive)
    _sweep_task = asyncio.create_task(_periodic_sweep())


def _sweep_archive():
    try:
        if _archive:
            _archive.sweep()
        elif ctx.options.olr_device:
            _open_archive()
    except Exception as error:
        _warn(error)


async def _periodic_sweep():
    while True:
        await asyncio.sleep(SWEEP_SECONDS)
        async with _archive_gate:
            await asyncio.to_thread(_sweep_archive)


def done():
    if _sweep_task:
        _sweep_task.cancel()


async def archive(flow):
    # Each mitmproxy hook awaits the worker, applying backpressure rather than
    # keeping an unbounded queue of completed body-bearing flows in memory.
    async with _archive_gate:
        await asyncio.to_thread(_archive_flow, flow)


def _archive_flow(flow):
    try:
        if _archive:
            _archive.persist(flow)
            _status()
        elif ctx.options.olr_device:
            _open_archive()
            if _archive:
                _archive.persist(flow)
                _status()
    except Exception as error:
        _warn(error)


def endpoints(flow: http.HTTPFlow):
    """The device and the original destination recovered by the proxy."""
    peer = flow.client_conn.peername
    server = flow.server_conn.address
    return (peer[0] if peer else ""), (server[0] if server else "")


async def response(flow: http.HTTPFlow):
    client, server = endpoints(flow)
    emit({
        "kind": "request",
        "at": flow.request.timestamp_start,
        "method": flow.request.method,
        "url": flow.request.pretty_url,
        "status": flow.response.status_code,
        "client": client,
        "server": server,
        "request_headers": dict(flow.request.headers),
        "response_headers": dict(flow.response.headers),
    })
    retain(flow)
    await archive(flow)


def retain(flow: http.HTTPFlow):
    if any(item is flow for item in _retained):
        return
    _retained.append(flow)
    excess = len(_retained) - MAX_FLOWS
    if excess <= 0:
        return
    dropped = _retained[:excess]
    del _retained[:excess]
    try:
        ctx.master.commands.call("view.flows.remove", dropped)
    except Exception:
        pass


async def error(flow: http.HTTPFlow):
    client, server = endpoints(flow)
    emit({
        "kind": "failed",
        "at": flow.request.timestamp_start if flow.request else 0,
        "target": flow.request.host if flow.request else "unknown",
        "reason": str(flow.error)[:300] if flow.error else "connection failed",
        "client": client,
        "server": server,
    })
    retain(flow)
    await archive(flow)


async def websocket_end(flow: http.HTTPFlow):
    # Rewrite the handshake with all final websocket messages and close data.
    retain(flow)
    await archive(flow)


def client_connected(client):
    emit({"kind": "accepted"})
