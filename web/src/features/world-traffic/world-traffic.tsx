import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTheme } from 'next-themes'
import { Info } from 'lucide-react'
import * as maplibregl from 'maplibre-gl'
import type { GeoJSONSource } from 'maplibre-gl'
import type * as GeoJSON from 'geojson'
import workerUrl from 'maplibre-gl/dist/maplibre-gl-worker.mjs?worker&url'
import 'maplibre-gl/dist/maplibre-gl.css'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { api } from '@/lib/api'
import { formatRate } from '@/lib/utils'
import countries from './countries.json'
import centers from './centers.json'

interface Peer { ip: string; upload: number; download: number; place: { country?: string; country_name?: string } }
interface Snapshot { state: string; error?: string; as_of: string; seconds: number; total: number; peers: Peer[]; locations: { state: string } }
type Position = [number, number]
// Small countries omitted by the 1:110m geometry still need honest points.
const positions: Record<string, Position> = { ...(centers as unknown as Record<string, Position>), SG: [103.82, 1.35], HK: [114.17, 22.32], MO: [113.55, 22.2], MT: [14.38, 35.94], MV: [73.22, 3.2], BH: [50.55, 26.02], AD: [1.52, 42.51], LI: [9.55, 47.16], MC: [7.42, 43.74], SM: [12.46, 43.94], VA: [12.45, 41.9] }
const empty: GeoJSON.FeatureCollection = { type: 'FeatureCollection', features: [] }
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

function TrafficMap({ peers, origin, dark }: { peers: Peer[]; origin?: string; dark: boolean }) {
  const container = useRef<HTMLDivElement>(null)
  const map = useRef<maplibregl.Map | null>(null)
  const [ready, setReady] = useState(false)
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    if (!container.current) return
    let instance: maplibregl.Map
    try {
      instance = new maplibregl.Map({ container: container.current,
        style: { version: 8, sources: { countries: { type: 'geojson', data: countries as GeoJSON.FeatureCollection } }, layers: [
          { id: 'ocean', type: 'background', paint: { 'background-color': '#f6f7f9' } },
          { id: 'land', type: 'fill', source: 'countries', paint: { 'fill-color': '#e2e6ec' } },
          { id: 'borders', type: 'line', source: 'countries', paint: { 'line-color': '#ffffff', 'line-width': 0.4 } },
        ] }, center: [25, 16], zoom: 0, minZoom: -3, interactive: false, attributionControl: false })
    } catch { setFailed(true); return }
    map.current = instance
    const fit = () => { instance.resize(); instance.fitBounds([[-180, -58], [180, 80]], { padding: 3, duration: 0 }) }
    const observer = new ResizeObserver(fit)
    observer.observe(container.current)
    instance.on('load', () => {
      for (const id of ['flows', 'dots', 'places']) instance.addSource(id, { type: 'geojson', data: empty })
      instance.addLayer({ id: 'flows', type: 'line', source: 'flows', paint: { 'line-color': ['get', 'color'], 'line-width': ['get', 'width'], 'line-opacity': 0.7 } })
      instance.addLayer({ id: 'places', type: 'circle', source: 'places', paint: { 'circle-radius': 2.4, 'circle-color': ['get', 'color'], 'circle-stroke-width': 1, 'circle-stroke-color': '#ffffff' } })
      instance.addLayer({ id: 'dots', type: 'circle', source: 'dots', paint: { 'circle-radius': 1.8, 'circle-color': ['get', 'color'] } })
      fit()
      setReady(true)
    })
    instance.on('error', () => setFailed(true))
    return () => { observer.disconnect(); instance.remove(); map.current = null }
  }, [])
  useEffect(() => {
    const instance = map.current
    if (!instance || !ready) return
    instance.setPaintProperty('ocean', 'background-color', dark ? '#232731' : '#f6f7f9')
    instance.setPaintProperty('land', 'fill-color', dark ? '#424b5b' : '#e2e6ec')
    instance.setPaintProperty('borders', 'line-color', dark ? '#232731' : '#ffffff')
    const blue = dark ? '#8ab4ff' : '#1d4ed8', teal = dark ? '#6bd4c6' : '#0f766e'
    const start = origin ? positions[origin.toUpperCase()] : undefined
    const lines: GeoJSON.Feature[] = [], points: GeoJSON.Feature[] = []
    const paths: { coordinates: Position[]; color: string; reverse: boolean }[] = []
    if (start) points.push({ type: 'Feature', properties: { color: dark ? '#f1f3f7' : '#171b22' }, geometry: { type: 'Point', coordinates: start } })
    for (const peer of peers) {
      const end = peer.place.country ? positions[peer.place.country] : undefined
      if (!end) continue
      points.push({ type: 'Feature', properties: { color: blue }, geometry: { type: 'Point', coordinates: end } })
      if (!start || peer.place.country === origin?.toUpperCase()) continue
      for (const direction of ['download', 'upload'] as const) {
        if (peer[direction] <= 0) continue
        const color = direction === 'download' ? blue : teal
        const coordinates = arc(start, end, direction === 'download' ? 1.5 : -1.5)
        const width = Math.min(3.5, 0.6 + Math.log10(1 + peer[direction] / 1024) * 0.7)
        lines.push({ type: 'Feature', properties: { color, width }, geometry: { type: 'LineString', coordinates } })
        paths.push({ coordinates, color, reverse: direction === 'download' })
      }
    }
    const set = (id: string, features: GeoJSON.Feature[]) => (instance.getSource(id) as GeoJSONSource).setData({ type: 'FeatureCollection', features })
    set('flows', lines)
    set('places', points)
    const reduced = matchMedia('(prefers-reduced-motion: reduce)')
    const animate = () => {
      const t = (performance.now() % 4000) / 4000
      const features: GeoJSON.Feature[] = reduced.matches || document.hidden ? [] : paths.map((path, index) => {
        let progress = (t + index / Math.max(1, paths.length)) % 1
        if (path.reverse) progress = 1 - progress
        const p = progress * 48
        const a = path.coordinates[Math.floor(p)], b = path.coordinates[Math.min(48, Math.floor(p) + 1)]
        return { type: 'Feature', properties: { color: path.color }, geometry: { type: 'Point', coordinates: [a[0] + (b[0] - a[0]) * (p % 1), a[1] + (b[1] - a[1]) * (p % 1)] } }
      })
      set('dots', features)
    }
    animate()
    const timer = window.setInterval(animate, 100)
    return () => clearInterval(timer)
  }, [peers, origin, dark, ready])
  return <div className="relative h-36 overflow-hidden rounded-lg"><div ref={container} style={{ position: 'absolute', inset: 0 }} aria-hidden="true" />{failed && <p className="absolute inset-0 flex items-center justify-center bg-muted p-3 text-xs text-muted-foreground">Map unavailable. View traffic details.</p>}</div>
}

