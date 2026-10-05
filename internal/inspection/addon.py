import json
import sys
from mitmproxy import http


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


def error(flow: http.HTTPFlow):
    emit({
        "kind": "failed",
        "at": flow.request.timestamp_start if flow.request else 0,
        "target": flow.request.host if flow.request else "unknown",
        "reason": str(flow.error)[:300] if flow.error else "connection failed",
    })
