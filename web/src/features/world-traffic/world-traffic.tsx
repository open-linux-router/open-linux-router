import { Heading } from '@/components/ui/heading'
import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTheme } from 'next-themes'
import { X } from 'lucide-react'
import * as maplibregl from 'maplibre-gl'
import type { ExpressionSpecification, GeoJSONSource } from 'maplibre-gl'
import type * as GeoJSON from 'geojson'
import workerUrl from 'maplibre-gl/dist/maplibre-gl-worker.mjs?worker&url'
import 'maplibre-gl/dist/maplibre-gl.css'
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { api } from '@/lib/api'
import { cn, formatRate } from '@/lib/utils'
import type { GeoPlace } from '@/lib/api-types'
import countries from './countries.json'
import regionsUrl from './regions.geojson?url'

interface Peer { ip: string; upload: number; download: number; place: GeoPlace }
interface Origin { latitude?: number; longitude?: number }
interface Snapshot { state: string; error?: string; as_of: string; seconds: number; total: number; peers: Peer[]; locations: { state: string; error?: string } }
type Position = [number, number]
interface FlowTarget { feature: GeoJSON.Feature; coordinates: Position[] }
const empty: GeoJSON.FeatureCollection = { type: 'FeatureCollection', features: [] }
// Traffic is one colour, outside the teal-green-blue family the map itself
// uses. A line-progress gradient only fades the alpha in at the router, where
// every arc starts, so the shared pixel becomes a fan instead of a knot.
const flowColor = (dark: boolean, halo: boolean) => dark
  ? (halo ? '#fdba74' : '#fb923c')
  : (halo ? '#fb923c' : '#ea580c')
const transparent = (hex: string) => {
  const n = parseInt(hex.slice(1), 16)
  return `rgba(${n >> 16}, ${(n >> 8) & 255}, ${n & 255}, 0)`
}
const flowGradient = (dark: boolean, halo: boolean) => {
  const color = flowColor(dark, halo)
  return ['interpolate', ['linear'], ['line-progress'], 0, transparent(color), 0.15, color, 1, color] as ExpressionSpecification
}
// Widths are screen pixels at the world view. Zooming into a region thins the
// arcs, so a country-sized view is not covered by ribbons meant for a globe.
// Zoom may only be the input of a top-level interpolate, so the scale wraps
// the data width instead of multiplying it.
const scaledWidth = (base: ExpressionSpecification) => ['interpolate', ['linear'], ['zoom'], 1, base, 3, ['*', base, 0.9], 4.5, ['*', base, 0.7], 6, ['*', base, 0.55]] as ExpressionSpecification
// Zoom may only appear as the input of a top-level step/interpolate expression.
// Layout expressions are evaluated at integer zooms; use integer thresholds
// for regions and cities so names don't wait for the final zoom level.
const labelText = ['step', ['zoom'], '', ...[1, 2, 3, 4, 5, 6].flatMap(zoom => [zoom, ['case', ['<=', ['get', 'minzoom'], zoom], ['get', 'name'], '']])] as ExpressionSpecification
function position(place: { latitude?: number; longitude?: number }): Position | undefined {
  if (typeof place.latitude === 'number' && typeof place.longitude === 'number' && Number.isFinite(place.latitude) && Number.isFinite(place.longitude) && Math.abs(place.latitude) <= 90 && Math.abs(place.longitude) <= 180) return [place.longitude, place.latitude]
}
maplibregl.setWorkerUrl(workerUrl)
maplibregl.setWorkerCount(1)

function arc(from: Position, to: Position, offset: number): Position[] {
  let end = to[0]
  while (end - from[0] > 180) end -= 360
  while (end - from[0] < -180) end += 360
  const bend = Math.min(22, Math.abs(end - from[0]) * 0.18) + offset
  return Array.from({ length: 49 }, (_, i) => {
    const t = i / 48
    return [from[0] + (end - from[0]) * t, Math.max(-78, Math.min(78, from[1] + (to[1] - from[1]) * t + Math.sin(Math.PI * t) * bend))]
  })
}

