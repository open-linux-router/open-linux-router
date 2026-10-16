import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTheme } from 'next-themes'
import * as maplibregl from 'maplibre-gl'
import type { ExpressionSpecification, GeoJSONSource } from 'maplibre-gl'
import type * as GeoJSON from 'geojson'
import workerUrl from 'maplibre-gl/dist/maplibre-gl-worker.mjs?worker&url'
import 'maplibre-gl/dist/maplibre-gl.css'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { api } from '@/lib/api'
import { formatRate } from '@/lib/utils'
import type { GeoPlace } from '@/lib/api-types'
import countries from './countries.json'
import centers from './centers.json'

interface Peer { ip: string; upload: number; download: number; place: GeoPlace }
interface Origin { country_code?: string; latitude?: number; longitude?: number }
interface Snapshot { state: string; error?: string; as_of: string; seconds: number; total: number; peers: Peer[]; locations: { state: string; error?: string } }
type Position = [number, number]
// Small countries omitted by the 1:110m geometry still need honest points.
const positions: Record<string, Position> = { ...(centers as unknown as Record<string, Position>), SG: [103.82, 1.35], HK: [114.17, 22.32], MO: [113.55, 22.2], MT: [14.38, 35.94], MV: [73.22, 3.2], BH: [50.55, 26.02], AD: [1.52, 42.51], LI: [9.55, 47.16], MC: [7.42, 43.74], SM: [12.46, 43.94], VA: [12.45, 41.9] }
const empty: GeoJSON.FeatureCollection = { type: 'FeatureCollection', features: [] }
const countryCodes = [...new Set(countries.features.map(feature => feature.properties.country))]
function position(place: { latitude?: number; longitude?: number }, country?: string): Position | undefined {
  if (typeof place.latitude === 'number' && typeof place.longitude === 'number' && Number.isFinite(place.latitude) && Number.isFinite(place.longitude) && Math.abs(place.latitude) <= 90 && Math.abs(place.longitude) <= 180) return [place.longitude, place.latitude]
  return country ? positions[country.toUpperCase()] : undefined
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
  const targets = useRef(new Map<string, { feature: GeoJSON.Feature; coordinates?: Position[] }>())
  useEffect(() => {
    if (!container.current) return
    let instance: maplibregl.Map
    try {
      instance = new maplibregl.Map({ container: container.current,
        style: { version: 8, sources: { countries: { type: 'geojson', data: countries as GeoJSON.FeatureCollection } }, layers: [
          { id: 'ocean', type: 'background', paint: { 'background-color': '#f6f7f9' } },
          { id: 'land', type: 'fill', source: 'countries', paint: { 'fill-color': '#e2e6ec' } },
          { id: 'borders', type: 'line', source: 'countries', paint: { 'line-color': '#ffffff', 'line-width': 0.4 } },
        ] }, center: [25, 16], zoom: 0, minZoom: -3, interactive: expanded, attributionControl: false,
        // Keep the whole world visible even in a tall phone viewport.
        transformConstrain: expanded ? (center, zoom) => ({ center, zoom }) : undefined })
    } catch { setFailed(true); return }
    map.current = instance
    const fit = () => { instance.resize(); instance.fitBounds([[-180, -58], [180, 80]], { padding: 3, duration: 0 }) }
    const observer = new ResizeObserver(fit)
    observer.observe(container.current)
    instance.on('load', () => {
      for (const id of ['flows', 'dots', 'places']) instance.addSource(id, { type: 'geojson', data: empty })
      instance.addLayer({ id: 'flows', type: 'line', source: 'flows', paint: { 'line-color': ['get', 'color'], 'line-width': ['get', 'width'], 'line-opacity': ['*', 0.7, ['get', 'opacity']] } })
      instance.addLayer({ id: 'places', type: 'circle', source: 'places', paint: { 'circle-radius': 1.4, 'circle-color': ['get', 'color'], 'circle-opacity': ['get', 'opacity'], 'circle-stroke-opacity': ['get', 'opacity'], 'circle-stroke-width': 0.5, 'circle-stroke-color': '#ffffff' } })
      instance.addLayer({ id: 'dots', type: 'circle', source: 'dots', paint: { 'circle-radius': 1.2, 'circle-color': ['get', 'color'], 'circle-opacity': ['get', 'opacity'] } })
      fit()
      setReady(true)
    })
    instance.on('error', () => setFailed(true))
    return () => { observer.disconnect(); instance.remove(); map.current = null }
  }, [expanded])
  useEffect(() => {
    const instance = map.current
    if (!instance || !ready) return
    const palette = dark ? ['#526c58', '#686083', '#80754f', '#815f52', '#496e7a', '#735c70'] : ['#c5e2b4', '#d8cdf0', '#f2e7ad', '#efd0b5', '#bfe1e4', '#e9cddd']
    instance.setPaintProperty('ocean', 'background-color', expanded ? (dark ? '#1c3545' : '#d6effb') : (dark ? '#232731' : '#f6f7f9'))
    const countryColors: ExpressionSpecification = ['match', ['get', 'country'], countryCodes[0], palette[0], ...countryCodes.slice(1).flatMap((code, index) => [code, palette[(index + 1) % palette.length]]), palette[0]]
    instance.setPaintProperty('land', 'fill-color', expanded ? countryColors : (dark ? '#424b5b' : '#e2e6ec'))
    instance.setPaintProperty('borders', 'line-color', dark ? '#232731' : '#ffffff')
    const blue = dark ? '#8ab4ff' : '#1d4ed8'
    const start = origin ? position(origin, origin.country_code) : undefined
    const next: typeof targets.current = new Map()
    if (start) next.set(`place:${start}`, { feature: { type: 'Feature', properties: { color: dark ? '#f1f3f7' : '#171b22' }, geometry: { type: 'Point', coordinates: start } } })
    const locations = new Map<string, Peer>()
    for (const peer of peers) {
      const end = position(peer.place, peer.place.country)
      if (!end) continue
      const key = end.join(',')
      const previous = locations.get(key)
      locations.set(key, { ...peer, upload: (previous?.upload ?? 0) + peer.upload, download: (previous?.download ?? 0) + peer.download })
    }
    for (const [location, peer] of locations) {
      const end = position(peer.place, peer.place.country)
      if (!end) continue
      if (!start || start[0] !== end[0] || start[1] !== end[1]) next.set(`place:${end}`, { feature: { type: 'Feature', properties: { color: blue }, geometry: { type: 'Point', coordinates: end } } })
      if (!start || (start[0] === end[0] && start[1] === end[1])) continue
      const total = peer.download + peer.upload
      if (total <= 0) continue
      const coordinates = arc(start, end, 0)
      const width = Math.min(3.5, 0.6 + Math.log10(1 + total / 1024) * 0.7)
      next.set(`${location}:${start}`, { feature: { type: 'Feature', properties: { width }, geometry: { type: 'LineString', coordinates } }, coordinates })
    }
    targets.current = next
  }, [peers, origin, dark, ready, expanded])
  useEffect(() => {
    const instance = map.current
    if (!instance || !ready) return
    const set = (id: string, features: GeoJSON.Feature[]) => (instance.getSource(id) as GeoJSONSource).setData({ type: 'FeatureCollection', features })
    // Keep identity and phase independent of snapshot order and rank changes.
    const visible = new Map<string, { target: typeof targets.current extends Map<string, infer T> ? T : never; opacity: number; width: number; phase: number }>()
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
          visible.set(key, { target, opacity: 0, width: Number(target.feature.properties?.width || 0), phase: hash / 4294967296 })
        }
      }
      const lines: GeoJSON.Feature[] = [], points: GeoJSON.Feature[] = [], dots: GeoJSON.Feature[] = []
      for (const [key, visual] of visible) {
        const active = targets.current.has(key)
        visual.opacity += ((active ? 1 : 0) - visual.opacity) * blend
        if (!active && visual.opacity < 0.01) { visible.delete(key); continue }
        const path = visual.target
        visual.width += (Number(path.feature.properties?.width || 0) - visual.width) * blend
        const properties = { ...path.feature.properties, opacity: visual.opacity, width: visual.width }
        const feature = { ...path.feature, properties }
        if (!path.coordinates) { points.push(feature); continue }
        lines.push({ ...feature, properties: { ...properties, color: dark ? '#abb4c3' : '#5b6472' } })
        if (reduced.matches) continue
        // One dot travels out and back, taking four seconds in each direction.
        visual.phase = (visual.phase + elapsed / 8000) % 1
        const returning = visual.phase >= 0.5
        const progress = returning ? (1 - visual.phase) * 2 : visual.phase * 2
        const color = returning ? (dark ? '#8ab4ff' : '#1d4ed8') : (dark ? '#6bd4c6' : '#0f766e')
        const p = progress * 48
        const a = path.coordinates[Math.floor(p)], b = path.coordinates[Math.min(48, Math.floor(p) + 1)]
        dots.push({ type: 'Feature', properties: { ...properties, color }, geometry: { type: 'Point', coordinates: [a[0] + (b[0] - a[0]) * (p % 1), a[1] + (b[1] - a[1]) * (p % 1)] } })
      }
      set('flows', lines)
      set('places', points)
      set('dots', dots)
    }
    frame = requestAnimationFrame(animate)
    return () => cancelAnimationFrame(frame)
  }, [ready, expanded, dark])
  return <div className="relative h-full min-h-32 w-full overflow-hidden rounded-lg" style={expanded ? { backgroundColor: dark ? '#1c3545' : '#d6effb' } : undefined}><div ref={container} style={{ position: 'absolute', inset: 0 }} aria-hidden={!expanded} />{failed && <p className="absolute inset-0 flex items-center justify-center bg-muted p-3 text-xs text-muted-foreground">Map unavailable.</p>}</div>
}

