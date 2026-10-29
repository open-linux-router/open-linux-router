import { t, useLocale } from '@/lib/i18n'
import { useState } from 'react'
import { useNavigate } from 'react-router'
import { LoaderCircle, Plus, Trash2 } from 'lucide-react'

import { PageHeading } from '@/components/layout/page-heading'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter } from '@/components/ui/card'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Heading } from '@/components/ui/heading'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { brandLogoSrc, useBrandLogoSearch } from '@/features/brand-logos/brand-logos'
import { SiteIcon } from '@/features/gateway/latency-site-icon'
import { useGatewayConfig, useGatewayLatency, useMeasureLatencySite, useSaveLatencySites } from '@/features/gateway/queries'
import type { GatewayLatency } from '@/lib/api-types'

const MAX_SITES = 20
const DEFAULT_ROUTE = ' default'
const CATEGORY_ORDER = ['communication', 'social', 'entertainment', 'news', 'shopping', 'tools']
const CATEGORY_LABELS: Record<string, string> = {
  communication: 'Communication',
  social: 'Social',
  entertainment: 'Entertainment',
  news: 'News',
  shopping: 'Shopping',
  tools: 'Tools',
}

type MonitoredSite = { name: string; url: string; exit?: string; icon?: string; category?: string; builtin?: boolean }
type DraftSite = MonitoredSite & { id: string }

function formatLatency(ms: number) {
  return ms < 1 ? '<1 ms' : `${Math.round(ms)} ms`
}

function siteValue(result?: GatewayLatency['custom'][number]) {
  if (!result || result.milliseconds == null) return '—'
  return formatLatency(result.milliseconds)
}

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

