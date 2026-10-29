import { t, useLocale } from '@/lib/i18n'
import { useState, type DragEvent } from 'react'
import { useNavigate } from 'react-router'
import { ArrowDown, ArrowUp, GripVertical, Trash2 } from 'lucide-react'

import { PageHeading } from '@/components/layout/page-heading'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter } from '@/components/ui/card'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Heading } from '@/components/ui/heading'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { RowAction } from '@/components/ui/row-action'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { brandLogoSrc, useBrandLogoSearch } from '@/features/brand-logos/brand-logos'
import {
  LATENCY_CATALOG,
  LATENCY_GROUPS,
  type CatalogSite,
  type LatencyCategory,
} from '@/features/gateway/latency-catalog'
import { SiteIcon } from '@/features/gateway/latency-site-icon'
import { useGatewayConfig, useGatewayLatency, useSaveLatencySites } from '@/features/gateway/queries'
import type { GatewayLatency } from '@/lib/api-types'

const MAX_SITES = 20
const DEFAULT_ROUTE = ' default'

export function LatencyPage() {
  useLocale()
  const latency = useGatewayLatency()
  const gateway = useGatewayConfig()
  const navigate = useNavigate()
  const [initial, setInitial] = useState<GatewayLatency>()
  const readError = latency.isError || gateway.isError
  // The overview's session cache is useful for measurements, but an editor must
  // start from a fresh read. Once initialized, polling never replaces its draft.
  if (!initial && !readError && latency.isFetchedAfterMount && latency.data && gateway.data) {
    setInitial(latency.data)
  }

  return <div className="space-y-8">
    <PageHeading title={t('Latency settings')} description={t('Measure HTTPS latency from this router.')} />
    {readError && <Alert variant="destructive">
      <AlertTitle>{t('Could not load settings')}</AlertTitle>
      <AlertDescription className="space-y-3">
        <p>{latency.error?.message || gateway.error?.message}</p>
        <Button variant="secondary" onClick={() => { void latency.refetch(); void gateway.refetch() }}>{t('Retry')}</Button>
      </AlertDescription>
    </Alert>}
    {initial ? <LatencySettings sites={initial.custom ?? []} latency={initial}
      exits={gateway.data?.enabled ? gateway.data.exits?.map(exit => exit.name) ?? [] : []}
      onClose={() => navigate('/')} />
      : !readError && <Skeleton className="h-64 w-full" />}
  </div>
}

type MonitoredSite = { name: string; url: string; exit?: string; icon?: string; category?: string }
type DraftSite = MonitoredSite & { id: string }

// Sites saved before categories existed, and hand-added sites, have none. Match
// a known catalog entry by name or URL so an existing list lands in the right
// group without a migration; the inferred value is saved on the next edit.
function inferCategory(name: string, url: string): LatencyCategory | '' {
  return LATENCY_CATALOG.find((entry) => entry.name === name || entry.url === url)?.category ?? ''
}