export function WorldTraffic({ origin, embedded = false }: { origin?: Origin; embedded?: boolean }) {
  const [open, setOpen] = useState(false)
  const [expanded, setExpanded] = useState(false)
  const preview = useRef<HTMLButtonElement>(null)
  const { resolvedTheme } = useTheme()
  const query = useQuery({ queryKey: ['gateway', 'world-traffic'], queryFn: () => api.get<Snapshot>('/api/gateway/world-traffic'), refetchInterval: 2000 })
  const data = query.data
  const stale = data?.as_of && Date.now() - Date.parse(data.as_of) > 10000
  const ready = !query.isError && !stale && data?.state === 'ready'
  const peers = ready ? data.peers : []
  const share = data?.total ? Math.round(peers.reduce((n, p) => n + p.download + p.upload, 0) / data.total * 100) : 0
  const unknown = peers.filter(p => !position(p.place, p.place.country)).length
  const status = query.isError || stale ? 'Unavailable' : !data ? 'Loading…' : data.state === 'disabled' ? 'Counting off' : data.state === 'warming' ? 'Measuring…' : data.state !== 'ready' ? 'Unavailable' : peers.length ? `Top ${peers.length} · ${share}%` : 'No observed traffic'
  return <section aria-label="Traffic map" className={embedded ? 'flex min-h-0 w-full min-w-0 flex-col gap-1' : 'col-span-2 flex min-w-0 flex-col gap-2 rounded-xl border bg-card p-3 shadow-xs sm:p-4 lg:col-span-1 lg:row-span-3'}>
    {!embedded && <h2 className="flex h-6 items-center text-sm font-semibold"><button onClick={() => setOpen(true)} className="relative rounded-sm before:absolute before:inset-x-0 before:-inset-y-2.5 focus-visible:outline-2 focus-visible:outline-ring" aria-label="Map details">Map</button></h2>}
    <button ref={preview} type="button" onClick={() => setExpanded(true)} aria-label="Open fullscreen map" aria-haspopup="dialog" className={embedded ? 'flex min-h-32 flex-1 cursor-zoom-in rounded-lg focus-visible:outline-2 focus-visible:outline-ring' : '-mx-1 -mb-1 flex min-h-44 flex-1 cursor-zoom-in rounded-lg focus-visible:outline-2 focus-visible:outline-ring sm:-mx-2 sm:-mb-2'}><TrafficMap peers={peers} origin={origin} dark={resolvedTheme === 'dark'} /></button>
    {(!ready || !peers.length) && <p className="text-xs text-muted-foreground">{status}</p>}
    <Dialog open={expanded} onOpenChange={setExpanded}><DialogContent finalFocus={preview} className="flex h-dvh max-h-dvh w-screen max-w-none flex-col rounded-none sm:max-w-none">
      <DialogHeader><DialogTitle>Map</DialogTitle><DialogDescription><span className="text-receive">↓ Download</span> · <span className="text-transmit">↑ Upload</span> · {status}</DialogDescription></DialogHeader>
      <div className="min-h-0 flex-1" role="region" aria-label="World traffic map, drag to pan and scroll to zoom"><TrafficMap peers={peers} origin={origin} dark={resolvedTheme === 'dark'} expanded /></div>
      <p className="text-xs text-muted-foreground">Top remote IPs over the last 30 seconds · {share}% of observed traffic. Connections ending between samples and traffic bypassing conntrack may be missed.{unknown ? ` ${unknown} locations unknown.` : ''}</p>
      {data?.error && <p role="alert" className="text-xs text-destructive">{data.error}</p>}
      {data?.locations?.error && <p role="alert" className="text-xs text-destructive">{data.locations.error}</p>}
      <p className="text-xs text-muted-foreground">Map: <a href="https://www.naturalearthdata.com" target="_blank" rel="noreferrer" className="underline">Natural Earth</a> (public domain) · IP locations: <a href="https://ip.sb" target="_blank" rel="noreferrer" className="underline">ip.sb</a></p>
    </DialogContent></Dialog>
    <Dialog open={open} onOpenChange={setOpen}><DialogContent className="max-h-[85dvh] overflow-y-auto"><DialogHeader><DialogTitle>Map</DialogTitle><DialogDescription>Remote IPs by upload and download in the last 30 seconds.{unknown ? ` ${unknown} locations unknown.` : ''}</DialogDescription></DialogHeader>
      <p className="text-sm text-muted-foreground">{status}{data?.seconds ? ` · ${Math.round(data.seconds)}s sampled` : ''}. {data?.state === 'disabled' ? 'Connection byte accounting is off. Enable Gateway traffic counting and apply its configuration.' : 'Counts include router and forwarded connections observed in Linux conntrack. Connections ending between samples and traffic bypassing conntrack may be missed.'} {!origin && 'Router location is unknown; only remote points are shown.'}</p>
      {data?.error && <p className="break-words text-sm text-destructive">{data.error}</p>}
      {data?.locations?.error && <p className="break-words text-sm text-destructive">{data.locations.error}</p>}
      <ul className="divide-y">{peers.map(peer => <li key={peer.ip} className="space-y-1 py-3 text-sm"><div className="flex flex-wrap justify-between gap-2"><span className="break-all font-mono">{peer.ip}</span><span>{[peer.place.city || peer.place.region, peer.place.country_name].filter(Boolean).join(', ') || 'Location unknown'}</span></div><div className="flex justify-between gap-2 text-xs tabular-nums"><span className="text-receive">↓ {formatRate(peer.download)}</span><span className="text-transmit">↑ {formatRate(peer.upload)}</span></div></li>)}</ul>
      <p className="text-xs text-muted-foreground">IP locations: {data?.locations?.state || 'Unavailable'} · <a href="https://ip.sb" target="_blank" rel="noreferrer" className="underline">ip.sb</a>. Map: <a href="https://www.naturalearthdata.com" target="_blank" rel="noreferrer" className="underline">Natural Earth</a> (public domain), rendered with MapLibre.</p>
    </DialogContent></Dialog>
  </section>
}
