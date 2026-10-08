import { ArrowDownLeft, ArrowUpRight, Cable, ChevronRight, LockKeyhole, Network, Route, Server, Waypoints } from 'lucide-react'
import { Link } from 'react-router'

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'

type Choice = {
  title: string
  detail: string
  icon: typeof Network
  to?: string
  note?: string
}

const outward: Choice[] = [
  {
    title: 'Use another gateway',
    detail: 'Send selected networks or devices through another router on your LAN.',
    icon: Route,
    to: '/gateway/exits',
  },
  {
    title: 'Use a VPN connection',
    detail: 'Route through an existing tunnel interface, such as WireGuard or Tailscale. OLR does not set up the outbound tunnel yet.',
    icon: Network,
    to: '/gateway/exits',
  },
  {
    title: 'Use a SOCKS5 proxy',
    detail: 'An application can use a proxy directly. Routing a whole LAN through one needs a separate TUN or transparent proxy, which OLR does not create yet.',
    icon: Waypoints,
    note: 'Not yet configurable here',
  },
]

const inward: Choice[] = [
  {
    title: 'Connect my phone or laptop',
    detail: 'Create a WireGuard device configuration to reach your home network from outside.',
    icon: LockKeyhole,
    to: '/advanced/remote',
  },
  {
    title: 'Publish a service',
    detail: 'Give a web service at home a public HTTPS address. Anyone who can reach it still needs the service\'s own access controls.',
    icon: Server,
    to: '/advanced/ingress',
  },
]

export function AccessPage() {
  return (
    <div className="space-y-6">
      <div className="rounded-2xl border bg-gradient-to-br from-primary/10 via-background to-muted/70 px-5 py-6 sm:px-7">
        <p className="text-xs font-semibold tracking-[0.18em] text-primary uppercase">Choose a direction</p>
        <h2 className="mt-2 font-heading text-2xl font-semibold tracking-tight sm:text-3xl">Where do you want to go?</h2>
        <p className="mt-2 max-w-2xl text-sm text-muted-foreground">
          Start with what you want to reach. OLR keeps the routing, tunnel, and publishing settings in their own pages.
        </p>
      </div>

      <div className="grid gap-5 lg:grid-cols-2">
        <Direction icon={ArrowUpRight} label="Inside → outside" title="From home to another network" description="Choose how devices at home reach something elsewhere." choices={outward} />
        <Direction icon={ArrowDownLeft} label="Outside → inside" title="From outside back home" description="Connect your own device, or make one service publicly reachable." choices={inward} />
      </div>

      <Card className="bg-muted/40">
        <CardHeader>
          <div className="flex items-center gap-2 text-xs font-semibold tracking-wide text-muted-foreground uppercase">
            <Cable className="size-4" aria-hidden /> Both directions · later
          </div>
          <CardTitle>Connect two homes</CardTitle>
          <CardDescription>
            Two OLR routers could share selected networks in both directions. Automatic pairing, routes, and access rules are not supported yet; this is not a phone-style WireGuard peer.
          </CardDescription>
        </CardHeader>
      </Card>
    </div>
  )
}

function Direction({ icon: Icon, label, title, description, choices }: {
  icon: typeof Network
  label: string
  title: string
  description: string
  choices: Choice[]
}) {
  return (
    <section className="space-y-3" aria-label={label}>
      <div className="flex items-start gap-3 px-1">
        <span className="flex size-9 shrink-0 items-center justify-center rounded-xl bg-primary/10 text-primary"><Icon className="size-5" aria-hidden /></span>
        <div>
          <p className="text-xs font-semibold tracking-wide text-primary uppercase">{label}</p>
          <h2 className="font-heading text-lg font-semibold">{title}</h2>
          <p className="text-sm text-muted-foreground">{description}</p>
        </div>
      </div>
      <div className="space-y-2">
        {choices.map((choice) => <ChoiceCard key={choice.title} {...choice} />)}
      </div>
    </section>
  )
}

function ChoiceCard({ title, detail, icon: Icon, to, note }: Choice) {
  const content = (
    <Card className={cn('h-full transition-colors', to && 'hover:bg-accent/50')}>
      <CardContent className="flex items-start gap-3">
        <Icon className="mt-0.5 size-4 shrink-0 text-primary" aria-hidden />
        <div className="min-w-0 flex-1 space-y-1">
          <h3 className="text-sm font-semibold">{title}</h3>
          <p className="text-sm text-muted-foreground">{detail}</p>
          {note && <p className="text-xs font-medium text-muted-foreground">{note}</p>}
        </div>
        {to && <ChevronRight className="mt-0.5 size-4 shrink-0 text-muted-foreground" aria-hidden />}
      </CardContent>
    </Card>
  )
  return to ? <Link to={to} className="block rounded-xl focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring">{content}</Link> : content
}
