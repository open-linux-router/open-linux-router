"""Native mitmproxy smoke test, run inside a disposable mitmweb process.

Copy beside addon.py, then run a nonproduction instance, for example:
  mitmweb --listen-host 127.0.0.1 --listen-port 28081 --web-host 127.0.0.1 \
    --web-port 28082 --no-web-open-browser --set confdir=/var/tmp/olr-archive-smoke \
    --set olr_device=02:00:00:00:00:01 -s addon.py -s archive_native_smoke.py
No traffic, nft rules, or real device is used. The process shuts itself down.
The caller must check stderr for OLR_ARCHIVE_NATIVE_OK (or ..._FAILED).
"""

import importlib.util
from pathlib import Path
import sys
import tempfile
import time
import traceback

from mitmproxy import connection, ctx, flow, http, io, websocket


def running():
    try:
        spec = importlib.util.spec_from_file_location("olr_archive_smoke_addon", Path(__file__).with_name("addon.py"))
        addon = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(addon)
        with tempfile.TemporaryDirectory(prefix="native-smoke-", dir=ctx.options.confdir) as directory:
            archive = addon.HistoryArchive(directory, "02:00:00:00:00:01", 30, 5*1024**3)
            client = connection.Client(peername=("192.0.2.2", 12345), sockname=("192.0.2.1", 28081))
            server = connection.Server(address=("archive.test", 443))
            captured = http.HTTPFlow(client, server)
            captured.request = http.Request.make("POST", "https://archive.test/body", b"request\x00\xffbody", {"content-type": "application/octet-stream"})
            captured.response = http.Response.make(201, b"response\x00\xfebody", {"content-type": "application/octet-stream", "x-native": "preserved"})
            archive.persist(captured)
            identifier = captured.metadata["olr_history_id"]
            native = archive.directory / (identifier + ".mitm")
            with native.open("rb") as source:
                restored, = io.FlowReader(source).stream()
            assert restored.request.raw_content == b"request\x00\xffbody"
            assert restored.response.raw_content == b"response\x00\xfebody"
            assert restored.response.headers["x-native"] == "preserved"
            # The same file is replaced with the final websocket state.
            captured.websocket = websocket.WebSocketData()
            captured.websocket.messages.append(websocket.WebSocketMessage(1, True, b"hello", time.time()))
            captured.websocket.messages.append(websocket.WebSocketMessage(2, False, b"\x00\xff", time.time()))
            archive.persist(captured)
            with native.open("rb") as source:
                restored, = io.FlowReader(source).stream()
            assert len(restored.websocket.messages) == 2
            assert restored.websocket.messages[1].content == b"\x00\xff"
            assert len(list(archive.directory.glob("*.mitm"))) == 1
            captured.websocket = None
            captured.response = None
            captured.error = flow.Error("native failure capture")
            captured.metadata.pop("olr_history_id", None)
            archive.persist(captured)
            native = archive.directory / (captured.metadata["olr_history_id"] + ".mitm")
            with native.open("rb") as source:
                restored, = io.FlowReader(source).stream()
            assert restored.error.msg == "native failure capture"
            assert restored.response is None
            assert restored.request.raw_content == b"request\x00\xffbody"
        print("OLR_ARCHIVE_NATIVE_OK", file=sys.stderr, flush=True)
    except Exception:
        print("OLR_ARCHIVE_NATIVE_FAILED", file=sys.stderr, flush=True)
        traceback.print_exc(file=sys.stderr)
    finally:
        ctx.master.shutdown()