function LatencySettings({ sites, exits, latency, onClose }: { sites: MonitoredSite[]; exits: string[]; latency?: GatewayLatency; onClose: () => void }) {
  useLocale()
  const [target, setTarget] = useState(latency?.target_url || 'auto')
  const [targetChanged, setTargetChanged] = useState(false)
  const [draft, setDraft] = useState<DraftSite[]>(() => sites.map(({ name, url, exit, icon, category, builtin }) => ({
    id: crypto.randomUUID(), name, url, exit, icon, category, builtin,
  })))
  const [selected, setSelected] = useState<string | null>(null)
  const [adding, setAdding] = useState(false)
  const [newName, setNewName] = useState('')
  const [newUrl, setNewUrl] = useState('')
  const [newExit, setNewExit] = useState(DEFAULT_ROUTE)
  const [addError, setAddError] = useState('')
  const [iconFor, setIconFor] = useState<string | null>(null)
  const [search, setSearch] = useState('')
  const [iconError, setIconError] = useState('')
  const { slugs: matches } = useBrandLogoSearch(search)
  const save = useSaveLatencySites()
  const measure = useMeasureLatencySite()
  const update = (id: string, changes: Partial<MonitoredSite>) => setDraft((current) => current.map((site) => site.id === id ? { ...site, ...changes } : site))
  const customCount = draft.filter((site) => !site.builtin).length
  const groups = CATEGORY_ORDER
    .map((category) => ({ category, label: CATEGORY_LABELS[category], sites: draft.filter((site) => site.builtin && site.category === category) }))
    .filter((group) => group.sites.length > 0)
  const custom = draft.filter((site) => !site.builtin)
  const addSite = () => {
    const name = newName.trim()
    const url = newUrl.trim()
    if (!name || !url.startsWith('https://') || customCount >= MAX_SITES || draft.some((site) => site.name.toLowerCase() === name.toLowerCase())) {
      setAddError(t('Use a unique name and an HTTPS URL.'))
      return
    }
    setDraft([...draft, { id: crypto.randomUUID(), name, url, exit: newExit === DEFAULT_ROUTE ? undefined : newExit, icon: undefined, builtin: false }])
    setNewName('')
    setNewUrl('')
    setNewExit(DEFAULT_ROUTE)
    setAdding(false)
  }
  const removeSite = (id: string) => {
    setDraft((current) => current.filter((site) => site.id !== id))
    setSelected(null)
  }
  const upload = async (file?: File) => {
    const site = draft.find((candidate) => candidate.id === iconFor)
    if (!site || !file) return
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
      update(site.id, { icon: data })
      setIconFor(null)
    } catch (error) { setIconError(error instanceof Error ? error.message : 'Could not read image') }
  }
  const site = draft.find((candidate) => candidate.id === selected)
  const result = site ? latency?.custom.find((candidate) => candidate.name === site.name) : undefined
  const saved = site ? latency?.custom.some((candidate) => candidate.name === site.name) ?? false : false

  const tile = (entry: DraftSite) => {
    const live = latency?.custom.find((candidate) => candidate.name === entry.name)
    return <button key={entry.id} type="button" onClick={() => { measure.reset(); setSelected(entry.id) }}
      aria-label={`${entry.name}: ${siteValue(live)}`}
      className="flex min-h-14 min-w-0 items-center gap-2 rounded-xl border bg-muted/40 px-3 text-left transition-colors hover:bg-muted focus-visible:outline-2 focus-visible:outline-ring">
      <SiteIcon name={entry.name} url={entry.url} icon={entry.icon} />
      <span className="min-w-0 flex-1 truncate text-sm font-medium">{entry.name}</span>
      {live?.measuring
        ? <LoaderCircle className="size-4 shrink-0 animate-spin text-muted-foreground" aria-label={t('Measuring')} />
        : <span className="shrink-0 text-sm tabular-nums text-muted-foreground">{siteValue(live)}</span>}
    </button>
  }

  return <>
    <Card>
      <CardContent className="space-y-6">
        <div className="space-y-2">
          <Label htmlFor="internet-latency-target">{t('Internet target')}</Label>
          <Select value={target} onValueChange={(value) => { setTarget(value ?? 'auto'); setTargetChanged(true) }}>
            <SelectTrigger id="internet-latency-target" className="w-full"><SelectValue>{target === 'auto' ? t('Choose fastest once') : latency?.sites.find((entry) => entry.url === target)?.name}</SelectValue></SelectTrigger>
            <SelectContent>
              <SelectItem value="auto">{t('Choose fastest once')}</SelectItem>
              {latency?.sites.map((entry) => <SelectItem key={entry.url} value={entry.url}>{entry.name}</SelectItem>)}
            </SelectContent>
          </Select>
          <p className="text-sm text-muted-foreground">{t('The chosen target stays fixed until you change it.')}</p>
        </div>
        <section aria-labelledby="monitored-sites-heading" className="space-y-5">
          <Heading id="monitored-sites-heading">{t('Monitored sites')}</Heading>
          {groups.map((group) => <section key={group.category} aria-label={t(group.label)} className="space-y-2">
            <Heading as="h3" size="sm" className="text-muted-foreground">{t(group.label)}</Heading>
            <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4">{group.sites.map(tile)}</div>
          </section>)}
          <section aria-label={t('Custom')} className="space-y-2">
            <Heading as="h3" size="sm" className="text-muted-foreground">{t('Custom')}</Heading>
            <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4">
              {custom.map(tile)}
              <button type="button" disabled={customCount >= MAX_SITES} onClick={() => { setAddError(''); setAdding(true) }}
                className="flex min-h-14 items-center justify-center gap-2 rounded-xl border border-dashed px-3 text-sm text-muted-foreground hover:bg-muted focus-visible:outline-2 focus-visible:outline-ring disabled:opacity-50">
                <Plus className="size-4" aria-hidden />{t('Add site')}
              </button>
            </div>
            {customCount >= MAX_SITES && <p className="text-sm text-muted-foreground">{t('At most {count} sites can be monitored.', { count: MAX_SITES })}</p>}
          </section>
        </section>
        {save.isError && <p role="alert" className="text-sm text-destructive">{save.error.message}</p>}
      </CardContent>
      <CardFooter className="justify-end gap-2"><Button variant="quiet" onClick={onClose}>{t('Cancel')}</Button><Button disabled={save.isPending} onClick={() => save.mutate({ sites: draft.map(({ name, url, exit, icon }) => ({ name, url, exit, icon })), target: targetChanged ? (target === 'auto' ? '' : target) : undefined }, { onSuccess: onClose })}>{t('Save')}</Button></CardFooter>
    </Card>
    {site && <Dialog open onOpenChange={(open) => { if (!open) setSelected(null) }}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <div className="flex min-w-0 items-center gap-2"><SiteIcon name={site.name} url={site.url} icon={site.icon} /><DialogTitle className="truncate">{site.name}</DialogTitle></div>
          <DialogDescription className="truncate">{site.url}</DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <div className="flex items-baseline justify-between rounded-lg bg-muted/50 px-3 py-2">
            <span className="text-sm text-muted-foreground">{t('Latest sample')}</span>
            <span className="text-lg font-semibold tabular-nums">{result?.measuring ? t('Measuring…') : siteValue(result)}</span>
          </div>
          {result?.error && <p className="text-sm text-destructive">{result.error}</p>}
          <div className="space-y-2">
            <Label htmlFor="site-exit">{t('Internet via')}</Label>
            <Select value={site.exit || DEFAULT_ROUTE} onValueChange={(value) => update(site.id, { exit: !value || value === DEFAULT_ROUTE ? undefined : value })}>
              <SelectTrigger id="site-exit" className="w-full"><SelectValue /></SelectTrigger>
              <SelectContent><SelectItem value={DEFAULT_ROUTE}>{t('Default')}</SelectItem>{exits.map((value) => <SelectItem key={value} value={value}>{value}</SelectItem>)}
                {site.exit && !exits.includes(site.exit) && <SelectItem value={site.exit}>{site.exit} {t('(unavailable)')}</SelectItem>}
              </SelectContent>
            </Select>
          </div>
          {!site.builtin && <div className="grid gap-2 sm:grid-cols-2">
            <Input aria-label={t('Name')} value={site.name} maxLength={40} onChange={(event) => update(site.id, { name: event.target.value })} />
            <Input aria-label="HTTPS URL" value={site.url} onChange={(event) => update(site.id, { url: event.target.value })} />
          </div>}
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex gap-2">
              {!site.builtin && <Button variant="quiet" size="sm" onClick={() => { setSelected(null); setSearch(''); setIconError(''); setIconFor(site.id) }}>{t('Change icon')}</Button>}
              {!site.builtin && <Button variant="quiet" size="sm" className="text-destructive" onClick={() => removeSite(site.id)}><Trash2 aria-hidden />{t('Remove')}</Button>}
            </div>
            <div className="flex gap-2">
              <Button variant="quiet" size="sm" onClick={() => setSelected(null)}>{t('Close')}</Button>
              <Button variant="secondary" size="sm" disabled={!saved || result?.measuring || measure.isPending} onClick={() => measure.mutate(site.name)}>{t('Measure now')}</Button>
            </div>
          </div>
          {!saved && <p className="text-sm text-muted-foreground">{t('Save this site before measuring it.')}</p>}
          {measure.isError && <p role="alert" className="text-sm text-destructive">{measure.error.message}</p>}
        </div>
      </DialogContent>
    </Dialog>}
    {adding && <Dialog open onOpenChange={(open) => { if (!open) setAdding(false) }}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader><DialogTitle>{t('Add site')}</DialogTitle><DialogDescription>{t('A custom site is measured with the others and can be removed at any time.')}</DialogDescription></DialogHeader>
        <div className="space-y-3">
          <Input aria-label={t('Site name')} placeholder={t('Name')} maxLength={40} value={newName} onChange={(event) => setNewName(event.target.value)} />
          <Input aria-label="HTTPS URL" placeholder="https://example.com/" value={newUrl} onChange={(event) => setNewUrl(event.target.value)} />
          <Select value={newExit} onValueChange={(value) => setNewExit(value ?? DEFAULT_ROUTE)}>
            <SelectTrigger className="w-full" aria-label={t('Internet via for new site')}><SelectValue /></SelectTrigger>
            <SelectContent><SelectItem value={DEFAULT_ROUTE}>{t('Default')}</SelectItem>{exits.map((value) => <SelectItem key={value} value={value}>{value}</SelectItem>)}</SelectContent>
          </Select>
          {addError && <p role="alert" className="text-sm text-destructive">{addError}</p>}
          <div className="flex justify-end gap-2"><Button variant="quiet" onClick={() => setAdding(false)}>{t('Cancel')}</Button><Button variant="secondary" onClick={addSite} disabled={!newName.trim() || !newUrl.startsWith('https://')}>{t('Add')}</Button></div>
        </div>
      </DialogContent>
    </Dialog>}
    {iconFor != null && <Dialog open onOpenChange={(open) => { if (!open) setIconFor(null) }}>
      <DialogContent className="sm:max-w-md"><DialogHeader><DialogTitle>{t('Icon for')} {draft.find((entry) => entry.id === iconFor)?.name}</DialogTitle><DialogDescription>{t('Upload an image or search theSVG library. Auto uses the site favicon.')}</DialogDescription></DialogHeader>
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