function TrafficMap({ peers, origin, dark, expanded = false }: { peers: Peer[]; origin?: Origin; dark: boolean; expanded?: boolean }) {
  const container = useRef<HTMLDivElement>(null)
  const map = useRef<maplibregl.Map | null>(null)
  const [ready, setReady] = useState(false)
  const [failed, setFailed] = useState(false)
  const targets = useRef(new Map<string, FlowTarget>())
  useEffect(() => {
    if (!container.current) return
    let instance: maplibregl.Map
    try {
      instance = new maplibregl.Map({ container: container.current,
        // The detailed administrative map is a fingerprinted local asset. Only
        // fullscreen maps fetch it; the small preview keeps the 110m outline.
        // Omitting glyphs uses local fonts, without a third-party glyph server.
        style: { version: 8, sources: { countries: { type: 'geojson', data: expanded ? regionsUrl : countries as GeoJSON.FeatureCollection, maxzoom: 6 } }, layers: [
          { id: 'ocean', type: 'background', paint: { 'background-color': '#f6f7f9' } },
          { id: 'land', type: 'fill', source: 'countries', filter: ['==', ['geometry-type'], 'Polygon'], paint: { 'fill-color': '#e2e6ec' } },
          { id: 'borders', type: 'line', source: 'countries', filter: ['==', ['geometry-type'], 'Polygon'], paint: { 'line-color': '#ffffff', 'line-width': 0.4 } },
        ] }, center: [25, 16], zoom: 0, minZoom: -3, maxZoom: 6, interactive: expanded, attributionControl: false,
        dragRotate: false, pitchWithRotate: false, touchZoomRotate: expanded ? { around: 'center' } : false,
        // Keep the whole world visible even in a tall phone viewport.
        transformConstrain: expanded ? (center, zoom) => ({ center, zoom: Math.max(-3, Math.min(6, zoom)) }) : undefined })
    } catch { setFailed(true); return }
    map.current = instance
    const fit = () => instance.fitBounds([[-180, -58], [180, 80]], { padding: 3, duration: 0 })
    // Resizing an already-open map must preserve the region the user chose.
    const observer = new ResizeObserver(() => { instance.resize(); if (!expanded) fit() })
    observer.observe(container.current)
    instance.on('load', () => {
      if (expanded) {
        instance.touchZoomRotate.disableRotation()
        instance.addLayer({ id: 'regions', type: 'line', source: 'countries', minzoom: 2, filter: ['==', ['get', 'kind'], 'boundary'], paint: { 'line-color': '#5b6472', 'line-width': ['interpolate', ['linear'], ['zoom'], 2, 0.3, 6, 0.8], 'line-opacity': 0.55 } })
        for (const kind of ['country', 'region', 'city']) {
          instance.addLayer({ id: `${kind}-labels`, type: 'symbol', source: 'countries', minzoom: kind === 'country' ? 1 : 3, filter: ['==', ['get', 'kind'], `${kind}-label`], layout: {
            'text-field': labelText,
            'text-font': ['sans-serif'], 'text-size': kind === 'country' ? 13 : 12, 'text-max-width': 9, 'text-padding': 6, 'symbol-sort-key': ['get', 'rank'],
          }, paint: { 'text-color': '#171b22', 'text-halo-color': '#ffffff', 'text-halo-width': 1 } })
        }
      }
      // lineMetrics computes the distances a line-progress gradient needs.
      instance.addSource('flows', { type: 'geojson', data: empty, lineMetrics: true })
      instance.addSource('dots', { type: 'geojson', data: empty })
      // Each arc is a soft halo under a translucent ribbon. Both composite
      // per feature, so where arcs bunch up — at the router, or along a busy
      // corridor — the halo accumulates into light and the ribbon deepens.
      instance.addLayer({ id: 'flow-halo', type: 'line', source: 'flows', layout: { 'line-cap': 'round', 'line-join': 'round' }, paint: {
        'line-width': scaledWidth(['+', ['*', ['get', 'width'], 1.8], 3]),
        'line-blur': scaledWidth(['+', ['*', ['get', 'width'], 0.8], 2]),
        'line-opacity': ['*', ['*', ['get', 'strength'], ['get', 'opacity']], 0.32],
      } })
      instance.addLayer({ id: 'flows', type: 'line', source: 'flows', layout: { 'line-cap': 'round', 'line-join': 'round' }, paint: {
        'line-width': scaledWidth(['get', 'width']),
        'line-opacity': ['*', ['get', 'strength'], ['get', 'opacity']],
      } })
      instance.addLayer({ id: 'dots', type: 'circle', source: 'dots', paint: { 'circle-radius': ['+', 0.85, ['*', ['get', 'width'], 0.18]], 'circle-color': ['get', 'color'], 'circle-opacity': ['get', 'opacity'] } })
      fit()
      setReady(true)
    })
    instance.on('error', () => setFailed(true))
    return () => { observer.disconnect(); instance.remove(); map.current = null }
  }, [expanded])
  useEffect(() => {
    const instance = map.current
    if (!instance || !ready) return
    // MapLibre's demo palette, with a muted counterpart for dark mode. The
    // generator assigns stable indices after checking country adjacency.
    const palette = dark ? ['#686083', '#80754f', '#526c58', '#797748', '#416d50', '#496e7a', '#535f85', '#815f52'] : ['#d6c7ff', '#ebca8a', '#c1e599', '#e7e58f', '#98dda1', '#83d5f4', '#b1bbf9', '#eab38f']
    instance.setPaintProperty('ocean', 'background-color', expanded ? (dark ? '#1c3545' : '#d6effb') : (dark ? '#232731' : '#f6f7f9'))
    const countryColors: ExpressionSpecification = ['match', ['get', 'color'], 0, palette[0], ...palette.slice(1).flatMap((color, index) => [index + 1, color]), dark ? '#424b5b' : '#e2e6ec']
    instance.setPaintProperty('land', 'fill-color', expanded ? countryColors : (dark ? '#424b5b' : '#e2e6ec'))
    instance.setPaintProperty('borders', 'line-color', dark ? '#232731' : '#ffffff')
    instance.setPaintProperty('flow-halo', 'line-gradient', flowGradient(dark, true))
    instance.setPaintProperty('flows', 'line-gradient', flowGradient(dark, false))
    if (expanded) {
      instance.setPaintProperty('regions', 'line-color', dark ? '#232731' : '#5b6472')
      for (const id of ['country-labels', 'region-labels', 'city-labels']) {
        instance.setPaintProperty(id, 'text-color', id === 'region-labels' ? (dark ? '#abb4c3' : '#5b6472') : (dark ? '#f1f3f7' : '#171b22'))
        instance.setPaintProperty(id, 'text-halo-color', dark ? '#232731' : '#ffffff')
      }
    }
    // Arcs carry the whole reading: no endpoint dots, because a handful of
    // nearby destinations turns them into a cluster of specks at the hub.
    const start = origin ? position(origin) : undefined
    const next = new Map<string, FlowTarget>()
    const locations = new Map<string, Peer>()
    for (const peer of peers) {
      const end = position(peer.place)
      if (!end) continue
      const key = end.join(',')
      const previous = locations.get(key)
      locations.set(key, { ...peer, upload: (previous?.upload ?? 0) + peer.upload, download: (previous?.download ?? 0) + peer.download })
    }
    let peak = 0
    for (const peer of locations.values()) peak = Math.max(peak, peer.download + peer.upload)
    for (const [location, peer] of locations) {
      const end = position(peer.place)
      if (!end) continue
      if (!start || (start[0] === end[0] && start[1] === end[1])) continue
      const total = peer.download + peer.upload
      if (total <= 0) continue
      // Width and translucency are relative to the busiest flow, not to an
      // absolute byte rate: a quiet router still reads, and one heavy flow
      // still stands out. The square root keeps small flows visible instead
      // of collapsing them into hairlines beside a dominant one.
      const weight = Math.sqrt(total / Math.max(1, peak))
      const width = 0.9 + 3.1 * weight
      const strength = 0.25 + 0.38 * weight
      const coordinates = arc(start, end, 0)
      next.set(`${location}:${start}`, { feature: { type: 'Feature', properties: { width, strength }, geometry: { type: 'LineString', coordinates } }, coordinates })
    }
    targets.current = next
  }, [peers, origin, dark, ready, expanded])
  useEffect(() => {
    const instance = map.current
    if (!instance || !ready) return
    const set = (id: string, features: GeoJSON.Feature[]) => (instance.getSource(id) as GeoJSONSource).setData({ type: 'FeatureCollection', features })
    // Keep identity and phase independent of snapshot order and rank changes.
    const visible = new Map<string, { target: FlowTarget; opacity: number; width: number; strength: number; phase: number }>()
    const reduced = matchMedia('(prefers-reduced-motion: reduce)')
    let previous = performance.now(), lastDraw = 0, frame = 0
    const animate = (now: number) => {
      frame = requestAnimationFrame(animate)
      if (document.hidden) { previous = now; return }
      if (now - lastDraw < 32) return
      const elapsed = Math.min(64, now - previous)
      previous = now
      lastDraw = now
      const blend = reduced.matches ? 1 : 1 - Math.exp(-elapsed / 450)
      for (const [key, target] of targets.current) {
        const existing = visible.get(key)
        if (existing) existing.target = target
        else {
          let hash = 0
          for (const char of key) hash = (hash * 31 + char.charCodeAt(0)) >>> 0
          visible.set(key, { target, opacity: 0, width: Number(target.feature.properties?.width || 0), strength: Number(target.feature.properties?.strength || 0), phase: hash / 4294967296 })
        }
      }
      const lines: GeoJSON.Feature[] = [], dots: GeoJSON.Feature[] = []
      for (const [key, visual] of visible) {
        const active = targets.current.has(key)
        visual.opacity += ((active ? 1 : 0) - visual.opacity) * blend
        if (!active && visual.opacity < 0.01) { visible.delete(key); continue }
        const path = visual.target
        visual.width += (Number(path.feature.properties?.width || 0) - visual.width) * blend
        visual.strength += (Number(path.feature.properties?.strength || 0) - visual.strength) * blend
        const properties = { ...path.feature.properties, opacity: visual.opacity, width: visual.width, strength: visual.strength }
        lines.push({ ...path.feature, properties })
        // The thumbnail keeps the arcs and drops the particles: at card size a
        // bead is a speck, and the cluster around the router reads as noise.
        if (reduced.matches || !expanded) continue
        // One dot travels out and back, taking four seconds in each direction.
        // The motion says which way it is going; the bead itself is neutral so
        // the amber arc stays the only traffic colour.
        visual.phase = (visual.phase + elapsed / 8000) % 1
        const returning = visual.phase >= 0.5
        const progress = returning ? (1 - visual.phase) * 2 : visual.phase * 2
        const color = dark ? '#e2e8f0' : '#ffffff'
        // Fade the bead near both ends. Every arc starts at the router, so a
        // bead that stayed visible there would rebuild the cluster the
        // endpoints used to make.
        const edge = Math.min(1, Math.min(progress, 1 - progress) / 0.08)
        const p = progress * 48
        const a = path.coordinates[Math.floor(p)], b = path.coordinates[Math.min(48, Math.floor(p) + 1)]
        dots.push({ type: 'Feature', properties: { ...properties, color, opacity: visual.opacity * edge }, geometry: { type: 'Point', coordinates: [a[0] + (b[0] - a[0]) * (p % 1), a[1] + (b[1] - a[1]) * (p % 1)] } })
      }
      set('flows', lines)
      set('dots', dots)
    }
    frame = requestAnimationFrame(animate)
    return () => cancelAnimationFrame(frame)
  }, [ready, expanded, dark])
  return <div className={cn('relative h-full min-h-32 w-full overflow-hidden', !expanded && 'rounded-lg')} style={expanded ? { backgroundColor: dark ? '#1c3545' : '#d6effb' } : undefined}><div ref={container} style={{ position: 'absolute', inset: 0 }} aria-hidden={!expanded} />{failed && <p className="absolute inset-0 flex items-center justify-center bg-muted p-3 text-xs text-muted-foreground">Map unavailable.</p>}</div>
}

