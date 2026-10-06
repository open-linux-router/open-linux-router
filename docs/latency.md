# Overview page response

`GET /api/gateway/latency` returns the latest background measurements without
starting network traffic. Internet and custom sites use the same metric: time
from a router-originated HTTPS GET to completion of the HTML document, including
DNS, TCP, TLS, same-host HTTPS redirects, server response and document download.
The router does **not** fetch images, scripts or styles, execute JavaScript or
render a page. This is not ICMP ping or the time until a browser finishes showing
the app. Page size and server behavior affect the number, so compare a site's
trend rather than treating different sites as equivalent network benchmarks.

Each probe has a 15-second deadline and a 2 MiB document limit. It follows up
to five same-host HTTPS redirects; cross-host and HTTP redirects are not
followed. A non-2xx response or a non-HTML content type fails. Connections are
fresh, environment proxies are ignored and TLS certificates are verified.
Internet candidates use the router's default route. Custom sites can choose a
gateway exit; the TCP sockets are marked with its current routing mark on Linux.
DNS still uses the router's resolver and default route. If a chosen exit is
removed, disabled or unsupported, no default-route fallback is attempted.
The separate DNS row measures a lookup of `example.com`, not the lookup of each
site; its time is already included in the page response measurement when that
site requires a lookup.

The Internet candidates are Google, Baidu, Yandex and Cloudflare. Four initial
rounds 15 seconds apart compare them; a site needs three successes and a
successful latest response. The lowest median wins, with a second candidate
retained when within 20% of the winner. The headline shows the lowest current
successful measurement, not a historical minimum. Failure restarts discovery;
all candidates are rediscovered daily. If none qualifies, another discovery
begins after a one-minute backoff. Selection and samples reset on daemon
restart. Custom sites are independent of selection and are measured on the same
cadence: every 15 seconds during discovery, then every minute.

Up to 12 named HTTPS URLs can be stored with `PUT /api/gateway/latency/sites`
using objects such as `{"name":"YouTube","url":"https://www.youtube.com/","exit":"Proxy"}`.
`exit` is optional and must name a configured gateway exit. An empty array
clears the list. Names are unique and 1-40 characters; URLs cannot contain
credentials, custom ports or fragments. Settings live alongside the router
configuration as `olr.json.latency-sites`; results are ephemeral. Site icons
are discovered separately via the ingress same-origin favicon rules and do not
affect the measurement.

The response includes `state` (`measuring`, `ok`, `unreachable`, `unavailable`),
nullable `milliseconds`, `dns_milliseconds`, `checked_at`, the winning `target`,
the Internet `sites` candidates and `custom` sites. Each site carries its last
attempt, nullable measurement and a short `error` on failure. Excluded Internet
candidates retain old results but never contribute to the headline. The UI
hides samples older than 90 seconds at receipt and never presents failures as
zero. The page response badges use Good below 1 second, Fair below 3 seconds,
and Slow above that; those are experience hints, not network health verdicts.