function LatencySettings({ sites, exits, latency, onClose }: { sites: MonitoredSite[]; exits: string[]; latency?: GatewayLatency; onClose: () => void }) {
  useLocale()
  const [target, setTarget] = useState(latency?.target_url || 'auto')
  const [targetChanged, setTargetChanged] = useState(false)
  const [draft, setDraft] = useState<DraftSite[]>(() => sites.map(({ name, url, exit, icon, category }) => ({
    id: crypto.randomUUID(),
    name,
    url,
    exit,
    icon,
    category: category && LATENCY_GROUPS.some((group) => group.key === category) ? category : inferCategory(name, url),
  })))
  const [name, setName] = useState('')
  const [url, setUrl] = useState('')
  const [exit, setExit] = useState(DEFAULT_ROUTE)
  const [editing, setEditing] = useState<number | null>(null)
  const [iconFor, setIconFor] = useState<number | null>(null)
  const [search, setSearch] = useState('')
  const [iconError, setIconError] = useState('')
  const [dragging, setDragging] = useState<number | null>(null)
  const [catalogOpen, setCatalogOpen] = useState(false)
  const [catalogSearch, setCatalogSearch] = useState('')
  const [catalogExit, setCatalogExit] = useState(DEFAULT_ROUTE)
  const { slugs: matches } = useBrandLogoSearch(search)
  const save = useSaveLatencySites()
  const update = (index: number, changes: Partial<MonitoredSite>) => setDraft((current) => current.map((site, i) => i === index ? { ...site, ...changes } : site))
  const add = () => {
    if (!name.trim() || !url.trim() || draft.length >= MAX_SITES) return
    setDraft([...draft, { id: crypto.randomUUID(), name: name.trim(), url: url.trim(), exit: exit === DEFAULT_ROUTE ? undefined : exit, icon: undefined, category: '' }])
    setName('')
    setUrl('')
    setExit(DEFAULT_ROUTE)
  }
  const addCatalogSite = (entry: CatalogSite) => {
    if (draft.length >= MAX_SITES) return
    if (draft.some((site) => site.name.toLowerCase() === entry.name.toLowerCase())) return
    setDraft([...draft, {
      id: crypto.randomUUID(),
      name: entry.name,
      url: entry.url,
      exit: catalogExit === DEFAULT_ROUTE ? undefined : catalogExit,
      icon: undefined,
      category: entry.category,
    }])
  }
  const siblings = (index: number) => draft.map((site, i) => ({ site, i })).filter(({ site }) => site.category === draft[index].category)
  const moveWithinGroup = (index: number, delta: number) => {
    const group = siblings(index)
    const position = group.findIndex(({ i }) => i === index)
    const other = group[position + delta]
    if (!other) return
    const next = [...draft]
    ;[next[index], next[other.i]] = [next[other.i], next[index]]
    setDraft(next)
  }
  const canMove = (index: number, delta: number) => {
    const group = siblings(index)
    const position = group.findIndex(({ i }) => i === index)
    return Boolean(group[position + delta])
  }
  const drop = (target: number) => {
    if (dragging == null || dragging === target) return
    if (draft[dragging].category !== draft[target].category) return
    const next = [...draft]
    const [site] = next.splice(dragging, 1)
    next.splice(target, 0, site)
    setDraft(next)
    setDragging(null)
    setEditing(null)
    setIconFor(null)
  }
  const upload = async (file?: File) => {
    if (iconFor == null || !file) return
    setIconError('')
    if (!['image/png', 'image/jpeg', 'image/webp'].includes(file.type) || file.size > 2 * 1024 * 1024) {
      setIconError('Choose a PNG, JPEG or WebP under 2 MiB')
      return
    }
    try {
      const image = await createImageBitmap(file)
      const canvas = document.createElement('canvas')
      const scale = Math.min(1, 128 / Math.max(image.width, image.height))
      canvas.width = Math.max(1, Math.round(image.width * scale))
      canvas.height = Math.max(1, Math.round(image.height * scale))
      canvas.getContext('2d')!.drawImage(image, 0, 0, canvas.width, canvas.height)
      image.close()
      const data = canvas.toDataURL('image/png')
      if (data.length > 130000) throw new Error('Image is too large after resizing')
      update(iconFor, { icon: data })
      setIconFor(null)
    } catch (error) { setIconError(error instanceof Error ? error.message : 'Could not read image') }
  }
  const groups = LATENCY_GROUPS
    .map((group) => ({ ...group, items: draft.map((site, index) => ({ site, index })).filter(({ site }) => site.category === group.key) }))
    .filter((group) => group.items.length > 0)
  const custom = draft.map((site, index) => ({ site, index })).filter(({ site }) => !site.category)
  const catalogQuery = catalogSearch.trim().toLowerCase()
  const full = draft.length >= MAX_SITES

  const siteRow = (site: DraftSite, i: number) => <div key={site.id} className="rounded-xl bg-muted/60 p-2 text-sm"
    onDragOver={(event: DragEvent<HTMLDivElement>) => event.preventDefault()} onDrop={(event) => { event.preventDefault(); drop(i) }}>
    <div className="grid grid-cols-[44px_minmax(0,1fr)_44px_44px] items-center gap-2 sm:flex sm:flex-nowrap">
      <span draggable onDragStart={() => setDragging(i)} onDragEnd={() => setDragging(null)} aria-label={`Drag ${site.name} to reorder`} className="hidden cursor-grab touch-none text-muted-foreground sm:block" title={t('Drag to reorder')}><GripVertical className="size-4" /></span>
      <Button variant="quiet" size="icon-sm" onClick={() => { setIconFor(i); setSearch(''); setIconError('') }} aria-label={`Change ${site.name} icon`}><SiteIcon name={site.name} url={site.url} icon={site.icon} /></Button>
      <div className="col-span-2 col-start-3 row-start-2 flex justify-end gap-2 sm:hidden"><Button variant="quiet" size="icon-sm" aria-label={`Move ${site.name} up`} title={`${t('Move up')}`} disabled={!canMove(i, -1)} onClick={() => moveWithinGroup(i, -1)}><ArrowUp aria-hidden /></Button><Button variant="quiet" size="icon-sm" aria-label={`Move ${site.name} down`} title={`${t('Move down')}`} disabled={!canMove(i, 1)} onClick={() => moveWithinGroup(i, 1)}><ArrowDown aria-hidden /></Button></div>
      <Button variant="quiet" onClick={() => setEditing(editing === i ? null : i)} className="col-span-2 h-auto min-h-11 min-w-0 flex-1 justify-start px-0 whitespace-normal break-words text-left sm:min-h-9" title={site.url}>{site.name}</Button>
      <Select value={site.exit || DEFAULT_ROUTE} onValueChange={(value) => update(i, { exit: !value || value === DEFAULT_ROUTE ? undefined : value })}>
        <SelectTrigger size="sm" className="col-span-2 col-start-1 row-start-2 w-full min-w-0 sm:w-auto sm:max-w-32" aria-label={`Internet via, for ${site.name}`}><SelectValue /></SelectTrigger>
        <SelectContent><SelectItem value={DEFAULT_ROUTE}>{t('Default')}</SelectItem>{exits.map((value) => <SelectItem key={value} value={value}>{value}</SelectItem>)}
          {site.exit && !exits.includes(site.exit) && <SelectItem value={site.exit}>{site.exit} {t('(unavailable)')}</SelectItem>}
        </SelectContent>
      </Select>
      <RowAction icon={Trash2} label={`${t('Remove')} ${site.name}`} destructive onClick={() => { setDraft(draft.filter((_, index) => index !== i)); setEditing(null); setIconFor(null) }} />
    </div>
    {editing === i && <div className="mt-2 grid gap-2 sm:grid-cols-[1fr_2fr]">
      <Input aria-label={`Name for ${site.name}`} value={site.name} maxLength={40} onChange={(event) => update(i, { name: event.target.value })} />
      <Input aria-label={`URL for ${site.name}`} value={site.url} onChange={(event) => update(i, { url: event.target.value })} />
    </div>}
  </div>

  return <>
    <Card>
      <CardContent className="space-y-6">
        <div className="space-y-2">
          <Label htmlFor="internet-latency-target">{t('Internet target')}</Label>
          <Select value={target} onValueChange={(value) => { setTarget(value ?? 'auto'); setTargetChanged(true) }}>
            <SelectTrigger id="internet-latency-target" className="w-full"><SelectValue>{target === 'auto' ? t('Choose fastest once') : latency?.sites.find((site) => site.url === target)?.name}</SelectValue></SelectTrigger>
            <SelectContent>
              <SelectItem value="auto">{t('Choose fastest once')}</SelectItem>
              {latency?.sites.map((site) => <SelectItem key={site.url} value={site.url}>{site.name}</SelectItem>)}
            </SelectContent>
          </Select>
          <p className="text-sm text-muted-foreground">{t('The chosen target stays fixed until you change it.')}</p>
        </div>
        <section aria-labelledby="monitored-sites-heading" className="space-y-4">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <Heading id="monitored-sites-heading">{t('Monitored sites')}</Heading>
            <Button variant="secondary" size="sm" onClick={() => { setCatalogSearch(''); setCatalogOpen(true) }}>{t('Add common site')}</Button>
          </div>
          {groups.map((group) => <section key={group.key} aria-label={t(group.label)} className="space-y-2">
            <Heading as="h3" size="sm" className="text-muted-foreground">{t(group.label)}</Heading>
            <div className="space-y-2">{group.items.map(({ site, index }) => siteRow(site, index))}</div>
          </section>)}
          {custom.length > 0 && <section aria-label={t('Custom')} className="space-y-2">
            <Heading as="h3" size="sm" className="text-muted-foreground">{t('Custom')}</Heading>
            <div className="space-y-2">{custom.map(({ site, index }) => siteRow(site, index))}</div>
          </section>}
          {draft.length === 0 && <p className="text-sm text-muted-foreground">{t('No monitored sites yet. Add a common site or one of your own.')}</p>}
          <div className="flex flex-wrap gap-2 sm:flex-nowrap">
            <Input className="min-w-24 flex-1 sm:w-24" aria-label={t('Site name')} placeholder={t('Name')} maxLength={40} value={name} onChange={(event) => setName(event.target.value)} />
            <Input className="min-w-40 flex-[2] sm:w-52" aria-label="HTTPS URL" placeholder="https://example.com/" value={url} onChange={(event) => setUrl(event.target.value)} />
            <Select value={exit} onValueChange={(value) => setExit(value ?? DEFAULT_ROUTE)}>
              <SelectTrigger className="w-28 shrink-0" aria-label={t('Internet via for new site')}><SelectValue /></SelectTrigger>
              <SelectContent><SelectItem value={DEFAULT_ROUTE}>{t('Default')}</SelectItem>{exits.map((value) => <SelectItem key={value} value={value}>{value}</SelectItem>)}</SelectContent>
            </Select>
            <Button variant="secondary" onClick={add} disabled={!name.trim() || !url.startsWith('https://') || full}>{t('Add')}</Button>
          </div>
          {full && <p className="text-sm text-muted-foreground">{t('At most {count} sites can be monitored.', { count: MAX_SITES })}</p>}
        </section>
        {save.isError && <p role="alert" className="text-sm text-destructive">{save.error.message}</p>}
      </CardContent>
      <CardFooter className="justify-end gap-2"><Button variant="quiet" onClick={onClose}>{t('Cancel')}</Button><Button disabled={save.isPending} onClick={() => save.mutate({ sites: draft.map(({ name, url, exit, icon, category }) => ({ name, url, exit, icon, category })), target: targetChanged ? (target === 'auto' ? '' : target) : undefined }, { onSuccess: onClose })}>{t('Save')}</Button></CardFooter>
    </Card>
    {catalogOpen && <Dialog open onOpenChange={(open) => { if (!open) setCatalogOpen(false) }}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader><DialogTitle>{t('Common sites')}</DialogTitle><DialogDescription>{t('Each site uses the faster of its main page and its static host. Choose an exit before adding; you can change it per site afterwards.')}</DialogDescription></DialogHeader>
        <div className="flex flex-wrap gap-2">
          <Input className="min-w-40 flex-1" aria-label={t('Search common sites')} placeholder={t('Search')} value={catalogSearch} onChange={(event) => setCatalogSearch(event.target.value)} />
          <Select value={catalogExit} onValueChange={(value) => setCatalogExit(value ?? DEFAULT_ROUTE)}>
            <SelectTrigger className="w-32" aria-label={t('Internet via for new site')}><SelectValue /></SelectTrigger>
            <SelectContent><SelectItem value={DEFAULT_ROUTE}>{t('Default')}</SelectItem>{exits.map((value) => <SelectItem key={value} value={value}>{value}</SelectItem>)}</SelectContent>
          </Select>
        </div>
        <div className="max-h-80 space-y-4 overflow-y-auto pr-1">
          {LATENCY_GROUPS.map((group) => {
            const entries = LATENCY_CATALOG.filter((entry) => entry.category === group.key && (!catalogQuery || entry.name.toLowerCase().includes(catalogQuery)))
            if (entries.length === 0) return null
            return <section key={group.key} className="space-y-1">
              <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{t(group.label)}</p>
              <div className="grid gap-1 sm:grid-cols-2">
                {entries.map((entry) => {
                  const added = draft.some((site) => site.name.toLowerCase() === entry.name.toLowerCase())
                  return <button key={entry.name} type="button" disabled={added || full} onClick={() => addCatalogSite(entry)}
                    className="flex min-w-0 items-center gap-2 rounded-lg border px-2 py-2 text-left text-sm hover:bg-muted disabled:opacity-50">
                    <span aria-hidden className="flex size-5 shrink-0 items-center justify-center rounded-md bg-foreground/10 text-[10px] font-bold uppercase">{entry.name.slice(0, 2)}</span>
                    <span className="min-w-0 flex-1 truncate">{entry.name}</span>
                    {added && <span className="shrink-0 text-xs text-muted-foreground">{t('Added')}</span>}
                  </button>
                })}
              </div>
            </section>
          })}
          {LATENCY_CATALOG.every((entry) => catalogQuery && !entry.name.toLowerCase().includes(catalogQuery)) && <p className="text-sm text-muted-foreground">{t('No matching sites.')}</p>}
        </div>
        {full && <p className="text-sm text-muted-foreground">{t('At most {count} sites can be monitored.', { count: MAX_SITES })}</p>}
      </DialogContent>
    </Dialog>}
    {iconFor != null && <Dialog open onOpenChange={(open) => { if (!open) setIconFor(null) }}>
      <DialogContent className="sm:max-w-md"><DialogHeader><DialogTitle>{t('Icon for')} {draft[iconFor]?.name}</DialogTitle><DialogDescription>{t('Upload an image or search theSVG library. Auto uses the site favicon.')}</DialogDescription></DialogHeader>
        <div className="flex gap-2"><Button variant="secondary" onClick={() => { update(iconFor, { icon: undefined }); setIconFor(null) }}>{t('Auto')}</Button>
          <label className="inline-flex min-h-11 cursor-pointer items-center rounded-lg border px-3 text-sm focus-within:outline-2 focus-within:outline-ring sm:min-h-9">{t('Upload image')}<input className="sr-only" type="file" accept="image/png,image/jpeg,image/webp" onChange={(event) => upload(event.target.files?.[0])} /></label></div>
        <Input aria-label={t('Search theSVG icons')} placeholder={t('Search theSVG (e.g. wechat)')} value={search} onChange={(event) => setSearch(event.target.value)} />
        <div className="grid max-h-52 grid-cols-4 gap-2 overflow-y-auto">{matches.map((slug) => <button key={slug} type="button" className="flex min-w-0 flex-col items-center gap-1 rounded-lg border p-2 text-xs hover:bg-muted" title={slug}
          onClick={() => { update(iconFor, { icon: `thesvg:${slug}` }); setIconFor(null) }}>
          <img src={brandLogoSrc(slug)} alt="" className="size-7" loading="lazy" />
          <span className="w-full truncate">{slug}</span></button>)}</div>
        {iconError && <p role="alert" className="text-sm text-destructive">{iconError}</p>}
        <p className="text-xs text-muted-foreground">{t('Icons by theSVG (MIT). Only the selected ID is saved.')}</p>
      </DialogContent>
    </Dialog>}
  </>
}
