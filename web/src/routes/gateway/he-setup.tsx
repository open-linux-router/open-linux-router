import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router'
import { PageHeading } from '@/components/layout/page-heading'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { useUplink } from '@/features/dial/queries'
import { ApiError, api, configPatch } from '@/lib/api'
import type { DialApplyResult, Plan, Uplink } from '@/lib/api-types'
import type { DialConfig, LinkConfig, DhcpConfig, GatewayConfig } from '@/lib/config-types'

type Server = { region: string; name: string; address: string }
type Measurement = {
  sent: number
  received: number
  loss: number
  avg_ms: number
  variation_ms: number
  as_of: string
}
type Draft = {
  step: number
  server: string
  address: string
  mtu: string
  upstream: boolean
  network?: string
  subnet?: string
  router?: string
}
type SetupPlan = {
  impact: string
  reasons?: string[]
  warnings?: { message: string }[]
  diff?: string
  changes: { diff?: string }[]
  blocked?: string
  known?: boolean
}
type NetworkChange = { module: string; body: object; plan: SetupPlan }
type ApplySteps = {
  error?: { message: string }
  steps?: { description: string; done?: boolean; error?: string }[]
}
const steps = [
  'Public IPv4',
  'Upstream router',
  'Choose a server',
  'Create HE tunnel',
  'Connect this router',
  'Connect your networks',
  'Finish',
]
const initial: Draft = { step: 0, server: '', address: '', mtu: '1480', upstream: false }
function restore(): Draft {
  try {
    const d = JSON.parse(localStorage.getItem('olr.he-setup') ?? 'null')
    if (
      d &&
      Number.isInteger(d.step) &&
      d.step >= 0 &&
      d.step < steps.length &&
      typeof d.server === 'string' &&
      typeof d.address === 'string' &&
      typeof d.mtu === 'string'
    )
      return { ...initial, ...d }
  } catch {
    /* Storage can be unavailable. */
  }
  return initial
}

