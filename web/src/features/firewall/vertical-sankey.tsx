import { useLayoutEffect, useRef, useState } from 'react'
import { sourceLabels, type TrafficFlow } from './traffic-model'

const barHeight = 12
const centerY = 360
const centerHeight = 48
const topY = 200
const bottomY = 568
const height = 808

function lines(title: string, limit: number): string[] {
  const result: string[] = []
  for (const word of title.split(' ')) {
    const last = result.length - 1
    if (last >= 0 && `${result[last]} ${word}`.length <= limit) result[last] += ` ${word}`
    else result.push(word)
  }
  return result
}

/** Both outer rows are arrivals at one virtual OLR/firewall boundary.
 * Vertical position distinguishes origins; it does not imply forwarding. */
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

  const outside = flows.filter(flow => flow.source === 'outside')
  const inside = (['inside', 'local'] as const).flatMap(source => flows.filter(flow => flow.source === source))
  const rows = [outside, inside]
  const maxNodes = Math.max(1, ...rows.map(row => row.length))
  const width = Math.max(availableWidth, 900, maxNodes * 120 + 48)
  const labelWidth = Math.max(108, Math.min(176, (width - 48) / maxNodes - 12))
  const total = flows.reduce((sum, flow) => sum + flow.packets, 0)

  // Labels reserve space without enlarging tiny measured bars. All links and
  // both sides of the virtual boundary share the same pixels-per-packet.
  let low = 0
  let high = (width - 48) / total
  for (let i = 0; i < 40; i++) {
    const candidate = (low + high) / 2
    if (rows.every(row => row.reduce((sum, flow) => sum + Math.max(labelWidth, flow.packets * candidate), 0) + Math.max(0, row.length - 1) * 12 <= width - 48)) low = candidate
    else high = candidate
  }
  const scale = low
  const centerWidth = total * scale
  const centerLeft = (width - centerWidth) / 2
  let offset = 0
  const nodes = rows.flatMap((row, rowIndex) => {
    const span = row.reduce((sum, flow) => sum + Math.max(labelWidth, flow.packets * scale), 0) + Math.max(0, row.length - 1) * 12
    let cursor = (width - span) / 2
    return row.map(flow => {
      const measuredWidth = flow.packets * scale
      const cell = Math.max(labelWidth, measuredWidth)
      const node = { flow, x: cursor + cell / 2, y: rowIndex === 0 ? topY : bottomY, width: measuredWidth, centerX: centerLeft + offset * scale, above: rowIndex === 0 }
      cursor += cell + 12
      offset += flow.packets
      return node
    })
  })
  const color = (flow: TrafficFlow) => flow.verdict === 'allowed' ? 'var(--success-foreground)' : 'var(--warning-foreground)'
  const detail = (flow: TrafficFlow) => `${sourceLabels[flow.source]} → OLR / Firewall\n${flow.category} · ${flow.verdict === 'allowed' ? 'Allowed' : 'Blocked'}\n${flow.packets.toLocaleString()} packets · ${flow.bytes.toLocaleString()} bytes\n${flow.descriptions.join('\n')}`

  return (
    <div ref={container} className="mt-4 overflow-x-auto focus-visible:outline-2 focus-visible:outline-primary" tabIndex={0} role="region" aria-label="Firewall Sankey: external arrivals above OLR, LAN and local arrivals below OLR" aria-describedby="firewall-chart-scroll">
      <svg width={width} height={height} role="img" aria-label="External traffic and LAN or local traffic both arrive at the central OLR firewall" className="text-foreground">
        {outside.length > 0 && <text x={width / 2} y={24} fill="currentColor" fontSize={14} fontWeight={500} textAnchor="middle">From external networks</text>}
        {inside.length > 0 && <text x={width / 2} y={height - 24} fill="currentColor" fontSize={14} fontWeight={500} textAnchor="middle">From LAN / trusted networks and this router</text>}
        {nodes.map(node => {
          const x1 = node.x - node.width / 2
          const y1 = node.above ? node.y + barHeight : node.y
          const x2 = node.centerX
          const y2 = node.above ? centerY : centerY + centerHeight
          const mid = (y1 + y2) / 2
          const w = node.width
          return (
            <path key={`band:${node.flow.id}`} d={`M ${x1} ${y1} C ${x1} ${mid}, ${x2} ${mid}, ${x2} ${y2} L ${x2 + w} ${y2} C ${x2 + w} ${mid}, ${x1 + w} ${mid}, ${x1 + w} ${y1} Z`} fill={color(node.flow)} fillOpacity={0.22}>
              <title>{detail(node.flow)}</title>
            </path>
          )
        })}
        <g data-node="olr">
          <rect x={centerLeft} y={centerY} width={centerWidth} height={centerHeight} rx={8} fill="var(--muted)" stroke="var(--transmit)">
            <title>{`OLR / Firewall\n${total.toLocaleString()} arriving packets across all sources`}</title>
          </rect>
          <text x={width / 2} y={centerY + 29} fill="currentColor" fontSize={16} fontWeight={600} textAnchor="middle">OLR · Firewall</text>
        </g>
        {nodes.map(node => {
          const title = lines(node.flow.category, Math.floor(labelWidth / 7))
          const origin = lines(sourceLabels[node.flow.source], Math.floor(labelWidth / 6))
          const labelY = node.above ? node.y - 16 - (title.length + origin.length + 2) * 18 : node.y + barHeight + 24
          const arrowY = node.above ? node.y + 36 : node.y - 24
          const arrowBack = node.above ? -5 : 5
          return (
            <g key={node.flow.id} data-source={node.flow.source}>
              <rect x={node.x - node.width / 2} y={node.y} width={node.width} height={barHeight} fill={color(node.flow)}>
                <title>{detail(node.flow)}</title>
              </rect>
              <path d={`M ${node.x - 5} ${arrowY + arrowBack} L ${node.x} ${arrowY} L ${node.x + 5} ${arrowY + arrowBack}`} fill="none" stroke={color(node.flow)} strokeWidth={2} aria-hidden="true" />
              <text x={node.x} y={labelY} fill="currentColor" fontSize={13} textAnchor="middle">
                {title.map((line, index) => <tspan key={index} x={node.x} dy={index ? 18 : 0}>{line}</tspan>)}
                {origin.map((line, index) => <tspan key={`origin:${index}`} x={node.x} dy={18} fontSize={12}>{line}</tspan>)}
                <tspan x={node.x} dy={18} fill={color(node.flow)}>{node.flow.verdict === 'allowed' ? 'Allowed' : 'Blocked'}</tspan>
                <tspan x={node.x} dy={18}>{node.flow.packets.toLocaleString()}</tspan>
                <tspan x={node.x} dy={18}>packets</tspan>
              </text>
            </g>
          )
        })}
      </svg>
    </div>
  )
}
