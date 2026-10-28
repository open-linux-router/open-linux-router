# Split check

The split check asks a fixed set of public endpoints which address they see
this router leave from, then groups the answers by address. One address means
every endpoint took the same way out; several means this network hands
different destinations to different exits.

This is the router's own answer, not a browser's: nothing on the LAN has to
install or trust anything, and an endpoint reached from this page describes the
path a forwarded connection would take.

## Endpoints

| Endpoint | Region | Answer |
| --- | --- | --- |
| ipify | Global | the bare address |
| ipinfo | Global | the bare address |
| icanhazip | Global | the bare address |
| ifconfig.me | Global | the bare address |
| AWS checkip | Global | the bare address |
| Cloudflare | Global | the `ip=` line of `cdn-cgi/trace` |
| ipip.net | China | the address inside a Chinese sentence |

The list is short and fixed on purpose. A provider belongs here only when it
answers with the caller's address and stays reachable without an account or a
key. It is a set of independent vantage points, not a census of the internet, and
the page reports coverage per run: a run where two endpoints answered is a
reading about two endpoints.

## The answer

```json
{"probes":[
  {"probe":"ipify","region":"Global","ip":"203.0.113.9","latency_ms":210.4},
  {"probe":"ipip.net","region":"China","ip":"198.51.100.8","latency_ms":35.1}
 ],
 "egresses":[
  {"ip":"203.0.113.9","place":{"city":"Hong Kong","country":"HK"},"probes":["ipify"]},
  {"ip":"198.51.100.8","place":{"city":"Beijing","country":"CN"},"probes":["ipip.net"]}
 ]
}
```

Groups appear in the order their first probe was asked, so the same answers
produce the same page. Places come from the same ip.sb lookup the traffic map
uses ([ip.sb](https://ip.sb)) and are approximate.

`POST /api/tools/split-check` runs the check; the whole run is bounded at 12
seconds and every probe is one small request. Failures are reported per probe
and never removed from the denominator.

## What it does not do yet

The sweep is not a census. A full sweep — the kind that asks thousands of
Ookla speed-test servers and maps the answer over the whole address space — is
not implemented; the endpoint list above is the part that can be relied on
without a reverse-engineered protocol. Adding a fleet is a change to
`splitProbes`, and the probe interface already separates fetching from parsing.

- Only IPv4 is asked. IPv6 egress can differ from IPv4 and is not covered.
- The check does not say *why* two addresses differ: DNS, an exit's routing, or
  the operator's own proxy rules all produce the same two rows.
- Nothing is stored and no configuration changes. Each run reveals this
  router's egress to the listed providers.
