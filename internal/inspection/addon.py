import json
import sys
from mitmproxy import http


def emit(item):
    print(json.dumps(item, ensure_ascii=True), file=sys.stdout, flush=True)


def response_preview(flow: http.HTTPFlow):
    content_type = flow.response.headers.get("content-type", "").split(";", 1)[0].lower()
    textual = content_type.startswith("text/") or content_type in (
        "application/json", "application/xml", "application/javascript",
        "application/xhtml+xml", "image/svg+xml",
    ) or content_type.endswith(("+json", "+xml"))
    if not textual:
        return None, "non-text response"
    raw = flow.response.raw_content
    if raw is None:
        return None, "streamed response"
    if len(raw) > 1024 * 1024:
        return None, "response exceeds 1 MiB preview limit"
    text = flow.response.get_text(strict=False)
    if text is None:
        return None, "could not decode response"
    return text[:4096], "truncated to 4,096 characters" if len(text) > 4096 else None


def response(flow: http.HTTPFlow):
    preview, note = response_preview(flow)
    emit({
        "kind": "request",
        "at": flow.request.timestamp_start,
        "method": flow.request.method,
        "url": flow.request.pretty_url,
        "status": flow.response.status_code,
        "request_headers": dict(flow.request.headers),
        "response_headers": dict(flow.response.headers),
        "response_preview": preview,
        "response_note": note,
    })


def error(flow: http.HTTPFlow):
    emit({
        "kind": "failed",
        "at": flow.request.timestamp_start if flow.request else 0,
        "target": flow.request.host if flow.request else "unknown",
        "reason": str(flow.error)[:300] if flow.error else "connection failed",
    })


def client_connected(client):
    emit({"kind": "accepted"})
