import { Link } from 'react-router'

type Credit = { name: string; url: string; use: string }

const backends: Credit[] = [
  { name: 'Linux kernel', url: 'https://kernel.org/', use: 'Interfaces, routing, neighbours and packet filtering' },
  { name: 'systemd', url: 'https://systemd.io/', use: 'Service supervision' },
  { name: 'dnsmasq', url: 'https://thekelleys.org.uk/dnsmasq/doc.html', use: 'DHCP and IPv6 router advertisements' },
  { name: 'Unbound', url: 'https://nlnetlabs.nl/projects/unbound/about/', use: 'DNS resolution' },
  { name: 'nftables', url: 'https://netfilter.org/projects/nftables/', use: 'NAT, firewall and DNS enforcement' },
  { name: 'WireGuard', url: 'https://www.wireguard.com/', use: 'VPN access' },
  { name: 'shadowsocks-rust', url: 'https://github.com/shadowsocks/shadowsocks-rust', use: 'Shadowsocks server' },
  { name: '3proxy', url: 'https://3proxy.org/', use: 'SOCKS5 server' },
  { name: 'Caddy', url: 'https://caddyserver.com/', use: 'HTTPS ingress and certificates' },
  { name: 'mitmproxy', url: 'https://mitmproxy.org/', use: 'Optional request inspection' },
  { name: 'igmpproxy', url: 'https://github.com/pali/igmpproxy', use: 'IPTV multicast routing' },
  { name: 'iperf3', url: 'https://software.es.net/iperf/', use: 'Optional LAN throughput test' },
  { name: 'NextTrace', url: 'https://github.com/nxtrace/NTrace-core', use: 'Traceroute and route maps' },
  { name: 'DB-IP Lite', url: 'https://db-ip.com/db/lite.php', use: 'Remote peer geolocation' },
  { name: 'IEEE OUI registry', url: 'https://standards-oui.ieee.org/', use: 'Device vendor identification' },
  { name: 'ddns-go', url: 'https://github.com/jeessy2/ddns-go', use: 'Ported dynamic DNS provider code' },
]

const go: Credit[] = [
  { name: 'go-systemd', url: 'https://github.com/coreos/go-systemd', use: 'D-Bus service management' },
  { name: 'google/nftables', url: 'https://github.com/google/nftables', use: 'Direct netfilter rules' },
  { name: 'vishvananda/netlink', url: 'https://github.com/vishvananda/netlink', use: 'Network configuration' },
  { name: 'tun2socks', url: 'https://github.com/xjasonlyu/tun2socks', use: 'Proxy-backed exits' },
  { name: 'pro-bing', url: 'https://github.com/prometheus-community/pro-bing', use: 'Ping diagnostics' },
  { name: 'speedtest-go', url: 'https://github.com/showwin/speedtest-go', use: 'Internet speed tests' },
  { name: 'Cobra', url: 'https://github.com/spf13/cobra', use: 'Command-line interface' },
  { name: 'pflag', url: 'https://github.com/spf13/pflag', use: 'Command-line flags' },
  { name: 'invopop/jsonschema', url: 'https://github.com/invopop/jsonschema', use: 'API and MCP schemas' },
  { name: 'golang.org/x/net', url: 'https://pkg.go.dev/golang.org/x/net', use: 'DNS messages and network protocols' },
  { name: 'golang.org/x/sys', url: 'https://pkg.go.dev/golang.org/x/sys', use: 'Linux system calls' },
]

