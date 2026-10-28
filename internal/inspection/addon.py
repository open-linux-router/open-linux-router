import json
import sys

from mitmproxy import ctx, http

# The switch stays on until it is turned off, so the viewer keeps a bounded
# recent list: mitmweb holds every flow in the router's memory, and a device
# left under inspection for a week should not grow without bound. Older flows
# age out of the viewer only; OLR's own event list is bounded separately.
MAX_FLOWS = 1000
_retained = []


def emit(item):
    print(json.dumps(item, ensure_ascii=True), file=sys.stdout, flush=True)


def response(flow: http.HTTPFlow):
    emit({
        "kind": "request",
        "at": flow.request.timestamp_start,
        "method": flow.request.method,
        "url": flow.request.pretty_url,
        "status": flow.response.status_code,
        "request_headers": dict(flow.request.headers),
        "response_headers": dict(flow.response.headers),
    })
    retain(flow)


def retain(flow: http.HTTPFlow):
    _retained.append(flow)
    excess = len(_retained) - MAX_FLOWS
    if excess <= 0:
        return
    dropped = _retained[:excess]
    del _retained[:excess]
    try:
        ctx.master.commands.call("view.flows.remove", dropped)
    except Exception:
        # The cap is a courtesy, not a contract: never take the proxy down
        # because a mitmproxy version names its view command differently.
        pass


def error(flow: http.HTTPFlow):
    emit({
        "kind": "failed",
        "at": flow.request.timestamp_start if flow.request else 0,
        "target": flow.request.host if flow.request else "unknown",
        "reason": str(flow.error)[:300] if flow.error else "connection failed",
    })


def client_connected(client):
    emit({"kind": "accepted"})
