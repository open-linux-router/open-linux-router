# Overview connection latency

`GET /api/gateway/latency` returns background measurements; reading it does
not start a probe. The Internet headline and every custom site use the same
metric: **router-to-site TCP connection latency**, in milliseconds. DNS resolves
the site's hostname first, then timing starts immediately before dialing its
resolved address on port 443 and stops when the TCP handshake completes. It is
similar to a ping round trip, but **not ICMP ping**: TCP and ICMP can be routed,
filtered and prioritised differently. DNS lookup, TLS negotiation, server
processing and page download are **not** included. The separate DNS row measures
a lookup of `example.com`; it is not part of the connection value.

The router probes the Internet candidates over its default route. Custom sites
use the same TCP measurement and can select a gateway exit; on Linux, their
sockets use that exit's current `SO_MARK` routing mark. DNS lookup still uses
the router's resolver and default route. A removed or disabled exit reports a
failure rather than silently switching to the default route. Other platforms
cannot mark sockets and report unsupported rather than probing the wrong path.
A site's HTTPS URL supplies its hostname and icon-discovery starting point; its
path does not affect connection timing. A successful TCP handshake establishes
only that port 443 accepts connections, **not** that HTTPS, login or the app is
working. Icon discovery is a separate bounded HTTPS request, using the chosen
exit, with same-origin page, manifest and favicon rules shared with ingress.
It does not affect the measured latency.

Probes run in the daemon without browser sessions. Each target has a four-second
deadline. Resolved IPv4 and IPv6 addresses are attempted concurrently, and the
first successful connection determines the sample; a broken address cannot
make a working address look slow. Connections are closed immediately. The
Internet candidates are Google, Baidu, Yandex and Cloudflare. Initial discovery
compares all four for four rounds, 15 seconds apart. Sites need three successes
and a successful latest response. The lowest median wins; a second candidate
is retained if its median is within 20% of the winner. The headline shows the
lowest successful selected measurement in the current round. Failure restarts
discovery; all candidates are rediscovered daily. If none qualifies, another
four-round discovery begins after a one-minute backoff. Selection and samples
reset on daemon restart.

Custom sites are independent of Internet selection and are measured on the
same cadence: every 15 seconds during initial discovery, then every minute.
Up to 12 named HTTPS URLs can be stored with `PUT /api/gateway/latency/sites`
using objects such as `{"name":"YouTube","url":"https://www.youtube.com/", "exit":"Proxy"}`.
`exit` is optional and must name a configured gateway exit when saved. An empty
array clears the list. Names are unique and 1-40 characters; URLs cannot include
credentials, custom ports or fragments. Settings live alongside the router
configuration as `olr.json.latency-sites`; results are ephemeral.

The response includes `state` (`measuring`, `ok`, `unreachable`, `unavailable`),
nullable `milliseconds`, `dns_milliseconds`, `checked_at`, the winning `target`,
the Internet `sites` candidates and `custom` sites. Each site carries its last
attempt, nullable measurement, and a short `error` on failure. Excluded Internet
candidates retain old results but never contribute to the headline. The UI
hides stale results (older than 90 seconds at receipt); neither failure nor an
old success is displayed as zero. Custom-site colors indicate this TCP
connection latency, not application health.
