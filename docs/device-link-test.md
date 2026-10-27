# Device-to-router speed tests

Two tools measure one client device's link to OLR: **LibreSpeed**, which runs in
that device's browser, and **iPerf3**, which runs a client on it. Both answer
"how fast can this phone or laptop talk to the router", which is a different
question from the **Speedtest by Ookla** tool beside them: that one measures the
router's own uplink and never involves the device at all.

Neither of these measures LAN-to-WAN forwarding, NAT, or another device's link.

## LibreSpeed (browser, default)

Open Tools, then LibreSpeed, on the device. The test runs in that device's
browser against this router, so nothing has to be installed on either end.

The measurement is LibreSpeed 6.3.1 (`internal/tools/librespeed/`, LGPLv3,
unmodified). `olrd` serves the engine at `/link-test/speedtest.js` and its
worker at `/link-test/speedtest_worker.js`, and answers the four requests the
worker makes:

| Request | Answer |
| --- | --- |
| `GET /link-test/download?ckSize=N` | N MiB of a fixed random block, flushed per MiB |
| `POST /link-test/upload` | 200 once the body is read and discarded |
| `GET /link-test/ping` | an empty reply, for latency and jitter |
| `GET /link-test/ip` | the address the router saw the request come from |

`/link-test/` is deliberately not part of `/api`. The upload test posts raw
bytes, and the API refuses a non-JSON body on purpose, so these endpoints sit
outside it. What authorizes them is a viewer cookie: the tools page calls
`POST /api/tools/link-test/session` — inside the API, behind the same password
or token as everything else — and `olrd` answers with an HttpOnly,
SameSite=Strict session cookie scoped to `/link-test/`, forgotten by the daemon
after two hours. This is the pattern the device inspection page already uses to
embed mitmweb. Reopening the page mints a new cookie; a browser that never
signed in to this router gets an explanation, not a bandwidth source.

The tool page loads that engine in its own document instead of framing
upstream's page: LibreSpeed's background and title are gone, the start control
sits outside the measurement, and the measurement is the two dials. Each dial
follows the direction being measured on the engine's own logarithmic scale, holds
its last reading while the other direction runs, and settles on the final number;
latency and jitter appear above them once the engine has measured them. Leaving
the page aborts the test, so the router stops serving blocks to a browser that is
no longer reading them. Nothing frames this surface any more, so it keeps
BrowserGuard's global refusal rather than relaxing it to `SAMEORIGIN`.

Telemetry is off and no result is stored: there is no database behind this page,
so the privacy dialog LibreSpeed can show would be a false promise.

The random block is generated once at startup and repeated. That is what keeps
the router's CPU out of the measurement, and it is safe because the point of the
randomness is only that no step between the router and the browser can compress
the payload and report a speed the wire never carried.

### What limits the number

The measurement ends in the browser, so it is capped by the slowest of: the
device's Wi-Fi, the switch, the router's NIC, the router's HTTP path, and the
device's own JavaScript and CPU. A browser test rarely passes about 1 Gbps even
on a faster link, and a phone on Wi-Fi usually reports the Wi-Fi rate rather than
the wire. Read it as "at least this fast, and stable or not", not as a wire-rate
certificate.

## iPerf3 (sustained, wired)

Open Tools, then iPerf3. OLR starts a temporary iperf3 server on a chosen,
currently configured LAN IPv4 address (TCP 5201) and shows the client commands.
Install iperf3 on the router and the other device; the other device's terminal
displays the results.

This is the honest option for a wired link faster than a browser can measure.
The server stops after two minutes, on request, or when olrd shuts down
normally. A process killed ungracefully can leave a child behind; check for
`iperf3 -s` if the port is unexpectedly busy.

The address is checked against OLR's configured networks and the kernel's
current interface addresses. OLR does not modify firewall rules. If the OLR
firewall is enabled, temporarily allow TCP 5201 from the test device and
remove that opening afterward. Only start a session on a trusted LAN: other
clients able to reach that address and port can use the server while it runs.

The measurement terminates on the router. It measures that device's link and
OLR's local TCP receive/send path, not forwarding between two interfaces,
NAT, or internet speed. Wi-Fi, switches, the client and other traffic can
limit the result before the router's NIC does.

If TCP 5201 is already held by an `iperf3` process and the selected LAN address
is reachable, Tools displays the external server and the client commands
without starting or stopping it. The session lifetime is controlled by that
service, not OLR. A different port holder is reported by process name/PID/unit
where available; OLR never kills or reconfigures an existing process.

## Picking between them

Use LibreSpeed for the everyday question — "is this room's Wi-Fi fine?" —
because it needs nothing installed and runs on the device that is asking. Use
iPerf3 when the answer has to be a wire rate: gigabit or faster Ethernet, a
sustained multi-stream load, or a before/after comparison of a cable or a switch
port.
