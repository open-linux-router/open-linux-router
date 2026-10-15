import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Disclosure } from '@/components/ui/disclosure'
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
import { usePublicAddresses } from '@/features/system/queries'
import { ApiError, api, configPatch } from '@/lib/api'
import type { DialApplyResult, Plan, Uplink } from '@/lib/api-types'
import type {
  DialConfig,
  TunnelBroker,
  LinkConfig,
  DhcpConfig,
  GatewayConfig,
} from '@/lib/config-types'

type Config = DialConfig & { uplink?: Uplink }
type Server = { region: string; name: string; address: string }
type Measurement = {
  received: number
  loss: number
  avg_ms: number
  variation_ms: number
}
type TunnelStatus = {
  present: boolean
  up: boolean
  matches: boolean
  native_default_dev: string
}
type Draft = {
  tunnel_id: string
  server_ipv4: string
  server_ipv6: string
  client_ipv4: string
  client_ipv6: string
  routed_64: string
  routed_48: string
  mtu: string
}
const initial: Draft = {
  tunnel_id: '',
  server_ipv4: '',
  server_ipv6: '',
  client_ipv4: '',
  client_ipv6: '',
  routed_64: '',
  routed_48: '',
  mtu: '1480',
}
const sections = [
  'IPv4 endpoint',
  'Server',
  'Tunnel details',
  'Activation',
  'Networks',
]
const ids = ['endpoint', 'server', 'details', 'activation', 'networks']
function restore(): Draft {
  try {
    const d = JSON.parse(localStorage.getItem('olr.tunnelbroker') ?? 'null')
    if (d && Object.keys(initial).every((k) => typeof d[k] === 'string'))
      return d
  } catch {
    /* Optional draft storage. */
  }
  return initial
}
function fields(b: TunnelBroker): Draft {
  return {
    ...initial,
    ...b,
    tunnel_id: b.tunnel_id ?? '',
    server_ipv6: b.server_ipv6 ?? '',
    client_ipv4: b.client_ipv4 ?? '',
    routed_64: b.routed_64 ?? '',
    routed_48: b.routed_48 ?? '',
    mtu: String(b.mtu || 1480),
  }
}

