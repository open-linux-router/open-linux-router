import { AlertTriangle, ArrowRight, Tv } from 'lucide-react'
import { useState } from 'react'
import { Link } from 'react-router'
import { toast } from 'sonner'

import { SubPage } from '@/components/layout/sub-page'
import { StatusStrip } from '@/components/layout/status-strip'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useInterfaces } from '@/features/link/queries'
import {
  useIptvApply,
  useIptvConfig,
  useIptvPlan,
  useIptvStatus,
  type IptvApplyResult,
  type IptvConfig,
  type IptvPlan,
} from '@/features/iptv/queries'
import { ApiError } from '@/lib/api'

export function IptvPage() {
  const config = useIptvConfig()
  const status = useIptvStatus()
  const links = useInterfaces()
  const plan = useIptvPlan()
  const apply = useIptvApply()
  const [edited, setEdited] = useState<IptvConfig | null>(null)
  const [sourceEdit, setSourceEdit] = useState<string | null>(null)
  const [preview, setPreview] = useState<{ config: IptvConfig; plan: IptvPlan } | null>(null)

  const draft = edited ?? config.data
  const sources = sourceEdit ?? (config.data?.sources ?? []).join('\n')

  function edit(next: IptvConfig) {
    setEdited(next)
    setPreview(null)
  }

  async function review(next: IptvConfig) {
    try {
      setPreview({ config: next, plan: await plan.mutateAsync(next) })
    } catch (error) {
      toast.error(error instanceof Error ? error.message : String(error))
    }
  }

  async function save(next: IptvConfig) {
    try {
      const result = await apply.mutateAsync(next)
      if (result.error) throw new Error(result.error.message)
      setEdited(next)
      setSourceEdit((next.sources ?? []).join('\n'))
      setPreview(null)
      toast.success(next.enabled ? 'IPTV forwarding is on' : 'IPTV forwarding is off')
    } catch (error) {
      const partial = error instanceof ApiError ? error.body as IptvApplyResult | undefined : undefined
      toast.error(error instanceof Error ? error.message : String(error), {
        description: partial?.steps?.filter((step) => step.done).map((step) => step.description).join('; ') || undefined,
      })
      setPreview(null)
      void status.refetch()
    }
  }

  if (config.isPending) return <SubPage section="/advanced" slug="iptv"><Skeleton className="h-48 w-full" /></SubPage>
  if (config.isError) return <SubPage section="/advanced" slug="iptv"><Alert variant="destructive"><AlertTriangle /><AlertTitle>Could not load IPTV</AlertTitle><AlertDescription>{config.error.message}</AlertDescription></Alert></SubPage>

  if (!draft) return <SubPage section="/advanced" slug="iptv"><Alert variant="destructive">Configuration is unavailable.</Alert></SubPage>

  const interfaces = links.data?.interfaces ?? []
  const networks = links.data?.networks ?? []
  const available = interfaces.filter((iface) => iface.adopted && iface.present && iface.address && !iface.network && !iface.loopback)
  const selectedInterface = interfaces.find((iface) => iface.name === draft.upstream)
  const selectedNetworks = draft.networks ?? []
  const ready = !!draft.upstream && selectedNetworks.length > 0 &&
    !!selectedInterface?.adopted && !!selectedInterface.present && !!selectedInterface.address &&
    !selectedInterface.network && selectedNetworks.every((name) => networks.some((network) =>
      network.name === name && network.subnet && network.members.length === 1 &&
      network.members[0] !== draft.upstream &&
      interfaces.some((iface) => iface.name === network.members[0] && iface.present)))
  const next = { ...draft, sources: sources.split(/[\s,]+/).filter(Boolean) }
  const service = status.data?.service

  return (
    <SubPage section="/advanced" slug="iptv">
      <StatusStrip
        headline={status.isError ? 'Cannot read IPTV status' : status.data?.enabled ? service?.active ? 'IGMP proxy running' : 'IPTV needs attention' : 'IPTV is off'}
        detail={status.isError ? status.error.message : status.data?.enabled ? service?.active ? 'The proxy is running; this does not confirm a viewer has joined or that playback works.' : 'The proxy is not running; check the problem below.' : 'Nothing is sent from the IPTV interface to your LAN.'}
        dot={status.isError ? 'bg-warning' : status.data?.enabled ? service?.active ? 'bg-success' : 'bg-warning' : 'bg-muted-foreground/40'}
      />
      {links.isError && <Alert variant="destructive"><AlertTriangle /><AlertTitle>Cannot list interfaces</AlertTitle><AlertDescription>{links.error.message}</AlertDescription></Alert>}
      {status.data?.problem && <Alert variant="destructive"><AlertTriangle /><AlertTitle>Cannot forward IPTV</AlertTitle><AlertDescription>{status.data.problem}</AlertDescription></Alert>}
      {status.data?.enabled && status.data.plan && !status.data.plan.empty && <Alert><AlertTriangle /><AlertTitle>Settings need reapplying</AlertTitle><AlertDescription>{status.data.plan.changes?.join(', ')}</AlertDescription></Alert>}
      <Alert><Tv /><AlertTitle>Before you turn this on</AlertTitle><AlertDescription>
        The IPTV interface must already exist, have an IPv4 address, and reach the provider's stream. IPv4 forwarding must be enabled. This only proxies routed multicast; it cannot create a VLAN, bridge the modem to the set-top box, or handle the provider's DHCP/authentication. Install igmpproxy on the router first (outside this page). An AP without IGMP snooping may slow Wi-Fi while a channel is playing.
      </AlertDescription></Alert>

      <section className="space-y-5 rounded-xl border bg-card p-5 sm:p-6">
        <div className="space-y-1"><h2 className="font-semibold">Signal path</h2><p className="text-sm text-muted-foreground">Choose where IPTV enters and which LANs may subscribe. Other interfaces stay out of the proxy.</p></div>
        <div className="grid gap-5 sm:grid-cols-2">
          <label className="space-y-2 text-sm font-medium" htmlFor="iptv-upstream">IPTV upstream interface
            <select id="iptv-upstream" value={draft.upstream ?? ''} onChange={(event) => edit({ ...draft, upstream: event.target.value })} className="h-9 w-full rounded-lg border border-input bg-background px-2 text-sm">
              <option value="">Choose an interface</option>
              {available.map((iface) => <option key={iface.name} value={iface.name}>{iface.name} · {iface.address}</option>)}
              {draft.upstream && !available.some((iface) => iface.name === draft.upstream) && <option value={draft.upstream}>{draft.upstream} · unavailable</option>}
            </select>
          </label>
          <div className="space-y-2 text-sm font-medium">Downstream networks
            <div className="space-y-2 rounded-lg border p-3">
              {networks.filter((network) => network.subnet && network.members.length === 1).map((network) => (
                <label key={network.name} className="flex cursor-pointer items-center gap-2 font-normal">
                  <input type="checkbox" checked={selectedNetworks.includes(network.name)} onChange={(event) => edit({ ...draft, networks: event.target.checked ? [...selectedNetworks, network.name] : selectedNetworks.filter((name) => name !== network.name) })} />
                  <span>{network.name} <span className="text-muted-foreground">({network.members[0]})</span></span>
                </label>
              ))}
              {networks.length === 0 && <p className="font-normal text-muted-foreground">No LAN networks yet.</p>}
            </div>
          </div>
        </div>
        <label className="block space-y-2 text-sm font-medium" htmlFor="iptv-sources">Additional source IPv4 prefixes <span className="font-normal text-muted-foreground">(optional, one per line)</span>
          <textarea id="iptv-sources" rows={3} value={sources} onChange={(event) => { setSourceEdit(event.target.value); setPreview(null) }} placeholder="10.0.0.0/8" className="w-full rounded-lg border border-input bg-transparent px-3 py-2 font-mono text-sm" />
        </label>
        <p className="text-sm text-muted-foreground">Missing your IPTV interface? Set up its VLAN and IPv4 address on the router first, then <Link className="underline underline-offset-4" to="/gateway#interfaces">adopt it under Interfaces <ArrowRight className="inline size-3" /></Link>.</p>
        <div className="flex flex-wrap gap-2 border-t pt-4">
          <Button variant="outline" disabled={!ready || plan.isPending || apply.isPending} onClick={() => void review({ ...next, enabled: true })}>Preview changes</Button>
          {draft.enabled && <Button variant="outline" disabled={plan.isPending || apply.isPending} onClick={() => void review({ ...next, enabled: false })}>Turn off</Button>}
        </div>
        {!ready && !links.isPending && <p className="text-sm text-muted-foreground">Select an adopted IPTV interface with IPv4 and at least one present IPv4 LAN to continue.</p>}
      </section>
      {preview && <section className="space-y-4 rounded-xl border border-primary/30 bg-primary/5 p-5 sm:p-6">
        <div><h2 className="font-semibold">Review before applying</h2><p className="text-sm text-muted-foreground">{preview.plan.empty ? 'No service changes; the selected settings will still be saved.' : `Impact: ${preview.plan.impact}`}</p></div>
        <ul className="list-inside list-disc space-y-1 text-sm">{(preview.plan.changes ?? []).map((change) => <li key={change}>{change}</li>)}</ul>
        <div className="flex gap-2"><Button disabled={apply.isPending} onClick={() => void save(preview.config)}>{preview.config.enabled ? 'Enable IPTV' : 'Turn off IPTV'}</Button><Button variant="outline" onClick={() => setPreview(null)}>Cancel</Button></div>
      </section>}
    </SubPage>
  )
}
