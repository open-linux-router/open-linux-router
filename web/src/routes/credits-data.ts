// What the credits page lists is a feature's supplier, not the whole closure:
// a Go or web package earns a row only where it is the thing that carries a
// router capability. The complete lists stay in go.mod and web/package.json.
//
// Versions for Go modules mirror go.mod; update both together. `distro` is a
// descriptor rather than a version: OLR does not pin what the box installs.
export type Credit = {
  name: string
  version: string
  url: string
  website: string
  use: string
  logo?: string
}

export type CreditGroup = {
  title: string
  items: readonly Credit[]
}

export const CREDIT_GROUPS: readonly CreditGroup[] = [
  {
    title: 'Network & kernel',
    items: [
      { name: 'Linux kernel', version: 'distro', url: 'https://kernel.org/', website: 'kernel.org', use: 'Interfaces, routing, neighbours and packet filtering', logo: '/credit-logos/linux.svg' },
      { name: 'iproute2', version: 'distro', url: 'https://git.kernel.org/pub/scm/network/iproute2/iproute2.git', website: 'git.kernel.org', use: 'Traffic control for priorities and speed limits' },
      { name: 'vishvananda/netlink', version: 'v1.3.1', url: 'https://github.com/vishvananda/netlink', website: 'github.com', use: 'Interface, address, route and neighbour changes' },
    ],
  },
  {
    title: 'Services & system',
    items: [
      { name: 'systemd', version: 'distro', url: 'https://systemd.io/', website: 'systemd.io', use: 'Service supervision and unit state', logo: '/credit-logos/systemd.svg' },
      { name: 'go-systemd', version: 'v22.7.0', url: 'https://github.com/coreos/go-systemd', website: 'github.com', use: 'Starting, stopping and inspecting units over D-Bus' },
    ],
  },
  {
    title: 'DHCP & DNS',
    items: [
      { name: 'dnsmasq', version: 'distro', url: 'https://thekelleys.org.uk/dnsmasq/doc.html', website: 'thekelleys.org.uk', use: 'DHCP and IPv6 router advertisements' },
      { name: 'Unbound', version: 'distro', url: 'https://nlnetlabs.nl/projects/unbound/about/', website: 'nlnetlabs.nl', use: 'Recursion, DNSSEC validation and caching', logo: '/credit-logos/unbound.svg' },
      { name: 'unbound-anchor', version: 'distro', url: 'https://nlnetlabs.nl/projects/unbound/about/', website: 'nlnetlabs.nl', use: 'Root trust anchor refresh for DNSSEC', logo: '/credit-logos/unbound.svg' },
    ],
  },
  {
    title: 'Firewall, NAT & enforcement',
    items: [
      { name: 'nftables', version: 'distro', url: 'https://netfilter.org/projects/nftables/', website: 'netfilter.org', use: 'NAT, firewall and DNS enforcement' },
      { name: 'google/nftables', version: 'v0.3.0', url: 'https://github.com/google/nftables', website: 'github.com', use: 'Netfilter rules written directly from Go' },
    ],
  },
  {
    title: 'Remote access & proxies',
    items: [
      { name: 'WireGuard', version: 'in-tree', url: 'https://www.wireguard.com/', website: 'wireguard.com', use: 'VPN tunnels; wg loads keys and peers', logo: '/access-logos/wireguard.svg' },
      { name: '3proxy', version: 'distro', url: 'https://3proxy.org/', website: '3proxy.org', use: 'SOCKS5 server' },
      { name: 'tun2socks', version: 'v2.7.0', url: 'https://github.com/xjasonlyu/tun2socks', website: 'github.com', use: 'Carries proxied traffic through a TUN device' },
      { name: 'mitmproxy', version: 'distro', url: 'https://mitmproxy.org/', website: 'mitmproxy.org', use: 'Optional device request inspection', logo: '/credit-logos/mitmproxy.svg' },
    ],
  },
  {
    title: 'HTTPS ingress',
    items: [
      { name: 'Caddy', version: 'v2', url: 'https://caddyserver.com/', website: 'caddyserver.com', use: 'HTTPS ingress, certificates and reverse proxy', logo: '/service-logos/caddy.svg' },
    ],
  },
  {
    title: 'IPTV & multicast',
    items: [
      { name: 'igmpproxy', version: 'distro', url: 'https://github.com/pali/igmpproxy', website: 'github.com', use: 'IGMP and IPTV multicast routing' },
    ],
  },
  {
    title: 'Names & addresses',
    items: [
      { name: 'ddns-go', version: '7aad574', url: 'https://github.com/jeessy2/ddns-go', website: 'github.com', use: 'Dynamic DNS provider code, ported' },
      { name: 'ip.sb', version: 'API', url: 'https://ip.sb', website: 'ip.sb', use: 'Public addresses and city locations' },
      { name: 'IEEE OUI registry', version: 'registry', url: 'https://standards-oui.ieee.org/', website: 'standards-oui.ieee.org', use: 'Device vendor identification', logo: '/credit-logos/ieee.svg' },
    ],
  },
  {
    title: 'Diagnostics',
    items: [
      { name: 'pro-bing', version: 'v0.9.1', url: 'https://github.com/prometheus-community/pro-bing', website: 'github.com', use: 'Ping, loss and reachability checks' },
      { name: 'cloudflare-speed-cli', version: 'v1.0.10', url: 'https://github.com/kavehtehrani/cloudflare-speed-cli', website: 'github.com', use: 'Reference speed transfer for network quality' },
      { name: 'speedtest-go', version: 'v1.8.3', url: 'https://github.com/showwin/speedtest-go', website: 'github.com', use: 'Selectable-server internet speed test' },
      { name: 'NextTrace', version: 'unpinned', url: 'https://github.com/nxtrace/NTrace-core', website: 'github.com', use: 'Traceroute and route maps', logo: '/tool-logos/nexttrace.webp' },
      { name: 'iperf3', version: 'distro', url: 'https://software.es.net/iperf/', website: 'software.es.net', use: 'LAN throughput test' },
    ],
  },
  {
    title: 'Storage',
    items: [
      { name: 'modernc.org/sqlite', version: 'v1.60.1', url: 'https://sqlite.org/', website: 'sqlite.org', use: 'Runtime history and traffic samples', logo: '/credit-logos/sqlite.svg' },
    ],
  },
]
