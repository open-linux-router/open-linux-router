import { useLayoutEffect, useRef, useState } from 'react'
import { sourceLabels, type TrafficFlow } from './traffic-model'

type Node = { id: string; title: string; packets: number; x: number; y: number; width: number; color: string; detail: string }
type Band = { id: string; from: Node; to: Node; fromX: number; toX: number; width: number; color: string; detail: string }

const barHeight = 12
const allowedColor = 'var(--success-foreground)'
const blockedColor = 'var(--warning-foreground)'
const decisionColor = (verdict: string) => verdict === 'allowed' ? allowedColor : blockedColor

function lines(title: string, limit = 25): string[] {
  const result: string[] = []
  for (const word of title.split(' ')) {
    const last = result.length - 1
    if (last >= 0 && `${result[last]} ${word}`.length <= limit) result[last] += ` ${word}`
    else result.push(word)
  }
  return result
}

/** Three rows on one packet scale: sources, sourced categories, decisions.
 * All ribbons travel down; horizontal scrolling starts at the first node. */
export function VerticalSankey({ flows }: { flows: TrafficFlow[] }) {
  const container = useRef<HTMLDivElement>(null)
  const [availableWidth, setAvailableWidth] = useState(900)
  useLayoutEffect(() => {
    const element = container.current
    if (!element) return
    const resize = new ResizeObserver(() => setAvailableWidth(element.clientWidth))
    resize.observe(element)
    return () => resize.disconnect()
  }, [])
  const sourceOrder = ['outside', 'inside', 'local'] as const
  const ordered = sourceOrder.flatMap(source => flows.filter(flow => flow.source === source))
  const width = Math.max(availableWidth, 900, ordered.length * 96 + 48)
  const labelWidth = Math.max(84, Math.min(176, (width - 48) / Math.max(1, ordered.length) - 12))
  const total = flows.reduce((sum, flow) => sum + flow.packets, 0)
  const nodes: Node[] = []
  const bands: Band[] = []
  const sourceY = 96
  const categoryY = 340
  const decisionY = 560
  const height = 660

  const sourceRows = (rows: TrafficFlow[]) => [...new Set(rows.map(flow => flow.source))].map(source => ({
    id: `source:${source}`, title: sourceLabels[source],
    packets: rows.filter(flow => flow.source === source).reduce((sum, flow) => sum + flow.packets, 0),
    color: source === 'outside' ? 'var(--receive)' : 'var(--transmit)', detail: sourceLabels[source],
  }))
  const categories = (rows: TrafficFlow[]) => rows.map(flow => ({
    id: flow.id, title: flow.category, packets: flow.packets, color: decisionColor(flow.verdict),
    detail: `${sourceLabels[flow.source]} → ${flow.category} → ${flow.verdict === 'allowed' ? 'Allowed' : 'Blocked'}\n${flow.packets.toLocaleString()} packets · ${flow.bytes.toLocaleString()} bytes\n${flow.descriptions.join('\n')}`,
  }))
  const decisions = (['allowed', 'blocked'] as const).flatMap(verdict => {
    const packets = flows.filter(flow => flow.verdict === verdict).reduce((sum, flow) => sum + flow.packets, 0)
    return packets ? [{ id: verdict, title: verdict === 'allowed' ? 'Allowed' : 'Blocked', packets, color: decisionColor(verdict), detail: 'Total from all sources' }] : []
  })
  const rows = [sourceRows(ordered), categories(ordered), decisions]
  // Reserve label space even for tiny but nonzero paths, while preserving the
  // same pixels-per-packet for every bar and ribbon across the entire graph.
  let low = 0
  let high = (width - 48) / total
  for (let i = 0; i < 40; i++) {
    const candidate = (low + high) / 2
    if (rows.every((row, index) => row.reduce((sum, node) => sum + Math.max(index === 1 ? labelWidth : 176, node.packets * candidate), 0) + Math.max(0, row.length - 1) * 12 <= width - 48)) low = candidate
    else high = candidate
  }
  const scale = low
  const place = (row: Omit<Node, 'x' | 'y' | 'width'>[], y: number) => {
    const space = y === categoryY ? labelWidth : 176
    const span = row.reduce((sum, node) => sum + Math.max(space, node.packets * scale), 0) + Math.max(0, row.length - 1) * 12
    let cursor = (width - span) / 2
    return row.map(node => {
      const cell = Math.max(space, node.packets * scale)
      const placed = { ...node, x: cursor + cell / 2, y, width: node.packets * scale }
      cursor += cell + 12
      nodes.push(placed)
      return placed
    })
  }
  const topSources = place(rows[0], sourceY)
  const middleCategories = place(rows[1], categoryY)
  const bottomDecisions = place(rows[2], decisionY)

  const used = new Map<string, number>()
  const connect = (items: TrafficFlow[], sources: Node[], categories: Node[]) => {
    const offset = (node: Node, packets: number) => {
      const prior = used.get(node.id) ?? 0
      used.set(node.id, prior + packets)
      return node.x - node.width / 2 + prior * scale
    }
    items.forEach((flow, i) => {
      const source = sources.find(node => node.id === `source:${flow.source}`)!
      const category = categories[i]
      const decision = bottomDecisions.find(node => node.id === flow.verdict)!
      const detail = category.detail
      bands.push({ id: `${flow.id}:source`, from: source, to: category, fromX: offset(source, flow.packets), toX: category.x - category.width / 2, width: category.width, color: source.color, detail })
      bands.push({ id: `${flow.id}:decision`, from: category, to: decision, fromX: category.x - category.width / 2, toX: offset(decision, flow.packets), width: category.width, color: category.color, detail })
    })
  }
  connect(ordered, topSources, middleCategories)

  return (
    <div ref={container} className="mt-4 overflow-x-auto focus-visible:outline-2 focus-visible:outline-primary" tabIndex={0} role="region" aria-label="Vertical firewall Sankey: source, semantic category and decision" aria-describedby="firewall-chart-scroll">
      <svg width={width} height={height} role="img" aria-label="Three layers from top to bottom: sources, semantic categories by source, Allowed or Blocked" className="text-foreground">
        {bands.map(band => {
          const y1 = band.from.y + barHeight
          const y2 = band.to.y
          const mid = (y1 + y2) / 2
          const x1 = band.fromX
          const x2 = band.toX
          const w = band.width
          return (
            <path key={band.id} d={`M ${x1} ${y1} C ${x1} ${mid}, ${x2} ${mid}, ${x2} ${y2} L ${x2 + w} ${y2} C ${x2 + w} ${mid}, ${x1 + w} ${mid}, ${x1 + w} ${y1} Z`} fill={band.color} fillOpacity={0.22}>
              <title>{band.detail}</title>
            </path>
          )
        })}
        {nodes.map(node => {
          const flow = ordered.find(flow => flow.id === node.id)
          const label = lines(node.title, Math.floor((flow ? labelWidth : 176) / 7))
          const below = node.y === decisionY
          const origin = flow ? lines(sourceLabels[flow.source], Math.floor(labelWidth / 6)) : []
          const labelY = below ? node.y + barHeight + 24 : node.y - 16 - (label.length + origin.length + (flow ? 1 : 0)) * 18
          return (
            <g key={node.id}>
              <rect x={node.x - node.width / 2} y={node.y} width={node.width} height={barHeight} fill={node.color}>
                <title>{`${node.detail}\n${node.packets.toLocaleString()} packets`}</title>
              </rect>
              <text x={node.x} y={labelY} fill="currentColor" fontSize={flow ? 13 : 14} textAnchor="middle">
                {label.map((line, index) => <tspan key={index} x={node.x} dy={index ? 18 : 0}>{line}</tspan>)}
                {origin.map((line, index) => <tspan key={`origin:${index}`} x={node.x} dy={18} fontSize={12}>{line}</tspan>)}
                <tspan x={node.x} dy={18}>{node.packets.toLocaleString()}{flow ? '' : ' packets'}</tspan>
                {flow && <tspan x={node.x} dy={18}>packets</tspan>}
              </text>
            </g>
          )
        })}
      </svg>
    </div>
  )
}
