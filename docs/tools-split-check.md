# Split check

The split check asks Ookla's public speed-test fleet which address it sees this
router leave from, then groups the answers by address. One address means every
server took the same way out; several means this network hands different
destinations to different exits.

This is the router's own answer, not a browser's: nothing on the LAN installs or
trusts anything, and a server reached from this page describes the path a
forwarded connection would take.

## How the fleet is assembled

Ookla's server list answers with servers near the caller unless asked for a
place, so the fleet is built from one search per country over a fixed list (48
entries covering every continent), merged round-robin and capped at 1200
servers. The list is cached for six hours; a run does not re-read it.

Each probe is one request:

```
GET http://<server>:8080/getip?nocache=<nonce>
```

The canonical server name answers with the caller's address as text. A server
that still answers on its own name redirects there first, which the client
follows. Probes run 32 at a time, each with an 8-second timeout, and the whole
run is bounded at 180 seconds.

## The answer

```json
{"asked":1200,"answered":1111,
 "egresses":[
   {"ip":"203.0.113.9","place":{"city":"Hong Kong","country":"HK"},"count":1111,
    "countries":["Australia","Brazil","Canada","…"],"servers":["CSL · Hong Kong","…"]}
 ],
 "failures":[{"server":"HOT Mobile · Israel","error":"EOF"}]}
```

`servers` is a sample of four per group; with a thousand probes in one group the
count and the country list are the answer. Failures keep their row, and the
totals stay on screen so a run that answered 1111 of 1200 servers reads as one
about 1111 servers. Places come from the same ip.sb lookup the traffic map uses
([ip.sb](https://ip.sb)) and are approximate.

## What it does and does not say

- The fleet is Ookla's public speed-test server list, not every network on the
  internet. It spans 36+ countries in a typical run, which is enough to tell one
  way out from several, and not enough to prove a claim about every destination.
- A server that answers with a private, loopback, link-local or carrier-grade
  NAT address is a failed probe. Some servers behind their own NAT answer that
  way; reporting such an address as an egress would be worse than reporting the
  failure.
- Only IPv4 is asked. IPv6 egress can differ and is not covered.
- The check does not say *why* two addresses differ: DNS, an exit's routing, or
  the operator's own proxy rules all produce the same two groups.
- A server answering with this router's address is evidence about the path to
  that server's network, not a promise about that server's own reachability.
- Nothing is stored and no configuration changes. Each run reveals this
  router's egress to up to 1200 third parties.
