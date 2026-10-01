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