const web: Credit[] = [
  { name: 'React & React DOM', url: 'https://react.dev/', use: 'Interface rendering' },
  { name: 'React Router', url: 'https://reactrouter.com/', use: 'Navigation' },
  { name: 'TanStack Query', url: 'https://tanstack.com/query', use: 'Server state' },
  { name: 'Base UI', url: 'https://base-ui.com/', use: 'Accessible controls' },
  { name: 'shadcn', url: 'https://ui.shadcn.com/', use: 'Component styles' },
  { name: 'Tailwind CSS & Vite plugin', url: 'https://tailwindcss.com/', use: 'Styling' },
  { name: 'Geist', url: 'https://vercel.com/font', use: 'Typography' },
  { name: 'Lucide', url: 'https://lucide.dev/', use: 'Icons' },
  { name: 'Motion', url: 'https://motion.dev/', use: 'Topology animations' },
  { name: '@paulmillr/qr', url: 'https://github.com/paulmillr/qr', use: 'Client QR codes' },
  { name: 'next-themes', url: 'https://github.com/pacocoursey/next-themes', use: 'Theme preference' },
  { name: 'Sonner', url: 'https://sonner.emilkowal.ski/', use: 'Notifications' },
  { name: 'class-variance-authority, clsx & tailwind-merge', url: 'https://github.com/joe-bell/cva', use: 'Component variants and classes' },
  { name: 'tw-animate-css', url: 'https://github.com/Wombosvideo/tw-animate-css', use: 'UI animations' },
  { name: 'Vite, TypeScript & plugin-react', url: 'https://vite.dev/', use: 'Web build' },
  { name: 'Oxlint', url: 'https://oxc.rs/docs/guide/usage/linter', use: 'Linting' },
  { name: 'json-schema-to-typescript', url: 'https://github.com/bcherny/json-schema-to-typescript', use: 'Generated API types' },
  { name: '@types/node, @types/react & @types/react-dom', url: 'https://www.typescriptlang.org/docs/handbook/declaration-files/introduction.html', use: 'Development type definitions' },
]

function CreditSection({ title, description, items }: { title: string; description: string; items: Credit[] }) {
  return (
    <section className="space-y-4" aria-label={title}>
      <div>
        <h2 className="text-lg font-semibold tracking-tight">{title}</h2>
        <p className="text-sm text-muted-foreground">{description}</p>
      </div>
      <ul className="grid gap-x-8 sm:grid-cols-2 xl:grid-cols-3">
        {items.map((item) => (
          <li key={item.name} className="border-t border-border/60 py-3">
            <a href={item.url} target="_blank" rel="noreferrer" className="font-medium underline-offset-4 hover:underline">{item.name}</a>
            <p className="mt-1 text-sm text-muted-foreground">{item.use}</p>
          </li>
        ))}
      </ul>
    </section>
  )
}

export function CreditsPage() {
  return (
    <div className="mx-auto max-w-5xl space-y-12 pb-12">
      <div className="space-y-3">
        <Link to="/" className="text-sm text-muted-foreground underline-offset-4 hover:text-foreground hover:underline">Back to Overview</Link>
        <h1 className="text-3xl font-semibold tracking-tight">Built on open work</h1>
        <p className="max-w-2xl text-muted-foreground">Open Linux Router coordinates Linux services and open-source libraries rather than replacing them. Thank you to their maintainers and contributors.</p>
        <p className="text-sm text-muted-foreground">Feature backends may be optional or installed separately. Package lists cover direct dependencies; their transitive dependencies and license details are in the project manifests.</p>
      </div>
      <CreditSection title="Feature backends & data" description="The tools and projects behind router features." items={backends} />
      <CreditSection title="Go dependencies" description="Direct modules used by the daemon and CLI." items={go} />
      <CreditSection title="Web dependencies & tooling" description="Direct packages behind the interface and its build." items={web} />
      <p className="text-sm text-muted-foreground">See <a className="underline underline-offset-4" href="https://github.com/open-linux-router/open-linux-router/blob/main/go.mod" target="_blank" rel="noreferrer">go.mod</a> and <a className="underline underline-offset-4" href="https://github.com/open-linux-router/open-linux-router/blob/main/web/package.json" target="_blank" rel="noreferrer">web/package.json</a> for the current dependency lists.</p>
    </div>
  )
}
