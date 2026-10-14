import { useLayoutEffect, useRef } from 'react'
import { sourceLabels, type TrafficFlow } from './traffic-model'

type Node = { id: string; title: string; packets: number; x: number; y: number; width: number; color: string; detail: string }
type Band = { id: string; from: Node; to: Node; fromX: number; toX: number; width: number; color: string; detail: string }

const barHeight = 12
const allowedColor = 'var(--success-foreground)'
const blockedColor = 'var(--warning-foreground)'
const decisionColor = (verdict: string) => verdict === 'allowed' ? allowedColor : blockedColor

function lines(title: string): string[] {
  const result: string[] = []
  for (const word of title.split(' ')) {
    const last = result.length - 1
    if (last >= 0 && `${result[last]} ${word}`.length <= 25) result[last] += ` ${word}`
    else result.push(word)
  }
  return result
}

/** One shared packet scale, with external arrivals above the decisions and
 * trusted/local arrivals below. All ribbons flow toward the central decisions. */
export function VerticalSankey({ flows }: { flows: TrafficFlow[] }) {
  const container = useRef<HTMLDivElement>(null)
  const outside = flows.filter(flow => flow.source === 'outside')
  const inside = flows.filter(flow => flow.source !== 'outside')
  const width = Math.max(900, Math.max(outside.length, inside.length) * 200 + 48)
  useLayoutEffect(() => {
    const element = container.current
    if (element) element.scrollLeft = Math.max(0, (width - element.clientWidth) / 2)
  }, [width])
  const total = flows.reduce((sum, flow) => sum + flow.packets, 0)
  const nodes: Node[] = []
  const bands: Band[] = []
  const outsideY = outside.length ? 96 : 0
  const decisionY = outside.length ? 360 : 96
  const insideCategoryY = decisionY + 200
  const insideSourceY = decisionY + 370
  const height = inside.length ? insideSourceY + 96 : decisionY + 112

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
  const rows = [sourceRows(outside), categories(outside), decisions, categories(inside), sourceRows(inside)]
  // Reserve label space even for tiny but nonzero paths, while preserving the
  // same pixels-per-packet for every bar and ribbon across the entire graph.
  let low = 0
  let high = (width - 48) / total
  for (let i = 0; i < 40; i++) {
    const candidate = (low + high) / 2
    if (rows.every(row => row.reduce((sum, node) => sum + Math.max(176, node.packets * candidate), 0) + Math.max(0, row.length - 1) * 24 <= width - 48)) low = candidate
    else high = candidate
  }
  const scale = low
  const place = (row: Omit<Node, 'x' | 'y' | 'width'>[], y: number) => {
    const span = row.reduce((sum, node) => sum + Math.max(176, node.packets * scale), 0) + Math.max(0, row.length - 1) * 24
    let cursor = (width - span) / 2
    return row.map(node => {
      const cell = Math.max(176, node.packets * scale)
      const placed = { ...node, x: cursor + cell / 2, y, width: node.packets * scale }
      cursor += cell + 24
      nodes.push(placed)
      return placed
    })
  }
  const topSources = place(rows[0], outsideY)
  const topCategories = place(rows[1], outsideY + 140)
  const middle = place(rows[2], decisionY)
  const bottomCategories = place(rows[3], insideCategoryY)
  const bottomSources = place(rows[4], insideSourceY)

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
      const decision = middle.find(node => node.id === flow.verdict)!
      const detail = category.detail
      bands.push({ id: `${flow.id}:source`, from: source, to: category, fromX: offset(source, flow.packets), toX: category.x - category.width / 2, width: category.width, color: source.color, detail })
      bands.push({ id: `${flow.id}:decision`, from: category, to: decision, fromX: category.x - category.width / 2, toX: offset(decision, flow.packets), width: category.width, color: category.color, detail })
    })
  }
  connect(outside, topSources, topCategories)
  connect(inside, bottomSources, bottomCategories)

  return (
    <div ref={container} className="mt-4 overflow-x-auto focus-visible:outline-2 focus-visible:outline-primary" tabIndex={0} role="region" aria-label="Vertical firewall Sankey: external sources above, LAN and router sources below">
      <svg width={width} height={height} role="img" aria-label="Traffic flows toward Allowed or Blocked in the middle" className="text-foreground">
        {bands.map(band => {
          const down = band.from.y < band.to.y
          const y1 = band.from.y + (down ? barHeight : 0)
          const y2 = band.to.y + (down ? 0 : barHeight)
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
        {[...topSources, ...bottomSources].map(node => {
          const down = node.y === outsideY
          const y = node.y + (down ? 36 : -24)
          return <path key={`direction:${node.id}`} d={`M ${node.x - 5} ${y + (down ? -5 : 5)} L ${node.x} ${y} L ${node.x + 5} ${y + (down ? -5 : 5)}`} fill="none" stroke={node.color} strokeWidth={2} aria-hidden="true" />
        })}
        {nodes.map(node => {
          const label = lines(node.title)
          const below = node.y >= insideCategoryY
          const labelY = below ? node.y + barHeight + 24 : node.y - 16 - label.length * 18
          return (
            <g key={node.id}>
              <rect x={node.x - node.width / 2} y={node.y} width={node.width} height={barHeight} fill={node.color}>
                <title>{`${node.detail}\n${node.packets.toLocaleString()} packets`}</title>
              </rect>
              <text x={node.x} y={labelY} fill="currentColor" fontSize={14} textAnchor="middle">
                {label.map((line, index) => <tspan key={index} x={node.x} dy={index ? 18 : 0}>{line}</tspan>)}
                <tspan x={node.x} dy={18}>{node.packets.toLocaleString()} packets</tspan>
              </text>
            </g>
          )
        })}
      </svg>
    </div>
  )
}
