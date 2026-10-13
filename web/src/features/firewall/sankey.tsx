import { useEffect, useMemo, useRef } from 'react'
import { init, use as registerCharts } from 'echarts/core'
import { SankeyChart } from 'echarts/charts'
import { SVGRenderer } from 'echarts/renderers'
import { useDeviceList } from '@/features/devices/queries'
import { useGatewayTraffic } from '@/features/gateway/queries'
import type { FirewallStatus } from '@/lib/api-types'

registerCharts([SankeyChart, SVGRenderer])
type Node = { name: string; title: string; depth: number; token: string }
type FlowLink = { source: string; target: string; value: number }

export function FirewallSankey({ status }: { status: FirewallStatus }) {
  const devices = useDeviceList()
  const traffic = useGatewayTraffic()
  const container = useRef<HTMLDivElement>(null)
  const graph = useMemo(() => {
    const nodes = new Map<string, Node>()
    const links = new Map<string, FlowLink>()
    const groups = new Map<string, Set<string>>()
    for (const device of devices.data?.devices ?? []) {
      for (const address of device.ips ?? []) {
        const owners = groups.get(address) ?? new Set<string>()
        owners.add(device.group ?? 'Ungrouped')
        groups.set(address, owners)
      }
    }
    function node(name: string, title: string, depth: number, token = '--primary') {
      nodes.set(name, { name, title, depth, token })
      return name
    }
    function link(source: string, target: string, value: number) {
      const key = JSON.stringify([source, target])
      links.set(key, { source, target, value: value + (links.get(key)?.value ?? 0) })
    }
    if (!devices.error && !traffic.error && traffic.data?.counting && devices.data && !devices.data.problems?.length) {
      for (const row of traffic.data.usage) {
        const packets = row.up_packets + row.down_packets
        if (!(packets > 0)) continue
        const owners = groups.get(row.address)
        const group = owners?.size === 1 ? [...owners][0] : 'Unattributed'
        const source = node(`group:${group}`, group, 0)
        const exit = node(`exit:${row.unknown ? 'unknown:' : ''}${row.exit}`, row.unknown ? `Removed exit: ${row.exit || 'Unknown'}` : row.exit || 'Unassigned exit', 1, '--transmit')
        const action = node('forwarded', 'Forwarded', 2, '--success-foreground')
        link(source, exit, packets)
        link(exit, action, packets)
      }
    }
    if (status.enabled && status.known && !status.drifted && !status.problem && !status.native) {
      for (const [name, packets] of [['To router', status.blocked_input], ['To inside devices', status.blocked_forward]] as const) {
        if (!(packets > 0)) continue
        const source = node('outside', 'Outside', 0, '--warning-foreground')
        const destination = node(`blocked:${name}`, name, 1, '--warning-foreground')
        const action = node('blocked', 'Blocked', 2, '--warning-foreground')
        link(source, destination, packets)
        link(destination, action, packets)
      }
    }
    return { nodes: [...nodes.values()], links: [...links.values()] }
  }, [devices.data, devices.error, traffic.data, traffic.error, status])

  useEffect(() => {
    const element = container.current
    if (!element || !graph.links.length) return
    const chart = init(element, undefined, { renderer: 'svg' })
    function draw() {
      const styles = getComputedStyle(element!)
      chart.setOption({
        animation: false,
        series: [{
          type: 'sankey', left: 12, right: 160, top: 24, bottom: 24,
          nodeWidth: 10, nodeGap: 24, draggable: false,
          emphasis: { focus: 'adjacency' },
          label: {
            color: styles.getPropertyValue('--foreground').trim(), fontSize: 13,
            fontFamily: styles.fontFamily, width: 140, overflow: 'break',
            formatter: (params: { data: { title?: string; value?: number } }) => `${params.data.title}\n${(params.data.value ?? 0).toLocaleString()} packets`,
          },
          data: graph.nodes.map(node => ({
            ...node,
            value: graph.links.filter(link => node.depth === 2 ? link.target === node.name : link.source === node.name).reduce((total, link) => total + link.value, 0),
            itemStyle: { color: styles.getPropertyValue(node.token).trim(), borderWidth: 0 },
          })),
          links: graph.links,
          lineStyle: { color: 'source', opacity: 0.2, curveness: 0.5 },
        }],
      })
    }
    draw()
    const resize = new ResizeObserver(() => chart.resize())
    resize.observe(element)
    const theme = new MutationObserver(draw)
    theme.observe(document.documentElement, { attributes: true, attributeFilter: ['class', 'style'] })
    return () => { resize.disconnect(); theme.disconnect(); chart.dispose() }
  }, [graph])

  const unavailable = devices.error || traffic.error || devices.data?.problems?.length
  return (
    <section className="rounded-2xl border bg-card p-4 shadow-xs sm:p-6" aria-label="Measured traffic paths">
      <h2 className="font-semibold">Traffic paths</h2>
      <p className="mt-1 text-sm text-muted-foreground">Group → Exit / destination → Action</p>
      {unavailable ? <p className="mt-4 text-sm text-muted-foreground">Group traffic is unavailable.</p>
        : !traffic.data || !devices.data ? <p className="mt-4 text-sm text-muted-foreground">Loading traffic…</p>
        : !traffic.data.counting ? <p className="mt-4 text-sm text-muted-foreground">Traffic accounting is not active.</p> : null}
      {graph.links.length ? (
        <div className="mt-4 overflow-x-auto focus-visible:outline-2 focus-visible:outline-primary" tabIndex={0} role="region" aria-label="Traffic Sankey diagram">
          <div ref={container} style={{ height: Math.max(320, graph.nodes.filter(node => node.depth === 0).length * 56), minWidth: 640 }} />
          <ul className="sr-only">{graph.links.map(link => <li key={JSON.stringify([link.source, link.target])}>{graph.nodes.find(node => node.name === link.source)?.title} → {graph.nodes.find(node => node.name === link.target)?.title}: {link.value.toLocaleString()} packets</li>)}</ul>
        </div>
      ) : traffic.data?.counting && devices.data && !unavailable ? <p className="mt-6 text-sm text-muted-foreground">No measured traffic.</p> : null}
      <p className="mt-4 text-xs text-muted-foreground">Widths show cumulative packets, including upload and download, not current rates. Forwarded and blocked traffic have separate counter lifetimes. Outside blocks cannot be attributed to a group or exit.</p>
      {traffic.data?.limits?.map(limit => <p key={limit} className="mt-2 text-xs text-muted-foreground">{limit}</p>)}
      {traffic.data && traffic.data.capacity > 0 && traffic.data.held >= traffic.data.capacity && <p className="mt-2 text-xs text-warning-foreground">Traffic accounting is full; some paths may be missing.</p>}
    </section>
  )
}
