# DNS exit

The DNS exit page asks each resolver this router can name where that resolver
leaves from. The answer arrives from the resolver itself: two whoami names
report the address of the resolver that queried them, and that address is
located through the same ip.sb lookup the traffic map uses.

| Leg | Asked at | What it is |
| --- | --- | --- |
| This router | `127.0.0.1:5353` | unbound, doing recursion or forwarding per the DNS settings |
| `<exit name>` | the exit's next hop on port 53, or the custom server | the resolver the relay forwards this exit's clients to |

The legs are the resolvers OLR actually configured, not an inference: the same
list the relay writes into its routes file. When DNS is off there is no leg, and
the page says so instead of guessing.

## The two sources

| Source | Name | Answer |
| --- | --- | --- |
| Google | `o-o.myaddr.l.google.com` TXT | the resolver's address |
| Akamai | `whoami.akamai.net` A | the resolver's address |

Both are asked per leg. One source failing leaves the other's answer visible;
the leg only reports an error when neither answers. Two sources exist because
one provider's view — or one hijacked name — should not pass for the whole
answer.

`POST /api/tools/dns-exit` runs the check:

```json
{"legs":[
  {"name":"This router","server":"127.0.0.1:5353","kind":"router","latency_ms":61.2,
   "answers":[{"source":"Google","name":"o-o.myaddr.l.google.com","ip":"203.0.113.9","place":{"city":"Hong Kong","country":"HK","country_name":"Hong Kong"}}]}
]}
```

The whole check is bounded at 12 seconds. A leg that did not answer keeps its
row with the error attached.

## What it does and does not say

- The address belongs to the **resolver**, not to this router. When unbound
  forwards to a public resolver, the answer is that resolver's exit; when it
  recurses, it is this router's own address.
- A per-device DNS selection is not exercised: the query comes from the router
  itself, so it follows the resolver's own path rather than a particular
  device's exit. Asking as a device would require sending its traffic, which
  this check deliberately does not do.
- The location is ip.sb's answer for that address ([ip.sb](https://ip.sb)).
  GeoIP is approximate and is reported as a city, not as a guarantee.
- Nothing is stored and no configuration changes. Each run sends two queries per
  resolver.
