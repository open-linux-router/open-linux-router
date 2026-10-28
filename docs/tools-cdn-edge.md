# CDN edge

The CDN edge page asks three providers which of their edge nodes answered this
router. It runs one HTTPS request per provider from the router, changes no
configuration, and stores nothing.

| Provider | Endpoint | What names the node |
| --- | --- | --- |
| Cloudflare | `https://www.cloudflare.com/cdn-cgi/trace` | the `colo=` line |
| CloudFront | `https://aws.amazon.com/` | the `x-amz-cf-pop` header |
| Fastly | `https://www.fastly.com/` | the last value of the `x-served-by` header |

The page shows the airport-style code and keeps the provider's original token
beside it, so `HKG61-P1` is not silently rounded to `HKG`. A provider that
fails keeps its row with the error attached: which CDN did not answer is part
of the result, not a reason to show nothing.

`POST /api/tools/edge` runs the check and answers with every row:

```json
{"edges":[
  {"cdn":"Cloudflare","pop":"HKG","token":"HKG","latency_ms":18.2},
  {"cdn":"CloudFront","pop":"HKG","token":"HKG61-P1","latency_ms":24.9},
  {"cdn":"Fastly","latency_ms":19.4,"error":"the answer carried no X-Served-By header"}
]}
```

The whole check is bounded at 12 seconds and the three requests run at once.
Per-provider failures are reported in the row; only a cancelled or unreachable
check answers 503.

## What it does and does not say

- It reports the node that answered **one request**, not the nearest node and not
  the node a different device, protocol or moment would reach. A CLI or a browser
  that resolves the same name differently — through a proxy's fake-IP, for
  example — can land on another edge, and that is the usual explanation for a
  result that disagrees with what a device sees.
- It measures time to response headers, once. This is not a speed test and does
  not predict transfer rates.
- Environment proxy settings are ignored, so the answer describes the router's
  own path rather than a proxy's.
- Every request to a provider reveals this router's public address to that
  provider, exactly as a browser visiting it would.

## Why only three providers

A provider belongs in the table only when it names its edge node in a response a
browser or the router can already fetch. Adding a provider means proving that,
not inferring a location from latency or from DNS answers.