export function HESetupPage() {
  const [draft, setDraft] = useState(restore)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [publicIP, setPublicIP] = useState('')
  const [measurements, setMeasurements] = useState<Record<string, Measurement | string>>({})
  const [progress, setProgress] = useState('')
  const [preview, setPreview] = useState<{
    plan: Plan
    patch: { uplink: { ipv6: Uplink['ipv6'] } }
  } | null>(null)
  const [result, setResult] = useState<DialApplyResult | null>(null)
  const [verified, setVerified] = useState(false)
  const [networkReport, setNetworkReport] = useState<string[]>([])
  const network = draft.network ?? ''
  const subnet = draft.subnet ?? ''
  const router = draft.router ?? ''
  const [networkChange, setNetworkChange] = useState<NetworkChange | null>(null)
  const [networkResult, setNetworkResult] = useState<ApplySteps | null>(null)
  const links = useQuery({
    queryKey: ['link', 'config'],
    queryFn: () => api.get<LinkConfig>('/api/link/config'),
  })
  const stop = useRef(false)
  const uplink = useUplink()
  const servers = useQuery({
    queryKey: ['dial', 'he', 'servers'],
    queryFn: () => api.get<Server[]>('/api/dial/he/servers'),
  })
  useEffect(() => {
    try {
      localStorage.setItem('olr.he-setup', JSON.stringify(draft))
    } catch {
      /* optional */
    }
  }, [draft])
  useEffect(
    () => () => {
      stop.current = true
    },
    [],
  )
  const update = (patch: Partial<Draft>) => {
    setDraft((d) => ({ ...d, ...patch }))
    setPreview(null)
    setVerified(false)
  }
  const u = uplink.data?.uplink
  const localAddresses = u?.addresses ?? []
  const direct = publicIP && localAddresses.some((a) => a.split('/')[0] === publicIP)
  const chosen = servers.data?.find((s) => s.address === draft.server && s.address)
  async function run(task: () => Promise<void>) {
    setBusy(true)
    setError('')
    try {
      await task()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
      setProgress('')
    }
  }
  async function measure() {
    stop.current = false
    setMeasurements({})
    for (const s of servers.data ?? []) {
      if (stop.current) break
      if (!s.address) continue
      setProgress(`Measuring ${s.name} · ${s.address}`)
      try {
        const m = await api.post<Measurement>('/api/dial/he/check', { target: s.address })
        if (!stop.current) setMeasurements((old) => ({ ...old, [s.address]: m }))
      } catch (e) {
        if (!stop.current)
          setMeasurements((old) => ({
            ...old,
            [s.address]: e instanceof Error ? e.message : String(e),
          }))
      }
    }
  }
  const ranked = (servers.data ?? [])
    .filter((s) => {
      const m = measurements[s.address]
      return typeof m === 'object' && m.received > 0
    })
    .sort((a, b) => {
      const x = measurements[a.address] as Measurement,
        y = measurements[b.address] as Measurement
      return x.loss - y.loss || x.avg_ms - y.avg_ms || x.variation_ms - y.variation_ms
    })
  async function review() {
    const cfg = await api.get<DialConfig & { uplink?: Uplink }>('/api/dial/config')
    if (!cfg.uplink) throw new Error('Set up an uplink in Gateway first.')
    const next = {
      ...cfg.uplink,
      ipv6: {
        via: '6in4' as const,
        server: draft.server,
        address: draft.address.trim(),
        mtu: Number(draft.mtu),
      },
    }
    const plan = await api.post<Plan>('/api/dial/plan', { ...cfg, uplink: next })
    setPreview({ plan, patch: configPatch(cfg, { uplink: { ipv6: next.ipv6 } }) })
  }
  async function apply() {
    if (!preview) return
    setResult(null)
    try {
      const applied = await api.patch<DialApplyResult>(
        '/api/dial/config?confirm=true',
        preview.patch,
      )
      setResult(applied)
      if (applied.error || applied.steps?.some((s) => s.error))
        throw new Error(
          'Only part of the change completed. Review the steps below before retrying.',
        )
      setPreview(null)
      await uplink.refetch()
    } catch (e) {
      if (e instanceof ApiError && e.body) setResult(e.body as DialApplyResult)
      throw e
    }
  }
  async function verifyTunnel() {
    setVerified(false)
    const latest = await uplink.refetch()
    if (latest.error) throw latest.error
    const t = latest.data?.uplink?.tunnel
    if (
      !t?.present ||
      !t.up ||
      t.route_dev !== t.interface ||
      t.server !== draft.server ||
      t.address !== draft.address.trim() ||
      t.mtu !== Number(draft.mtu)
    )
      throw new Error(
        'The running tunnel does not match these settings, or its IPv6 default route is missing. Apply the settings and retry.',
      )
    const peer = t.address.split('/')[0].replace(/::2$/, '::1')
    if (peer === t.address.split('/')[0])
      throw new Error(
        'Automatic peer testing expects HE’s client address ending in ::2. Check the address against the HE details page.',
      )
    for (const target of [peer, '2606:4700:4700::1111', '2001:4860:4860::8888']) {
      setProgress(`Testing IPv6 ${target}`)
      const m = await api.post<Measurement>('/api/dial/he/check', { target })
      if (!m.received)
        throw new Error(
          `No IPv6 reply from ${target}. Check HE’s client IPv4, upstream protocol 41 forwarding and firewall. No reply alone does not identify which one is responsible.`,
        )
    }
    setVerified(true)
  }
  async function checkNetworks() {
    const [link, dhcp, gateway] = await Promise.all([
      api.get<LinkConfig>('/api/link/config'),
      api.get<DhcpConfig>('/api/dhcp/config'),
      api.get<GatewayConfig>('/api/gateway/config'),
    ])
    const lines = (link.networks ?? [])
      .filter((n) => n.ipv6?.subnet)
      .map((n) => {
        const pool = dhcp.pools?.find((p) => p.network === n.name)
        return `${n.name}: ${n.ipv6?.subnet}; router ${n.ipv6?.router ?? 'automatic'}; advertisements ${pool?.ipv6?.mode ?? 'off'}; advertised MTU ${pool?.ipv6?.mtu ?? 'default'}.`
      })
    setNetworkReport([
      `Saved IPv6 forwarding: ${gateway.ipv6_forwarding ? 'on' : 'off'}.`,
      ...lines,
      'These are saved settings, not proof that a client can reach the internet.',
    ])
  }
  async function reviewNetwork(module: string) {
    setNetworkChange(null)
    if (!network) throw new Error('Choose a LAN network first.')
    let body: object
    if (module === 'link') {
      if (!subnet.trim().endsWith('/64') || !router.trim())
        throw new Error('Enter a Routed /64 and this router’s address within it.')
      const tunnel = new URL(`http://[${draft.address.split('/')[0]}]`).hostname
      const lan = new URL(`http://[${subnet.trim().split('/')[0]}]`).hostname
      // HE assigns the tunnel client ::2; normalize both network addresses before comparison.
      const tunnelNetwork = new URL(`http://[${tunnel.slice(1, -1).replace(/::2$/, '::')}]`)
        .hostname
      if (tunnelNetwork === lan) throw new Error('Use HE’s Routed /64, not the tunnel’s own /64.')
      const cfg = await api.get<LinkConfig>('/api/link/config')
      if (!cfg.networks?.some((n) => n.name === network))
        throw new Error('This network no longer exists. Reload and choose another.')
      body = configPatch(cfg, {
        networks: cfg.networks.map((n) =>
          n.name === network ? { ...n, ipv6: { subnet: subnet.trim(), router: router.trim() } } : n,
        ),
      })
    } else if (module === 'dhcp') {
      const cfg = await api.get<DhcpConfig>('/api/dhcp/config')
      const pool = cfg.pools?.find((p) => p.network === network)
      const ipv6 = { mode: 'slaac' as const, mtu: Number(draft.mtu) }
      body = configPatch(cfg, {
        enabled: true,
        pools: pool
          ? cfg.pools!.map((p) => (p.network === network ? { ...p, ipv6 } : p))
          : [...(cfg.pools ?? []), { network, ipv6 }],
      })
    } else {
      const cfg = await api.get<GatewayConfig>('/api/gateway/config')
      body = configPatch(cfg, { enabled: true, ipv6_forwarding: true })
    }
    const current = await api.get<object>(`/api/${module}/config`)
    const plan = await api.post<SetupPlan>(`/api/${module}/plan`, { ...current, ...body })
    if (plan.blocked || plan.known === false)
      throw new Error(
        plan.blocked ??
          'The router could not read its current routing state. Retry before applying.',
      )
    setNetworkChange({ module, body, plan })
  }
  async function applyNetwork() {
    if (!networkChange) return
    const change = networkChange
    setNetworkResult(null)
    let response: ApplySteps
    try {
      response = await api.patch<ApplySteps>(
        `/api/${change.module}/config?confirm=true`,
        change.body,
      )
      setNetworkResult(response)
    } catch (e) {
      if (e instanceof ApiError && e.body) setNetworkResult(e.body as ApplySteps)
      throw e
    }
    if (response.error || response.steps?.some((s) => s.error))
      throw new Error(
        response.error?.message ??
          'Some network changes failed. Inspect current settings before retrying.',
      )
    setNetworkChange(null)
    await links.refetch()
    await checkNetworks()
  }
  const canContinue =
    draft.step === 0
      ? Boolean(publicIP && u)
      : draft.step === 1
        ? Boolean(direct || draft.upstream)
        : draft.step === 2
          ? Boolean(chosen)
          : draft.step === 3
            ? Boolean(
                draft.address.trim() && Number(draft.mtu) >= 1280 && Number(draft.mtu) <= 1480,
              )
            : draft.step === 4
              ? verified
              : true
  return (
    <div className="space-y-8">
      <PageHeading
        title="Set up HE IPv6"
        back={{ to: '/gateway/interfaces', label: 'Gateway interfaces' }}
        description="Connect through Hurricane Electric Tunnel Broker, then bring IPv6 to your networks."
      />
      <nav aria-label="Setup progress" className="flex flex-wrap gap-2">
        {steps.map((name, i) => (
          <span
            key={name}
            aria-current={i === draft.step ? 'step' : undefined}
            className={`rounded-md px-3 py-2 text-sm ${i === draft.step ? 'bg-muted font-medium' : 'text-muted-foreground'}`}
          >
            {i + 1}. {name}
          </span>
        ))}
      </nav>
      <Card>
        <CardHeader>
          <CardTitle>
            <h2>
              {draft.step + 1}. {steps[draft.step]}
            </h2>
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {draft.step === 0 && (
            <>
              <p>
                Check the public IPv4 seen through the configured uplink. Use this address as HE’s
                IPv4 endpoint.
              </p>
              {uplink.isPending ? (
                <p>Reading uplink…</p>
              ) : uplink.error ? (
                <p role="alert">{uplink.error.message}</p>
              ) : !u ? (
                <p>
                  First{' '}
                  <Link className="text-primary underline" to="/gateway/interfaces">
                    set up the uplink
                  </Link>
                  , then return here.
                </p>
              ) : (
                <p className="break-all font-mono">
                  {u.interface}: {localAddresses.join(', ') || 'No observed IPv4 address'}
                </p>
              )}
              <Button
                disabled={busy || !u}
                onClick={() =>
                  void run(async () => {
                    setPublicIP('')
                    const r = await api.post<{ public_ipv4: string }>('/api/dial/he/check', {})
                    setPublicIP(r.public_ipv4)
                  })
                }
              >
                Check public IPv4
              </Button>
              {publicIP && (
                <>
                  <p className="break-all font-mono">Public IPv4: {publicIP}</p>
                  <p>
                    {direct
                      ? 'The uplink holds this public address.'
                      : 'The uplink and public address differ. An upstream router or NAT is present; inspect its WAN address to confirm you control the public endpoint.'}
                  </p>
                </>
              )}
              <p className="text-sm text-muted-foreground">
                HE may display a different address if your browser uses a VPN or proxy. Use the
                router’s measured address.
              </p>
            </>
          )}
          {draft.step === 1 && (
            <>
              <p>
                {direct
                  ? 'Your uplink directly holds the public IPv4. Check that its firewall permits HE’s protocol 41 traffic.'
                  : `On the upstream router, reserve OLR’s uplink IPv4 (${localAddresses.join(', ') || 'see Gateway'}). Forward IP protocol 41 to it, or set it as the DMZ host.`}
              </p>
              <p>
                TCP/UDP port 41 forwarding does not work. If the upstream WAN has a private or CGNAT
                address, ask your ISP for a public IPv4 or bridge the connection where appropriate.
              </p>
              <p>
                DMZ sends otherwise unmatched inbound traffic to OLR. Keep its firewall enabled and
                its administration services private. HE’s endpoint check may also require allowing
                WAN ping on the upstream router.
              </p>
              {!direct && (
                <label className="flex min-h-11 items-center gap-3">
                  <input
                    type="checkbox"
                    checked={draft.upstream}
                    onChange={(e) => update({ upstream: e.target.checked })}
                  />
                  I confirmed the public WAN address and configured upstream forwarding.
                </label>
              )}
              <p className="text-sm text-muted-foreground">
                This is your confirmation. Protocol 41 is verified by an IPv6 round trip after the
                tunnel is configured.
              </p>
            </>
          )}
          {draft.step === 2 && (
            <>
              <p>
                Measure from this router’s uplink. Recommendations prefer lower observed loss, then
                average latency and variation. You choose the server; OLR never switches it
                automatically.
              </p>
              <div className="flex flex-wrap gap-2">
                <Button disabled={busy || !servers.data} onClick={() => void run(measure)}>
                  Measure all servers
                </Button>
                {busy && (
                  <Button
                    variant="outline"
                    onClick={() => {
                      stop.current = true
                    }}
                  >
                    Stop after current test
                  </Button>
                )}
              </div>
              <p className="text-sm text-muted-foreground">
                Eight probes per server; about two minutes for the full list. IPv4 measurements
                cannot prove tunnel availability or IPv6 performance. Repeat at busy hours to assess
                stability.
              </p>
              {ranked.length > 0 && (
                <p>
                  Best measured candidate: {ranked[0].name} ({ranked[0].address}).{' '}
                  {busy ? 'Measurements are still running.' : 'Based on this test only.'}
                </p>
              )}
              {servers.isPending && <p>Loading servers…</p>}
              {servers.error && (
                <p role="alert">
                  {servers.error.message}{' '}
                  <Button variant="outline" onClick={() => void servers.refetch()}>
                    Retry
                  </Button>
                </p>
              )}
              <div className="divide-y">
                {(servers.data ?? []).map((s) => {
                  const m = measurements[s.address]
                  return (
                    <label
                      key={`${s.name}-${s.address}`}
                      className="flex min-h-16 cursor-pointer items-start gap-3 py-3"
                    >
                      <input
                        className="mt-1"
                        type="radio"
                        name="he-server"
                        disabled={!s.address || busy}
                        checked={Boolean(s.address) && draft.server === s.address}
                        onChange={() => update({ server: s.address })}
                      />
                      <span className="min-w-0 flex-1">
                        <span className="block">
                          {s.name}{' '}
                          <span className="text-sm text-muted-foreground">· {s.region}</span>
                        </span>
                        <span className="break-all font-mono text-sm">
                          {s.address || 'Full when catalog was recorded'}
                        </span>
                        <span className="block text-sm text-muted-foreground">
                          {typeof m === 'string'
                            ? `Unable to measure: ${m}`
                            : m
                              ? m.received
                                ? `${m.avg_ms.toFixed(1)} ms average · ${m.loss.toFixed(0)}% loss · ${m.variation_ms.toFixed(1)} ms variation · ${new Date(m.as_of).toLocaleTimeString()}`
                                : 'No replies — availability unknown'
                              : 'Not measured'}
                        </span>
                      </span>
                    </label>
                  )
                })}
              </div>
              <p className="text-sm text-muted-foreground">
                Catalog: October 15, 2026. HE’s creation page is authoritative for current capacity.
              </p>
            </>
          )}
          {draft.step === 3 && (
            <>
              <p>
                On{' '}
                <a
                  className="text-primary underline"
                  href="https://tunnelbroker.net/new_tunnel.php"
                  target="_blank"
                  rel="noreferrer"
                >
                  HE’s Create Regular Tunnel page
                </a>
                , enter {publicIP || 'the public IPv4 checked in step 1'} and choose {chosen?.name}{' '}
                ({draft.server}).
              </p>
              <p>
                Copy the Client IPv6 Address below, including /64. Keep the separate Routed /64 or
                Routed /48 for your LAN networks.
              </p>
              <div className="max-w-xl space-y-2">
                <Label htmlFor="he-address">Client IPv6 Address</Label>
                <Input
                  id="he-address"
                  value={draft.address}
                  placeholder="2001:db8:1234::2/64"
                  onChange={(e) => update({ address: e.target.value })}
                />
              </div>
              <div className="max-w-xl space-y-2">
                <Label htmlFor="he-mtu">Tunnel MTU</Label>
                <Input
                  id="he-mtu"
                  type="number"
                  min={1280}
                  max={1480}
                  value={draft.mtu}
                  onChange={(e) => update({ mtu: e.target.value })}
                />
                <p className="text-sm text-muted-foreground">
                  1480 for a 1500-byte uplink; 1472 for a 1492-byte PPPoE path. Minimum 1280.
                </p>
              </div>
            </>
          )}
          {draft.step === 4 && (
            <>
              <p>
                Preview the change before applying. It may replace the current IPv6 default route
                and interrupt IPv6 connections. Automatic rollback is unavailable.
              </p>
              <Button disabled={busy} onClick={() => void run(review)}>
                Preview tunnel settings
              </Button>
              {preview && (
                <div className="space-y-3">
                  {preview.plan.warnings?.map((w) => (
                    <p key={w.path + w.message} className="text-warning-foreground">
                      {w.message}
                    </p>
                  ))}
                  <pre className="max-h-80 overflow-auto whitespace-pre-wrap break-all text-xs">
                    {preview.plan.changes.map((c) => c.diff).join('\n') ||
                      'No configuration changes.'}
                  </pre>
                  <Button disabled={busy} onClick={() => void run(apply)}>
                    Apply these settings
                  </Button>
                </div>
              )}
              {result && (
                <div className="space-y-2">
                  <p>
                    {result.error
                      ? 'Apply failed; some steps may already have taken effect.'
                      : 'Apply response received. Verify the running tunnel below.'}
                  </p>
                  {result.steps?.map((s) => (
                    <p key={s.description} className="break-words text-sm">
                      {s.done ? 'Completed' : 'Incomplete'}: {s.description}
                      {s.error ? ` — ${s.error}` : ''}
                    </p>
                  ))}
                </div>
              )}
              <Button variant="outline" disabled={busy} onClick={() => void run(verifyTunnel)}>
                Verify tunnel and public IPv6
              </Button>
              {verified && (
                <p className="text-success-foreground" role="status">
                  Verified now: matching tunnel, IPv6 default route, HE peer and two public IPv6
                  destinations responded.
                </p>
              )}
              <p className="text-sm text-muted-foreground">
                ICMP checks verify round trips through the tunnel. They do not verify HTTPS, DNS or
                large transfers.
              </p>
            </>
          )}
          {draft.step === 5 && (
            <>
              {networkResult?.steps?.map((s) => (
                <p className="break-words text-sm" key={s.description}>
                  {s.done ? 'Completed' : 'Incomplete'}: {s.description}
                  {s.error ? ` — ${s.error}` : ''}
                </p>
              ))}
              <p>
                For each LAN, use HE’s Routed /64, or one /64 from your Routed /48. Set the router
                address within that subnet. Do not reuse the tunnel’s own /64.
              </p>
              <div className="max-w-xl space-y-4">
                <div className="space-y-2">
                  <Label htmlFor="he-network">LAN network</Label>
                  <Select
                    value={network}
                    onValueChange={(value) => {
                      update({ network: value ?? '' })
                      setNetworkChange(null)
                    }}
                  >
                    <SelectTrigger id="he-network" className="w-full">
                      <SelectValue placeholder="Choose a network" />
                    </SelectTrigger>
                    <SelectContent>
                      {links.data?.networks?.map((n) => (
                        <SelectItem key={n.name} value={n.name}>
                          {n.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                {links.error && <p role="alert">{links.error.message}</p>}
                <div className="space-y-2">
                  <Label htmlFor="he-subnet">HE Routed /64</Label>
                  <Input
                    id="he-subnet"
                    placeholder="2001:db8:5678::/64"
                    value={subnet}
                    onChange={(e) => {
                      update({ subnet: e.target.value })
                      setNetworkChange(null)
                    }}
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="he-lan-router">Router IPv6 address (without /64)</Label>
                  <Input
                    id="he-lan-router"
                    placeholder="2001:db8:5678::1"
                    value={router}
                    onChange={(e) => {
                      update({ router: e.target.value })
                      setNetworkChange(null)
                    }}
                  />
                </div>
              </div>
              <p>
                Apply these in order. Addressing changes can interrupt clients; enabling
                advertisements starts DHCP service if disabled, and enabling forwarding starts
                Gateway routing if disabled. Each action has its own preview. Completed changes are
                not automatically rolled back if a later action fails.
              </p>
              <div className="flex flex-wrap gap-2">
                <Button
                  disabled={busy || !network}
                  onClick={() => void run(() => reviewNetwork('link'))}
                >
                  1. Preview LAN address
                </Button>
                <Button
                  variant="outline"
                  disabled={busy || !network}
                  onClick={() => void run(() => reviewNetwork('dhcp'))}
                >
                  2. Preview SLAAC and DNS
                </Button>
                <Button
                  variant="outline"
                  disabled={busy || !network}
                  onClick={() => void run(() => reviewNetwork('gateway'))}
                >
                  3. Preview IPv6 forwarding
                </Button>
              </div>
              {networkChange && (
                <div className="space-y-3">
                  <p>Expected impact: {networkChange.plan.impact}.</p>
                  {networkChange.plan.reasons?.map((reason) => (
                    <p key={reason}>{reason}</p>
                  ))}
                  {networkChange.plan.warnings?.map((w) => (
                    <p className="text-warning-foreground" key={w.message}>
                      {w.message}
                    </p>
                  ))}
                  <pre className="max-h-80 overflow-auto whitespace-pre-wrap break-all text-xs">
                    {networkChange.plan.diff ||
                      networkChange.plan.changes
                        .map((c) => c.diff)
                        .filter(Boolean)
                        .join('\n') ||
                      'No configuration changes.'}
                  </pre>
                  <Button disabled={busy} onClick={() => void run(applyNetwork)}>
                    Apply {networkChange.module} changes
                  </Button>
                  <Button variant="outline" disabled={busy} onClick={() => setNetworkChange(null)}>
                    Cancel
                  </Button>
                </div>
              )}
              <p>
                <Link className="text-primary underline" to="/gateway/interfaces">
                  Configure network IPv6 and Gateway forwarding
                </Link>
                , then{' '}
                <Link className="text-primary underline" to="/gateway/dhcp/ranges">
                  enable SLAAC and DNS advertisements
                </Link>
                . Advertise the tunnel MTU ({draft.mtu}). Your draft is saved when you leave.
              </p>
              <Button disabled={busy} onClick={() => void run(checkNetworks)}>
                Check saved network settings
              </Button>
              {networkReport.map((line) => (
                <p className="break-words text-sm" key={line}>
                  {line}
                </p>
              ))}
              <p>
                Check IPv6 firewall rules allow LAN outbound traffic and necessary ICMPv6, while
                blocking unsolicited inbound connections.
              </p>
            </>
          )}
          {draft.step === 6 && (
            <>
              <p>
                Reconnect a LAN client. Confirm it receives an address from HE’s Routed prefix, an
                IPv6 default route and DNS. Open an IPv6 test site from that client, then test HTTPS
                and a large download.
              </p>
              <a
                className="inline-flex min-h-11 items-center text-primary underline"
                href="https://test-ipv6.com/"
                target="_blank"
                rel="noreferrer"
              >
                Test this client’s IPv6
              </a>
              <p>
                This client test is manual. Router probes do not prove a downstream client’s
                connectivity. If ping works but downloads stall, check MTU and ICMPv6.
              </p>
              <p>
                If your public IPv4 changes, update HE’s Client IPv4 endpoint. Ordinary domain DDNS
                does not update it. Finally, restart OLR during a suitable window and repeat the
                router and client tests.
              </p>
              <p>
                Server selection stays under your control. Changing servers can change your IPv6
                prefix and require renumbering LAN networks.
              </p>
              <Button
                variant="outline"
                onClick={() => {
                  setDraft(initial)
                  setPublicIP('')
                  setVerified(false)
                  setMeasurements({})
                  setNetworkReport([])
                  setResult(null)
                  setError('')
                }}
              >
                Start a new setup
              </Button>
            </>
          )}
          {progress && <p role="status">{progress}</p>}
          {error && (
            <p role="alert" className="break-words text-destructive">
              {error}
            </p>
          )}
          <div className="flex justify-between gap-3 pt-4">
            <Button
              variant="outline"
              disabled={busy || draft.step === 0}
              onClick={() => update({ step: draft.step - 1 })}
            >
              Back
            </Button>
            {draft.step < steps.length - 1 && (
              <Button
                disabled={busy || !canContinue}
                onClick={() => setDraft((d) => ({ ...d, step: d.step + 1 }))}
              >
                Continue
              </Button>
            )}
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
