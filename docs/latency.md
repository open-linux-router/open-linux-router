# Overview latency

`GET /api/gateway/latency` returns the latest background measurement. Reading
this endpoint does not initiate network traffic. The daemon measures selected sites every minute, independently of browser
sessions, and stops on shutdown. Initial discovery uses four rounds spaced
15 seconds apart. Failed discovery also backs off for one minute before retrying.

The measurement is router-originated HTTPS HEAD response time over its default
route: DNS lookup, TCP connection, TLS handshake and response headers. It is
not ICMP ping or the latency of a particular device, tunnel, or configured exit.
Connections are fresh, TLS certificates are verified, environment proxies are
ignored, redirects are not followed, and HTTP 2xx/3xx responses count as success.
If HEAD returns 403, 405, or 501, a GET is tried on the same URL; a second
rejection remains a failure. GET bodies are not read.
Each target has a four-second deadline and targets are measured concurrently.
A failed website does not establish that the internet as a whole is down.

The initial candidates are Google, Baidu, Yandex and Cloudflare. Four rounds
compare all candidates. Sites need at least three successes and a successful
latest response to qualify. The lowest median time wins; a second site is kept
when its median is within 20% of the winner. The card shows the minimum of the
successful measurements in the current round, not an all-time minimum.

Failure of either selected site restarts discovery on the next round. Every
day all candidates are compared again, allowing changes
in connectivity, regional reachability and edge deployment to change the
selection. If no candidate qualifies, another four-round discovery begins.
Selection and samples live only in memory and reset when the daemon restarts.

The response contains `state` (`measuring`, `ok`, `unreachable`, or `unavailable`),
nullable `milliseconds` and `checked_at`, the winning `target`, and a `sites`
list. Each site includes its URL, last attempt timestamp, nullable measurement,
and whether it is selected. Excluded sites retain their last result; these
older results never contribute to the current headline measurement. No success
in the current round means a null measurement, never zero or an older success.
The UI hides the measurement on API failure or when the sample is over 90 seconds
older than the response receipt time. The expandable list shows per-site results.

## Custom sites

The Overview card's **Monitor sites** dialog accepts up to 12 named HTTPS URLs,
for example WeChat or YouTube. Each custom site uses the same router-originated
HTTPS HEAD probe, four-second timeout, and one-minute steady-state cadence as
the Internet candidates. During initial discovery they are probed every 15
seconds. Custom sites are independent: their failures never change the Internet
headline or its candidate selection. An endpoint that rejects HEAD (for example
with HTTP 403 or 405) is retried with GET; if that also fails, the site
shows a failed status with the HTTP code or a short network error on hover.
Choose a suitable endpoint for that service rather than interpreting a failed
probe as proof that the entire app is down. Icons are discovered using the same
same-origin page, manifest, and favicon lookup as ingress services,
starting at the configured site URL. Discovery
uses the selected exit for HTTP requests, limits response sizes, and caches
icons for a day; an initial is shown when no icon is available.

`PUT /api/gateway/latency/sites` replaces the custom list with JSON objects
`{"name":"YouTube","url":"https://www.youtube.com/"}`. An empty array clears
it. Names are unique, 1–40 characters, and URLs must use HTTPS without
credentials, custom ports, or fragments. The list is stored alongside the
router configuration as `olr.json.latency-sites`; measurements are not persisted.
`GET /api/gateway/latency` includes `custom` measurements (with nullable
`milliseconds`, `checked_at`, and a short `error` on failure) in addition to
the existing `sites` candidate
list. Edits reset custom results until the next probe. A failed probe never
retains a previous successful value.

Each custom site can optionally choose **Internet via** a configured gateway
exit. The default remains the router's normal route. For a named exit, the
probe marks its TCP socket with that exit's current routing mark (`SO_MARK` on
Linux), so TCP, TLS, and the HTTPS response traverse the chosen path. Hostname
resolution still uses the router's resolver and default route. A removed or
disabled exit produces **No response**, never a fallback to the router default.
The JSON entry gains an optional `"exit":"Proxy"` field; the name must exist
when saved. Probes resolve the current mark for each attempt, so changing an
exit's slot does not leave a stale mark in the site settings. This requires
Linux and the daemon's socket-marking privilege; on other platforms selected
exit probes fail rather than silently using the default route.