export function WorldTraffic({ origin, embedded = false, expanded, onExpandedChange }: {
  origin?: Origin
  embedded?: boolean
  /** Whether the fullscreen map is open. The page may own this, so its URL can. */
  expanded?: boolean
  onExpandedChange?: (expanded: boolean) => void
}) {
  const [open, setOpen] = useState(false)
  // Uncontrolled when the card is used on its own; Overview passes the state
  // through so the address bar can hold the fullscreen map.
  const [ownExpanded, setOwnExpanded] = useState(false)
  const fullscreen = expanded ?? ownExpanded
  const setFullscreen = onExpandedChange ?? setOwnExpanded
  const preview = useRef<HTMLButtonElement>(null)
  const { resolvedTheme } = useTheme()
  const query = useQuery({ queryKey: ['gateway', 'world-traffic'], queryFn: () => api.get<Snapshot>('/api/gateway/world-traffic'), refetchInterval: 2000 })
  const data = query.data
  const stale = data?.as_of && Date.now() - Date.parse(data.as_of) > 10000
  const ready = !query.isError && !stale && data?.state === 'ready'
  const peers = ready ? data.peers : []
  const share = data?.total ? Math.round(peers.reduce((n, p) => n + p.download + p.upload, 0) / data.total * 100) : 0
  const unknown = peers.filter(p => !position(p.place)).length
  const status = query.isError || stale ? 'Unavailable' : !data ? 'Loading…' : data.state === 'disabled' ? 'Counting off' : data.state === 'warming' ? 'Measuring…' : data.state !== 'ready' ? 'Unavailable' : peers.length ? `Top ${peers.length} · ${share}%` : 'No observed traffic'
  return <section aria-label="Traffic map" className={embedded ? 'flex min-h-0 w-full min-w-0 flex-col gap-1' : 'col-span-2 flex min-w-0 flex-col gap-2 panel-surface p-3 sm:p-4 lg:col-span-1 lg:row-span-3'}>
    {!embedded && <Heading size="sm" className="flex h-6 items-center"><button onClick={() => setOpen(true)} className="relative rounded-sm before:absolute before:inset-x-0 before:-inset-y-2.5 focus-visible:outline-2 focus-visible:outline-ring" aria-label="Map details">Map</button></Heading>}
    <button ref={preview} type="button" onClick={() => setFullscreen(true)} aria-label="Open fullscreen map" aria-haspopup="dialog" className={embedded ? 'flex min-h-32 flex-1 cursor-zoom-in rounded-lg focus-visible:outline-2 focus-visible:outline-ring' : '-mx-1 -mb-1 flex min-h-44 flex-1 cursor-zoom-in rounded-lg focus-visible:outline-2 focus-visible:outline-ring sm:-mx-2 sm:-mb-2'}><TrafficMap peers={peers} origin={origin} dark={resolvedTheme === 'dark'} /></button>
    {(!ready || !peers.length) && <p className="text-xs text-muted-foreground">{status}</p>}
    <Dialog open={fullscreen} onOpenChange={setFullscreen}><DialogContent showCloseButton={false} finalFocus={preview} className="flex h-dvh max-h-dvh w-screen max-w-none flex-col gap-0 overflow-hidden rounded-none p-0 sm:max-w-none sm:p-0">
      <DialogTitle className="sr-only">Map</DialogTitle>
      <DialogClose render={<Button variant="secondary" size="icon-lg" className="absolute top-3 right-3 z-10 rounded-full bg-card/95 shadow-md ring-1 ring-border backdrop-blur-sm sm:size-9" />}><X aria-hidden="true" /><span className="sr-only">Close</span></DialogClose>
      <div className="min-h-0 flex-1" role="region" aria-label="World traffic map, drag to pan and scroll to zoom"><TrafficMap peers={peers} origin={origin} dark={resolvedTheme === 'dark'} expanded /></div>
    </DialogContent></Dialog>
    <Dialog open={open} onOpenChange={setOpen}><DialogContent className="max-h-[85dvh] overflow-y-auto"><DialogHeader><DialogTitle>Map</DialogTitle><DialogDescription>Remote IPs by upload and download in the last 30 seconds. Arcs are wider and more opaque for the busiest locations; each arc carries one dot that travels out and back. IP locations are approximate.{unknown ? ` ${unknown} locations unknown.` : ''}</DialogDescription></DialogHeader>
      <p className="text-sm text-muted-foreground">{status}{data?.seconds ? ` · ${Math.round(data.seconds)}s sampled` : ''}. {data?.state === 'disabled' ? 'Connection byte accounting is off. Enable Gateway traffic counting and apply its configuration.' : 'Counts include router and forwarded connections observed in Linux conntrack. Connections ending between samples and traffic bypassing conntrack may be missed.'} {(!origin || !position(origin)) && 'Router location is unknown; arcs cannot be drawn.'}</p>
      {data?.error && <p className="break-words text-sm text-destructive">{data.error}</p>}
      {data?.locations?.error && <p className="break-words text-sm text-destructive">{data.locations.error}</p>}
      <ul className="divide-y">{peers.map(peer => <li key={peer.ip} className="space-y-1 py-3 text-sm"><div className="flex flex-wrap justify-between gap-2"><span className="break-all font-mono">{peer.ip}</span><span>{[peer.place.city || peer.place.region, peer.place.country_name].filter(Boolean).join(', ') || 'Location unknown'}</span></div><div className="flex justify-between gap-2 text-xs tabular-nums"><span className="text-receive">↓ {formatRate(peer.download)}</span><span className="text-transmit">↑ {formatRate(peer.upload)}</span></div></li>)}</ul>
      <p className="text-xs text-muted-foreground">IP locations: {data?.locations?.state || 'Unavailable'} · <a href="https://ip.sb" target="_blank" rel="noreferrer" className="underline">ip.sb</a>. Map: <a href="https://www.naturalearthdata.com" target="_blank" rel="noreferrer" className="underline">Natural Earth</a> (public domain), rendered with MapLibre.</p>
    </DialogContent></Dialog>
  </section>
}