export function WorldTraffic({ origin }: { origin?: string }) {
  const [open, setOpen] = useState(false)
  const { resolvedTheme } = useTheme()
  const query = useQuery({ queryKey: ['gateway', 'world-traffic'], queryFn: () => api.get<Snapshot>('/api/gateway/world-traffic'), refetchInterval: 2000 })
  const data = query.data
  const stale = data?.as_of && Date.now() - Date.parse(data.as_of) > 10000
  const ready = !query.isError && !stale && data?.state === 'ready'
  const peers = ready ? data.peers : []
  const share = data?.total ? Math.round(peers.reduce((n, p) => n + p.download + p.upload, 0) / data.total * 100) : 0
  const unknown = peers.filter(p => !p.place.country || !positions[p.place.country]).length
  const status = query.isError || stale ? 'Unavailable' : !data ? 'Loading…' : data.state === 'disabled' ? 'Counting off' : data.state === 'warming' ? 'Measuring…' : data.state !== 'ready' ? 'Unavailable' : peers.length ? `Top ${peers.length} · ${share}%` : 'No observed traffic'
  return <section className="col-span-2 flex min-w-0 flex-col gap-2 rounded-xl border bg-card p-3 shadow-xs sm:p-4 lg:col-span-1 lg:row-span-3">
    <h2 className="flex h-6 items-center justify-between text-sm font-semibold">World traffic<button onClick={() => setOpen(true)} className="flex min-h-11 min-w-11 items-center justify-end rounded-sm text-muted-foreground focus-visible:outline-2 focus-visible:outline-ring sm:min-h-6 sm:min-w-6" aria-label="World traffic details"><Info className="size-4" /></button></h2>
    <div className="min-h-36 flex-1"><TrafficMap peers={peers} origin={origin} dark={resolvedTheme === 'dark'} /></div>
    <div className="flex flex-wrap items-center justify-between gap-x-2 gap-y-1 text-xs text-muted-foreground"><span><span className="text-receive">↓</span> Download <span className="ml-2 text-transmit">↑</span> Upload</span><button onClick={() => setOpen(true)} className="min-h-8 rounded-sm text-left hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring">{status}{unknown ? ` · ${unknown} unknown` : ''}</button></div>
    <Dialog open={open} onOpenChange={setOpen}><DialogContent className="max-h-[85dvh] overflow-y-auto"><DialogHeader><DialogTitle>World traffic</DialogTitle><DialogDescription>Top 10 remote IPs by upload and download in the last 30 seconds. Locations indicate countries, not exact endpoints.</DialogDescription></DialogHeader>
      <p className="text-sm text-muted-foreground">{status}{data?.seconds ? ` · ${Math.round(data.seconds)}s sampled` : ''}. {data?.state === 'disabled' ? 'Connection byte accounting is off. Enable Gateway traffic counting and apply its configuration.' : 'Counts include router and forwarded connections observed in Linux conntrack. Connections ending between samples and traffic bypassing conntrack may be missed.'} {!origin && 'Router location is unknown; only remote points are shown.'}</p>
      {data?.error && <p className="break-words text-sm text-destructive">{data.error}</p>}
      <ul className="divide-y">{peers.map(peer => <li key={peer.ip} className="space-y-1 py-3 text-sm"><div className="flex flex-wrap justify-between gap-2"><span className="break-all font-mono">{peer.ip}</span><span>{peer.place.country_name || 'Location unknown'}</span></div><div className="flex justify-between gap-2 text-xs tabular-nums"><span className="text-receive">↓ {formatRate(peer.download)}</span><span className="text-transmit">↑ {formatRate(peer.upload)}</span></div></li>)}</ul>
      <p className="text-xs text-muted-foreground">Local GeoIP: {data?.locations?.state || 'Unavailable'} · <a href="https://db-ip.com" target="_blank" rel="noreferrer" className="underline">DB-IP</a>. Map: <a href="https://www.naturalearthdata.com" target="_blank" rel="noreferrer" className="underline">Natural Earth</a> (public domain), rendered with MapLibre.</p>
    </DialogContent></Dialog>
  </section>
}
