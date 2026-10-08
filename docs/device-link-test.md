# Device-to-router link test

Tools starts a temporary iperf3 server on a chosen, currently configured OLR
LAN IPv4 address (TCP 5201). Install iperf3 on the router and the other device.
The UI supplies commands for traffic toward and away from the router; the
other device's terminal displays the results. The server stops after two
minutes, on request, or when olrd shuts down normally. A process killed
ungracefully can leave a child behind; check for `iperf3 -s` if the port is
unexpectedly busy.

The address is checked against OLR's configured networks and the kernel's
current interface addresses. OLR does not modify firewall rules. If the OLR
firewall is enabled, temporarily allow TCP 5201 from the test device and
remove that opening afterward. Only start a session on a trusted LAN: other
clients able to reach that address and port can use the server while it runs.

The measurement terminates on the router. It measures that device's link and
OLR's local TCP receive/send path, not forwarding between two interfaces,
NAT, or internet speed. Wi-Fi, switches, the client and other traffic can
limit the result before the router's NIC does.