export function TunnelBrokerPage() {
  const queryClient = useQueryClient()
  const config = useQuery({
    queryKey: ['dial', 'config'],
    queryFn: () => api.get<Config>('/api/dial/config'),
  })
  const status = useQuery({
    queryKey: ['dial', 'tunnelbroker', 'status'],
    queryFn: () => api.get<TunnelStatus>('/api/dial/tunnelbroker/status'),
    refetchInterval: 5000,
  })
  const servers = useQuery({
    queryKey: ['dial', 'he', 'servers'],
    queryFn: () => api.get<Server[]>('/api/dial/he/servers'),
  })
  const links = useQuery({
    queryKey: ['link', 'config'],
    queryFn: () => api.get<LinkConfig>('/api/link/config'),
  })
  const uplink = useUplink()
  const publicAddresses = usePublicAddresses()
  const [draft, setDraft] = useState(restore)
  const [active, setActive] = useState(0)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const [progress, setProgress] = useState('')
  const [measurements, setMeasurements] = useState<
    Record<string, Measurement | string>
  >({})
  const [preview, setPreview] = useState<{
    plan: Plan
    patch: object
    label: string
  } | null>(null)
  const [result, setResult] = useState<DialApplyResult | null>(null)
  const [network, setNetwork] = useState('')
  const [subnet, setSubnet] = useState('')
  const [router, setRouter] = useState('')
  const [networkChange, setNetworkChange] = useState<{
    module: string
    patch: object
    plan: Plan
  } | null>(null)
  const [networkResult, setNetworkResult] = useState<DialApplyResult | null>(
    null,
  )
  const hydrated = useRef(false)
  const stop = useRef(false)
  const saved = config.data?.tunnelbroker
  const legacy = config.data?.uplink?.ipv6?.via === '6in4'
  const publicIP = publicAddresses.data?.ipv4.ip ?? ''
  const u = uplink.data?.uplink
  const chosen = servers.data?.find(
    (s) => s.address === draft.server_ipv4 && s.address,
  )

  useEffect(() => {
    if (!config.data || hydrated.current) return
    hydrated.current = true
    if (config.data.tunnelbroker) setDraft(fields(config.data.tunnelbroker))
    else if (config.data.uplink?.ipv6?.via === '6in4') {
      const old = config.data.uplink.ipv6
      setDraft((d) => ({
        ...d,
        server_ipv4: old.server ?? '',
        client_ipv6: old.address ?? '',
        mtu: String(old.mtu || 1480),
      }))
    }
  }, [config.data])
  useEffect(() => {
    try {
      localStorage.setItem('olr.tunnelbroker', JSON.stringify(draft))
    } catch {
      /* Optional. */
    }
  }, [draft])
  useEffect(() => {
    const observer = new IntersectionObserver(
      (entries) => {
        const visible = entries
          .filter((e) => e.isIntersecting)
          .sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top)
        if (visible[0]) setActive(ids.indexOf(visible[0].target.id))
      },
      { rootMargin: '-128px 0px -50% 0px' },
    )
    ids.forEach((id) => {
      const el = document.getElementById(id)
      if (el) observer.observe(el)
    })
    return () => {
      observer.disconnect()
      stop.current = true
    }
  }, [])
  function update(key: keyof Draft, value: string) {
    setDraft((d) => ({ ...d, [key]: value }))
    setPreview(null)
    setMessage('')
  }
  async function run(task: () => Promise<void>) {
    setBusy(true)
    setError('')
    setMessage('')
    try {
      await task()
    } catch (e) {
      setError(
        e instanceof ApiError
          ? [e.message, ...e.problems.map((p) => p.message)].join(' · ')
          : e instanceof Error
            ? e.message
            : String(e),
      )
    } finally {
      setBusy(false)
      setProgress('')
    }
  }
  async function refresh() {
    await queryClient.invalidateQueries({ queryKey: ['dial'] })
  }
  async function commit(patch: object, label: string) {
    setResult(null)
    try {
      const response = await api.patch<DialApplyResult>(
        '/api/dial/config?confirm=true',
        patch,
      )
      setResult(response)
      if (response.error || response.steps?.some((s) => s.error))
        throw new Error(
          'Some changes failed. Review the completed steps before retrying.',
        )
      setPreview(null)
      setMessage(label)
    } catch (e) {
      if (e instanceof ApiError && e.body) setResult(e.body as DialApplyResult)
      throw e
    } finally {
      await refresh()
    }
  }
  async function propose(cfg: Config, change: object, label: string) {
    const patch = configPatch(cfg, change)
    const plan = await api.post<Plan>('/api/dial/plan', { ...cfg, ...change })
    if (plan.impact === 'disruptive') setPreview({ plan, patch, label })
    else await commit(patch, label)
  }
  async function save() {
    const cfg = await api.get<Config>('/api/dial/config')
    const broker: TunnelBroker = {
      enabled: cfg.tunnelbroker?.enabled ?? false,
      server_ipv4: draft.server_ipv4.trim(),
      client_ipv6: draft.client_ipv6.trim(),
      mtu: Number(draft.mtu),
      tunnel_id: draft.tunnel_id.trim() || undefined,
      server_ipv6: draft.server_ipv6.trim() || undefined,
      client_ipv4: draft.client_ipv4.trim() || undefined,
      routed_64: draft.routed_64.trim() || undefined,
      routed_48: draft.routed_48.trim() || undefined,
    }
    // Moving an old active tunnel to inventory is explicit and previewed.
    const assignment = {
      ...broker,
      tunnel_id: broker.tunnel_id ?? null,
      server_ipv6: broker.server_ipv6 ?? null,
      client_ipv4: broker.client_ipv4 ?? null,
      routed_64: broker.routed_64 ?? null,
      routed_48: broker.routed_48 ?? null,
    }
    const change =
      cfg.uplink?.ipv6?.via === '6in4'
        ? { tunnelbroker: assignment, uplink: { ...cfg.uplink, ipv6: null } }
        : { tunnelbroker: assignment }
    if (!broker.enabled && cfg.uplink?.ipv6?.via !== '6in4') {
      await commit(configPatch(cfg, change), 'Tunnel saved · Off')
      return
    }
    await propose(
      cfg,
      change,
      broker.enabled ? 'Tunnel details saved.' : 'Tunnel saved · Off',
    )
  }
  async function activate(enabled: boolean) {
    const cfg = await api.get<Config>('/api/dial/config')
    if (!cfg.tunnelbroker) throw new Error('Save the tunnel details first.')
    await propose(
      cfg,
      { tunnelbroker: { ...cfg.tunnelbroker, enabled } },
      enabled
        ? 'Tunnel enabled. Verify the connection below.'
        : 'Tunnel disabled. Saved details retained.',
    )
  }
  async function verify() {
    const cfg = await api.get<Config>('/api/dial/config')
    const s = await status.refetch()
    if (!cfg.tunnelbroker?.enabled || !s.data?.matches)
      throw new Error(
        'The running tunnel or source routes do not match the saved settings. Reapply activation and retry.',
      )
    const peer = cfg.tunnelbroker.server_ipv6?.split('/')[0]
    for (const target of [
      ...(peer ? [peer] : []),
      '2606:4700:4700::1111',
      '2001:4860:4860::8888',
    ]) {
      setProgress(`Testing ${target}`)
      const m = await api.post<Measurement>('/api/dial/he/check', { target })
      if (!m.received)
        throw new Error(
          `No reply from ${target}. Check HE's Client IPv4, protocol 41 forwarding and firewall.`,
        )
    }
    setMessage('IPv6 replies received through HE. Test LAN clients separately.')
  }
  async function measure() {
    stop.current = false
    setMeasurements({})
    for (const s of servers.data ?? []) {
      if (stop.current) break
      if (!s.address) continue
      setProgress(`Measuring ${s.name}`)
      try {
        const m = await api.post<Measurement>('/api/dial/he/check', {
          target: s.address,
        })
        if (!stop.current)
          setMeasurements((old) => ({ ...old, [s.address]: m }))
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
      return (
        x.loss - y.loss ||
        x.avg_ms - y.avg_ms ||
        x.variation_ms - y.variation_ms
      )
    })

  async function reviewNetwork(module: string) {
    setNetworkChange(null)
    if (!saved?.enabled || !status.data?.matches)
      throw new Error('Enable and verify the saved tunnel first.')
    if (!network) throw new Error('Choose a network.')
    let patch: object
    let current: object
    if (module === 'link') {
      current = await api.get<LinkConfig>('/api/link/config')
      const cfg = current as LinkConfig
      if (!cfg.networks?.some((n) => n.name === network))
        throw new Error('This network no longer exists.')
      // The server validates IPv6 syntax and overlap. Restrict HE assignments to
      // the saved routed allocation before proposing any LAN changes.
      const { containsSubnet } =
        await import('@/features/dial/tunnelbroker-prefix')
      if (
        !containsSubnet(saved.routed_64, subnet) &&
        !containsSubnet(saved.routed_48, subnet)
      )
        throw new Error('Choose a /64 within the saved HE routed prefixes.')
      patch = configPatch(cfg, {
        networks: cfg.networks.map((n) =>
          n.name === network
            ? { ...n, ipv6: { subnet: subnet.trim(), router: router.trim() } }
            : n,
        ),
      })
    } else if (module === 'dhcp') {
      current = await api.get<DhcpConfig>('/api/dhcp/config')
      const cfg = current as DhcpConfig
      const pool = cfg.pools?.find((p) => p.network === network)
      const ipv6 = { mode: 'slaac' as const, mtu: saved.mtu || 1480 }
      patch = configPatch(cfg, {
        enabled: true,
        pools: pool
          ? cfg.pools!.map((p) => (p.network === network ? { ...p, ipv6 } : p))
          : [...(cfg.pools ?? []), { network, ipv6 }],
      })
    } else {
      current = await api.get<GatewayConfig>('/api/gateway/config')
      patch = configPatch(current, { enabled: true, ipv6_forwarding: true })
    }
    const plan = await api.post<Plan & { blocked?: string; known?: boolean }>(
      `/api/${module}/plan`,
      { ...current, ...patch },
    )
    if (plan.blocked || plan.known === false)
      throw new Error(
        plan.blocked ?? 'Current routing state is unavailable. Retry.',
      )
    setNetworkChange({ module, patch, plan })
  }
  async function applyNetwork() {
    if (!networkChange) return
    setNetworkResult(null)
    try {
      const response = await api.patch<DialApplyResult>(
        `/api/${networkChange.module}/config?confirm=true`,
        networkChange.patch,
      )
      setNetworkResult(response)
      if (response.error || response.steps?.some((s) => s.error))
        throw new Error(
          'Some network changes failed. Review the steps before retrying.',
        )
      setNetworkChange(null)
      await links.refetch()
      setMessage('Network settings applied.')
    } catch (e) {
      if (e instanceof ApiError && e.body)
        setNetworkResult(e.body as DialApplyResult)
      throw e
    }
  }
  const disabled = busy || config.isPending || Boolean(config.error)
  const formFields: [keyof Draft, string, string][] = [
    ['tunnel_id', 'Tunnel ID', '1041353'],
    ['server_ipv4', 'Server IPv4 Address', '216.218.226.238'],
    ['server_ipv6', 'Server IPv6 Address', '2001:db8:1234::1/64'],
    ['client_ipv4', 'Client IPv4 Address', publicIP],
    ['client_ipv6', 'Client IPv6 Address', '2001:db8:1234::2/64'],
    ['routed_64', 'Routed /64', '2001:db8:5678::/64'],
    ['routed_48', 'Routed /48 (optional)', '2001:db8:abcd::/48'],
  ]
  return (
    <div className="space-y-8">
      <h1 className="sr-only">Tunnel Broker</h1>
      <nav
        aria-label="Tunnel Broker sections"
        className="sticky top-16 z-10 -mt-2 overflow-x-auto border-b bg-background/95 py-2 backdrop-blur-xl"
      >
        <ol className="flex min-w-max gap-2">
          {sections.map((name, i) => (
            <li key={name}>
              <a
                href={`#${ids[i]}`}
                aria-current={i === active ? 'step' : undefined}
                className={`inline-flex min-h-11 items-center gap-2 rounded-md px-3 text-sm ${i === active ? 'bg-muted font-medium' : 'text-muted-foreground hover:text-foreground'}`}
              >
                <span className="flex size-6 items-center justify-center rounded-full border text-xs">
                  {i + 1}
                </span>
                {name}
              </a>
            </li>
          ))}
        </ol>
      </nav>
      {config.error && (
        <p role="alert">
          Could not load saved tunnel: {config.error.message}{' '}
          <Button variant="outline" onClick={() => void config.refetch()}>
            Retry
          </Button>
        </p>
      )}
      {config.isPending && <p role="status">Loading saved tunnel…</p>}
      <Section id="endpoint" title="1. IPv4 endpoint">
        <div className="flex flex-wrap items-center gap-4">
          <span className="break-all font-mono text-lg">
            {publicIP ||
              (publicAddresses.isFetching ? 'Checking…' : 'Unavailable')}
          </span>
          <Button
            variant="outline"
            disabled={busy || publicAddresses.isFetching}
            onClick={() =>
              void run(async () => {
                const r = await publicAddresses.refetch()
                if (r.error) throw r.error
                if (!r.data?.ipv4.ip)
                  throw new Error('No public IPv4 returned.')
              })
            }
          >
            Refresh
          </Button>
        </div>
        {publicAddresses.error && (
          <p role="alert">{publicAddresses.error.message}</p>
        )}
        {uplink.error && <p role="alert">{uplink.error.message}</p>}
        {u && (
          <p className="break-all">
            {u.interface} · {u.addresses?.join(', ') || 'No observed IPv4'}
          </p>
        )}
        <p>
          Allow IP protocol 41 from HE. Behind another router, forward protocol
          41 to OLR.
        </p>
        <Disclosure summary="Upstream router requirements">
          <div className="space-y-3 pt-3">
            <p>
              Use a reachable public IPv4. CGNAT requires ISP assistance.
              TCP/UDP port 41 forwarding does not carry the tunnel.
            </p>
            <p>
              If using DMZ, keep OLR’s firewall enabled and administration
              private. HE may also require WAN ping during creation.
            </p>
            <p>
              This address is measured from the router; a browser VPN may show a
              different one.
            </p>
          </div>
        </Disclosure>
      </Section>
      <Section id="server" title="2. Server">
        <p>
          {chosen
            ? `${chosen.name} · ${chosen.address}`
            : draft.server_ipv4 ||
              'Choose an HE server, or enter your existing tunnel below.'}
        </p>
        <Disclosure summary="Compare servers">
          <div className="space-y-4 pt-3">
            <div className="flex flex-wrap gap-2">
              <Button
                disabled={busy || !servers.data || !u}
                onClick={() => void run(measure)}
              >
                Measure servers
              </Button>
              {busy && (
                <Button
                  variant="outline"
                  onClick={() => {
                    stop.current = true
                  }}
                >
                  Stop
                </Button>
              )}
            </div>
            <p>IPv4 ping from this router; this does not verify the tunnel.</p>
            {ranked[0] && <p>Best measured: {ranked[0].name}</p>}
            {servers.isPending && <p>Loading servers…</p>}
            {servers.error && (
              <p role="alert">
                {servers.error.message}{' '}
                <Button
                  variant="outline"
                  onClick={() => void servers.refetch()}
                >
                  Retry
                </Button>
              </p>
            )}
            <div className="max-h-96 overflow-y-auto divide-y">
              {servers.data?.map((s) => {
                const m = measurements[s.address]
                return (
                  <label
                    key={`${s.name}-${s.address}`}
                    className="flex min-h-16 items-start gap-3 py-3"
                  >
                    <input
                      type="radio"
                      name="he-server"
                      className="mt-1"
                      checked={
                        Boolean(s.address) && draft.server_ipv4 === s.address
                      }
                      disabled={busy || !s.address}
                      onChange={() => update('server_ipv4', s.address)}
                    />
                    <span className="min-w-0">
                      <span className="block">{s.name}</span>
                      <span className="block break-all font-mono text-sm">
                        {s.address || 'Full'}
                      </span>
                      {m && (
                        <span className="block text-sm text-muted-foreground">
                          {typeof m === 'string'
                            ? m
                            : m.received
                              ? `${m.avg_ms.toFixed(1)} ms · ${m.loss.toFixed(0)}% loss`
                              : 'No replies'}
                        </span>
                      )}
                    </span>
                  </label>
                )
              })}
            </div>
          </div>
        </Disclosure>
      </Section>
      <Section id="details" title="3. Tunnel details">
        <p>
          <a
            className="text-primary underline"
            href="https://tunnelbroker.net/new_tunnel.php"
            target="_blank"
            rel="noreferrer"
          >
            Create a tunnel at HE
          </a>
          , then copy its details here.
        </p>
        <div className="grid gap-4 md:grid-cols-2">
          {formFields.map(([key, label, placeholder]) => (
            <div key={key} className="space-y-2">
              <Label htmlFor={`he-${key}`}>{label}</Label>
              <Input
                id={`he-${key}`}
                value={draft[key]}
                placeholder={placeholder}
                disabled={disabled}
                onChange={(e) => update(key, e.target.value)}
              />
            </div>
          ))}
          <div className="space-y-2">
            <Label htmlFor="he-mtu">Tunnel MTU</Label>
            <Input
              id="he-mtu"
              type="number"
              min={1280}
              max={1480}
              value={draft.mtu}
              disabled={disabled}
              onChange={(e) => update('mtu', e.target.value)}
            />
          </div>
        </div>
        <Disclosure summary="MTU and endpoint updates">
          <div className="space-y-3 pt-3">
            <p>
              Use 1480 for a 1500-byte uplink, or 1472 for PPPoE. Minimum 1280.
            </p>
            <p>
              If your public IPv4 changes, update HE’s Client IPv4. Domain DDNS
              does not update the tunnel endpoint.
            </p>
          </div>
        </Disclosure>
        {legacy && (
          <p className="text-warning-foreground">
            Saving moves your existing active tunnel into saved inventory and
            turns it off. Review the change before confirming.
          </p>
        )}
        <Button disabled={disabled} onClick={() => void run(save)}>
          {saved?.enabled || legacy ? 'Review saved details' : 'Save tunnel'}
        </Button>
        <p className="text-muted-foreground">
          {saved?.enabled
            ? 'Editing an active tunnel requires confirmation.'
            : 'Saving leaves the tunnel off.'}
        </p>
      </Section>
      <Section id="activation" title="4. Activation">
        <p>
          {!saved
            ? 'Save the tunnel details first.'
            : saved.enabled
              ? status.error
                ? 'Enabled · Runtime unavailable'
                : status.isPending
                  ? 'Enabled · Checking…'
                  : status.data?.matches
                    ? 'Enabled · Configuration matches'
                    : 'Enabled · Not applied'
              : status.data?.present
                ? 'Saved · Off requested, tunnel still present'
                : status.error
                  ? 'Saved · Off requested, runtime unavailable'
                  : 'Saved · Off'}
        </p>
        <p>
          HE addresses use the tunnel. Native IPv6 keeps its existing route and
          uplink settings.
        </p>
        {status.error && <p role="alert">{status.error.message}</p>}
        {status.data?.native_default_dev && (
          <p>
            IPv6 default:{' '}
            <span className="font-mono">{status.data.native_default_dev}</span>
          </p>
        )}
        <div className="flex flex-wrap gap-2">
          <Button
            disabled={disabled || !saved || !u}
            onClick={() => void run(() => activate(true))}
          >
            {saved?.enabled
              ? 'Review / reapply activation'
              : 'Review activation'}
          </Button>
          {(saved?.enabled || (saved && status.data?.present)) && (
            <Button
              variant="outline"
              disabled={disabled}
              onClick={() => void run(() => activate(false))}
            >
              Review deactivation
            </Button>
          )}
          <Button
            variant="outline"
            disabled={disabled || !saved?.enabled}
            onClick={() => void run(verify)}
          >
            Test tunnel
          </Button>
        </div>
        {!u && (
          <p>
            <Link className="text-primary underline" to="/gateway/interfaces">
              Configure an uplink
            </Link>{' '}
            before activation.
          </p>
        )}
        {saved?.enabled && (
          <p className="text-warning-foreground">
            Turning off HE interrupts networks using its prefixes. Their LAN
            settings remain saved.
          </p>
        )}
        {preview && (
          <div className="space-y-3 rounded-lg border p-4">
            <PlanDetails plan={preview.plan} />
            <p>
              Network changes may interrupt connections. No automatic rollback.
            </p>
            <div className="flex flex-wrap gap-2">
              <Button
                disabled={busy}
                onClick={() =>
                  void run(() => commit(preview.patch, preview.label))
                }
              >
                Confirm changes
              </Button>
              <Button
                variant="outline"
                disabled={busy}
                onClick={() => setPreview(null)}
              >
                Cancel
              </Button>
            </div>
          </div>
        )}
        <ApplySteps result={result} />
      </Section>
      <Section id="networks" title="5. Networks">
        <p>
          Choose a /64 from your saved routed prefixes. Apply addressing,
          advertisements, then forwarding.
        </p>
        <div className="grid gap-4 md:grid-cols-2">
          <div className="space-y-2">
            <Label htmlFor="he-network">Network</Label>
            <Select
              value={network}
              onValueChange={(value) => {
                setNetwork(value ?? '')
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
                    {n.ipv6 ? ' · IPv6 configured' : ''}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-2">
            <Label htmlFor="he-prefix">HE /64</Label>
            <Select
              value={subnet === saved?.routed_64 ? 'routed' : 'custom'}
              onValueChange={(value) => {
                const s = value === 'routed' ? (saved?.routed_64 ?? '') : ''
                setSubnet(s)
                setRouter(s ? s.replace('::/64', '::1') : '')
                setNetworkChange(null)
              }}
            >
              <SelectTrigger id="he-prefix" className="w-full">
                <SelectValue>
                  {subnet === saved?.routed_64
                    ? saved.routed_64
                    : 'Choose / enter a /64'}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="custom">Choose / enter a /64</SelectItem>
                {saved?.routed_64 && (
                  <SelectItem value="routed">{saved.routed_64}</SelectItem>
                )}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-2">
            <Label htmlFor="he-subnet">LAN subnet</Label>
            <Input
              id="he-subnet"
              value={subnet}
              placeholder="2001:db8:5678::/64"
              onChange={(e) => {
                setSubnet(e.target.value)
                setNetworkChange(null)
              }}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="he-router">Router IPv6 address</Label>
            <Input
              id="he-router"
              value={router}
              placeholder="2001:db8:5678::1"
              onChange={(e) => {
                setRouter(e.target.value)
                setNetworkChange(null)
              }}
            />
          </div>
        </div>
        {links.error && <p role="alert">{links.error.message}</p>}
        <p className="text-warning-foreground">
          Applying replaces this LAN’s current IPv6 assignment. Advertisements
          may start DHCP; forwarding may enable Gateway routing.
        </p>
        <div className="flex flex-wrap gap-2">
          {[
            ['link', '1. Addressing'],
            ['dhcp', '2. Advertisements'],
            ['gateway', '3. Forwarding'],
          ].map(([module, label]) => (
            <Button
              key={module}
              variant="outline"
              disabled={disabled || !saved?.enabled || !network}
              onClick={() => void run(() => reviewNetwork(module))}
            >
              {label}
            </Button>
          ))}
        </div>
        {networkChange && (
          <div className="space-y-3 rounded-lg border p-4">
            <PlanDetails plan={networkChange.plan} />
            <div className="flex gap-2">
              <Button disabled={busy} onClick={() => void run(applyNetwork)}>
                Apply {networkChange.module} changes
              </Button>
              <Button
                variant="outline"
                disabled={busy}
                onClick={() => setNetworkChange(null)}
              >
                Cancel
              </Button>
            </div>
          </div>
        )}
        <ApplySteps result={networkResult} />
        <p>
          <a
            className="text-primary underline"
            href="https://test-ipv6.com/"
            target="_blank"
            rel="noreferrer"
          >
            Test IPv6 from a LAN client
          </a>{' '}
          after reconnecting it.
        </p>
      </Section>
      <div
        aria-live="polite"
        className="sticky bottom-20 z-10 space-y-2 rounded-lg bg-background/95 lg:bottom-4"
      >
        {progress && (
          <p className="break-words border p-3" role="status">
            {progress}
          </p>
        )}
        {message && (
          <p className="break-words border p-3" role="status">
            {message}
          </p>
        )}
        {error && (
          <p
            className="break-words border border-destructive p-3 text-destructive"
            role="alert"
          >
            {error}
          </p>
        )}
      </div>
    </div>
  )
}

function Section({
  id,
  title,
  children,
}: {
  id: string
  title: string
  children: ReactNode
}) {
  return (
    <section id={id} className="scroll-mt-36">
      <Card>
        <CardHeader>
          <CardTitle>
            <h2>{title}</h2>
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">{children}</CardContent>
      </Card>
    </section>
  )
}
function PlanDetails({ plan }: { plan: Plan }) {
  return (
    <>
      <p>Impact: {plan.impact}</p>
      {plan.warnings?.map((w) => (
        <p
          key={w.path + w.message}
          className="break-words text-warning-foreground"
        >
          {w.message}
        </p>
      ))}
      <Disclosure summary="Change details">
        <pre className="max-h-80 overflow-auto whitespace-pre-wrap break-all pt-3 text-sm">
          {plan.changes?.map((c) => c.diff).join('\n') ||
            'No configuration changes.'}
        </pre>
      </Disclosure>
    </>
  )
}
function ApplySteps({ result }: { result: DialApplyResult | null }) {
  if (!result) return null
  return (
    <div className="space-y-2">
      {result.error && <p role="alert">{result.error.message}</p>}
      {result.steps?.map((s) => (
        <p key={s.description} className="break-words">
          {s.done ? 'Completed' : 'Incomplete'}: {s.description}
          {s.error ? ` · ${s.error}` : ''}
        </p>
      ))}
    </div>
  )
}
