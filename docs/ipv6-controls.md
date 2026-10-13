# Independent IPv6 controls

DNS, address acquisition, DHCP advertisements and gateway forwarding are
independent decisions. Changing one does not change the others.

## DNS answers

Gateway → DNS and DNS → Resolving both expose **Resolve IPv6 addresses
(AAAA)**. Turning it off sets `dns.filter_aaaa` and returns successful empty
answers (NODATA) for AAAA queries before local names, policies or upstream
selection. A queries and other record types keep their usual behavior. This
applies over both UDP and TCP, including queries that would otherwise use an
exit's DNS server. Existing client caches remain until they expire.

The CLI equivalent is `olr dns set --filter-aaaa`;
`olr dns set --filter-aaaa=false` restores AAAA answers.

## Automatic interface addressing

Gateway → an interface → **Automatically obtain IPv6 addresses** controls
`link.ipv6_auto[interface]`. It requires an adopted interface. An absent setting
leaves the host configuration alone; it does not mean automatic addressing is
enabled. An explicit off stops kernel SLAAC and the dhcpcd IPv6 client. On
enables SLAAC even when this router forwards traffic, and enables dhcpcd IPv6
unless OLR already owns the uplink's prefix-delegation client.

Static IPv6 addresses, LAN advertisements, gateway forwarding and DNS are
unchanged. Existing dynamic addresses may remain until their leases expire.
Prefix delegation is also a separate uplink decision and continues when
configured. No new DHCPv6 client is installed or started by this switch.

Settings are reapplied when the daemon starts. Original kernel settings are
saved before the first change and restored when the override is removed or
the interface is released. Host configuration changes are confined to OLR's
existing dhcpcd block. Interfaces managed by NetworkManager, systemd-networkd
or dhclient are refused rather than reporting success while another manager
can overwrite the setting. Kernel writes and partial failures are reported by
the existing apply flow; interface changes require its disruption confirmation.
