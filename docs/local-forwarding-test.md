# Local forwarding self-test

Tools offers an on-demand software forwarding benchmark. It creates two network
namespaces and two veth pairs with temporary addresses in `198.19.254.0/29`,
then runs three five-second iperf3 stages: four-stream TCP in each direction
and a 128-byte UDP test at 100 Mbps. The latter reports received packets per
second and loss **at that offered rate**, not a maximum packet rate. Each chart
shows one-second throughput samples; TCP headline values are receiver rates. The
host-side virtual ingress RX and egress TX byte counters must both increase for
each stage; otherwise the test fails rather than presenting an unverified path.

The test requires Linux, `ip` (iproute2), `iperf3`, IPv4 forwarding and
privileges to create namespaces and interfaces. It refuses to use the test
subnet if an exact connected route already exists. On normal exit or request
timeout it removes the namespaces and links. A daemon killed without cleanup
may leave them behind; remove `olr-a-*` and `olr-b-*` namespaces and their
`oa*`/`ob*` links only after confirming they belong to this test.

It does not alter a real interface, WAN route, firewall or forwarding sysctl.
The host's firewall may block the temporary traffic, in which case the test
fails rather than bypassing the policy. It does not exercise a physical NIC,
WAN uplink or device-specific exit. OLR NAT matches the configured LAN source
subnets and actual uplink interface; OLR firewall policy matches configured
inside interface names. Temporary virtual interfaces do not satisfy these
conditions. The result explicitly marks both policies **not tested**, even if
traffic succeeds. This check verifies the virtual forwarding path, not that
OLR's production NAT or firewall rules were exercised. Traffic generation and reception share CPU with forwarding, so
results are comparative estimates, not guaranteed WAN capacity. Running the
test can temporarily contend with live router traffic.
