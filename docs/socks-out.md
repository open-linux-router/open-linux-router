# Transparent outbound SOCKS5 (initial implementation)

`olr socks-out set --proxy socks5://192.0.2.10:1080` saves a SOCKS5 endpoint and starts an independent `olr-socks-out.service`. The Go data plane creates and raises `olrsocks0`. Gateway can then use that TUN as an interface exit; assigning it to a LAN or device needs no changes on the client. No assignment is made automatically: enabling this backend alone changes no device's path.

This first version supports a single unauthenticated SOCKS5 endpoint at a literal IP. Hostnames would create a boot-time DNS dependency; credentials are rejected because the upstream engine logs its proxy URL on start, so passing a password to it would expose the secret in the journal. HTTP, HTTPS, and UDP support depend on the upstream SOCKS5 server; ICMP is not SOCKS traffic. Traffic from `olrd` itself is not selected by Gateway, preventing the proxy's own upstream connection from looping back into the TUN.

To select clients after the backend is active:

```
olr gateway add exit Proxy --interface olrsocks0
olr gateway set via <lan-interface> Proxy
```

Gateway blocks IPv6 for this exit rather than letting it bypass the IPv4 TUN. It rejects `--on-failure direct` and IPv6 `direct`. Its routing table contains a lower-priority `unreachable` default route: when the TUN disappears on a process crash, the interface route is removed but the unreachable route remains and packets cannot fall through to the box's normal gateway. Disable the Gateway assignment before disabling socks-out if you want those clients to use their normal connection again.

## Select destinations by IP

To proxy only traffic to a particular IPv4 address, create the exit above but
leave the LAN's normal assignment unchanged, then run:

```sh
olr gateway set destination 1.2.3.4 Proxy
# A whole destination range is also supported:
olr gateway set destination 203.0.113.0/24 Proxy
olr gateway rm destination 1.2.3.4
```

Use `--dry-run` to preview the kernel changes. These commands persist in
Gateway's `destinations` array as `{"prefix":"1.2.3.4/32","exit":"Proxy"}`.
The API offers PUT/DELETE `/api/gateway/destinations/1.2.3.4/32`
(PUT body: `{"exit":"Proxy"}`). Edits are serialized with
other Gateway changes; they do not replace other assignments.

Destination rules apply to forwarded traffic entering adopted LAN interfaces
with a matching LAN source prefix. They override device, network and default
exit choices; overlapping destination rules use the longest prefix. Router-local
destinations and DNAT connections are excluded. Unmatched traffic keeps its
normal source-based choice. Existing marked connections retain their exit.

These are IP rules, not domain or URL rules: all supported traffic to that IP
uses the exit, including HTTPS without TLS interception. An IPv4 rule does not
match a site's other IPv4 addresses or its IPv6 addresses. IPv6 destination
rules are accepted for other exits; selecting the IPv4-only SOCKS TUN for an
IPv6 destination blocks it. Missing or failed SOCKS TUNs fail closed as above.

This has not yet been exercised on physical Linux routing hardware. Verify the TUN, DNS behavior, UDP support, and fail-closed route on the target router before relying on it.
