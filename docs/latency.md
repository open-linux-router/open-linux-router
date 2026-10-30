# Overview latency

`GET /api/gateway/latency` returns the latest background measurements without
starting network traffic. Internet and custom sites use the same metric: one
HTTPS HEAD request, timed from acquiring the established connection to receiving
the first response byte. DNS, TCP and TLS setup and document downloads are
excluded. This is an application round trip, including server processing, not
ICMP ping or a page-load measurement. There is no repeated sampling or aggregation.

Each probe has a 15-second deadline. Connections are fresh, environment proxies
are ignored and TLS certificates are verified. Redirects are not followed.
Any HTTP response, including errors and redirects, establishes response latency;
it does not assert that the application is healthy. A proxy accepting TCP or
answering ICMP locally cannot complete this authenticated HTTPS exchange on
behalf of an unreachable destination. Sites that reject HEAD can still return
a measurable HTTP error response.

Internet candidates use the router's default route. Custom sites can choose a
gateway exit; TCP sockets are marked with its current routing mark on Linux.
DNS uses the router's resolver and default route. If a chosen exit is removed,
disabled or unsupported, no default-route fallback is attempted.
The DNS row independently times one lookup of `example.com`.

The built-in Internet choices are Google, Baidu, Yandex and Cloudflare. With no
saved target, one round probes each once and saves the fastest successful
target to `gateway.latency_target` in `olr.json`. Subsequent rounds measure
only that target, including after restart. Failure never changes the target.
If all candidates fail, selection retries on the next one-minute round.
The Internet target and DNS continue their one-minute refresh independently of
monitored sites. Built-in and custom monitored sites run at minute 30 of each
router-local calendar hour, using `time.Local`; changing the Internet target
never triggers an extra site round. On startup before :30 the sampler waits;
after :30 it measures only targets without an attempt in the current hour. It
never fills earlier missing hours or overwrites a restored/manual current-hour
measurement merely because the daemon restarted.

`PUT /api/gateway/latency/target` accepts `{"target":"<listed URL>"}`; an empty
target requests a fresh selection. Changes wake the background monitor and
discard any in-flight Internet result from the old selection. GET stays passive.
The test page does not edit it; the route remains for API and CLI clients.

A click on one monitored site measures that site now.
`POST /api/gateway/latency/sites/{name}/measure` answers `202 Accepted` with the
current snapshot; only the clicked row's `measuring` flag is set until its probe
publishes. A second click while that probe runs is accepted and starts no second
probe. `POST /api/gateway/latency/measure` does the same for every site, which is the
page's Test all; `?category=<key>` (or `custom`) limits it to one section's
refresh button. Background rounds leave every row's flag false, so a row spins
only where somebody asked. A row keeps its last completed value while the
probe runs. Other sites, the Internet target and DNS are left alone. These
routes change no configuration; they only ask for probes.

Common sites are part of the binary's catalog, not the operator's document.
They are present on a fresh install, always probed, and cannot be removed. The catalog carries one URL per site — the faster of the
app's main page and its static asset host, chosen from measurements on the
reference router — and a category (`communication`, `social`, `entertainment`,
`news`, `shopping` or `tools`). A CDN root may answer 403/404; the probe still
counts the response as the edge's latency. Only an exit is per-operator: it
lives in `gateway.latency_common` as `{"name":"Google","exit":"Gateway2"}`,
and omitting an entry leaves that site on the default route.

Up to 20 custom HTTPS URLs can be stored with `PUT /api/gateway/latency/sites`
using objects such as `{"name":"NAS","url":"https://nas.example/","exit":"Proxy"}`.
The same route carries catalog entries: an entry whose name matches a common
site updates its exit and is not stored as a custom site. An empty array clears
the custom list but not the catalog. `exit` is optional and must name a
configured gateway exit. Names are unique and 1–40 characters; URLs cannot
contain credentials, custom ports or fragments. Custom sites live in
`gateway.latency_sites` in `olr.json`; measurements are stored separately in
the runtime database. A custom
site saved before the catalog existed, under a catalog name, moves to the
catalog with its exit preserved.

The /latency page is a test view, not an editor: it groups sites under category
headings, with one row per site and 24 fixed hourly positions for the router's
current day (00–01 through 23–24). Each position holds the latest attempt begun
in that calendar hour; manual tests replace the same hour's point rather than
appending another. Missing hours remain empty, while failed attempts keep their
error and a nil measurement. The row shows the latest measurement and an explicit
site test button. Test all probes everything, and each section has its own
refresh. Exits and custom sites are changed through the API or CLI.

The response includes `state` (`measuring`, `ok`, `unreachable`, `unavailable`),
nullable `milliseconds`, `dns_milliseconds`, `checked_at`, the selected
`target` name and `target_url`, built-in Internet `sites` and the `custom` list.
`local_time` is the current router-local RFC3339 time **with its UTC offset**, read
fresh for every snapshot. The `custom` list holds catalog sites (`builtin: true`,
with a `category`) followed by the operator's custom sites. A monitored site
carries `measuring` while its on-demand probe runs, and chronological `samples`:
one latest attempt per router calendar hour retained for 48 hours. Each sample
has `milliseconds`, optional `error`, `checked_at` (the attempt's start in UTC),
and `hour_start` (the router-local hour's start with its offset). Frontends use
the calendar date/hour in `local_time` and `hour_start`, so the 24-slot view stays
consistent even when the browser and router have different time zones. A repeated
daylight-saving hour has distinct instants; a fixed 24-slot display uses the
latest attempt for that clock hour. A skipped hour stays empty.

Completed attempts are synchronously upserted to the `latency_samples` table in
`/var/lib/open-linux-router/runtime.db`, keyed by URL, exit and absolute hour
start. Runtime schema 4 adds this table without changing configuration. History
survives restarts, expires after 48 hours and has a 10,000-row database bound.
Changing a name/icon preserves measurements; changing the URL or exit does not
attach the prior target's measurements. The latest-started attempt wins when
manual and automatic probes finish out of order; a probe spanning midnight or an
hour boundary belongs to its start hour. Daemon cancellation clears progress
without inventing a failure. Database read errors are logged and probing stays
memory-only; write errors are logged, retain live results, and retry pending
samples on the next completion and shutdown. Runtime history is never written
to `olr.json`.

A failed fixed target retains its identity but clears its measurement.
Each site carries its latest attempt, nullable measurement and short error.
Custom sites keep their last completed outcome while the next probe runs;
a completed failure replaces success. Unmeasured sites show a dash, and
sub-millisecond results show `<1 ms`, never a rounded zero.
Browser-session caching uses a new hourly-history key; old rolling samples are
not merged back into snapshots, because durable history comes from the daemon.

The monitoring dialog preserves the order of the `custom` list. Rows can be
reordered, edited, and given a custom icon. Clicking an icon offers automatic
favicon discovery, a resized PNG/JPEG/WebP upload (at most 96 KiB in stored
form), or a searchable theSVG icon ID. Uploaded images are stored as validated
base64 in the saved site configuration; a theSVG ID names one of the marks
bundled in `olrd`, which is served from the binary and never fetched from a third
party (`internal/brandlogo`). Supplying an icon is independent of the latency
probe.
