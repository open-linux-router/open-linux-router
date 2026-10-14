import { useEffect, useMemo, useRef } from 'react'
import { init, use as registerCharts } from 'echarts/core'
import { SankeyChart } from 'echarts/charts'
import { TooltipComponent } from 'echarts/components'
import { SVGRenderer } from 'echarts/renderers'
import { Link } from 'react-router'
import type { FirewallStatus } from '@/lib/api-types'
import { sourceLabels, trafficFlows, type TrafficFlow } from './traffic-model'

registerCharts([SankeyChart, TooltipComponent, SVGRenderer])

function totals(flow: Pick<TrafficFlow, 'packets' | 'bytes'>) {
  return `${flow.packets.toLocaleString()} packets · ${flow.bytes.toLocaleString()} bytes`
}

function SourceSankey({ flows, title }: { flows: TrafficFlow[]; title: string }) {
  const container = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const element = container.current
    if (!element) return
    const chart = init(element, undefined, { renderer: 'svg' })
    function draw() {
      const styles = getComputedStyle(element!)
      const color = (token: string) => styles.getPropertyValue(token).trim()
      const sources = [...new Set(flows.map(flow => flow.source))]
      const verdicts = [...new Set(flows.map(flow => flow.verdict))]
      const verdictColor = (verdict: string) => color(verdict === 'allowed' ? '--success-foreground' : '--warning-foreground')
      const detail = (flow: TrafficFlow) => `${sourceLabels[flow.source]} → ${flow.category}\n${flow.verdict === 'allowed' ? 'Allowed' : 'Blocked'} · ${totals(flow)}\n${flow.descriptions.join('\n')}`
      chart.setOption({
        animation: false,
        tooltip: {
          trigger: 'item', renderMode: 'richText',
          formatter: (params: { data: { detail?: string; title?: string } }) => params.data.detail ?? params.data.title ?? '',
        },
        series: [{
          type: 'sankey', left: 12, right: 140, top: 24, bottom: 24,
          nodeWidth: 12, nodeGap: 40, nodeAlign: 'left', layoutIterations: 0, draggable: false,
          emphasis: { focus: 'adjacency' },
          label: {
            color: color('--foreground'), fontFamily: styles.fontFamily, fontSize: 14,
            width: 220, overflow: 'break',
            formatter: (params: { data: { title: string } }) => params.data.title,
          },
          data: [
            ...sources.map(source => ({
              name: `source:${source}`, title: sourceLabels[source], depth: 0,
              itemStyle: { color: color(source === 'outside' ? '--receive' : '--transmit') },
            })),
            ...flows.map(flow => ({
              name: flow.id, title: `${flow.category}\n${flow.packets.toLocaleString()} packets`, detail: detail(flow), depth: 1,
              itemStyle: { color: verdictColor(flow.verdict) },
            })),
            ...verdicts.map(verdict => ({
              name: `verdict:${verdict}`, title: `${verdict === 'allowed' ? 'Allowed' : 'Blocked'}\n${flows.filter(flow => flow.verdict === verdict).reduce((sum, flow) => sum + flow.packets, 0).toLocaleString()} packets`, depth: 2,
              itemStyle: { color: verdictColor(verdict) }, label: { width: 128 },
            })),
          ],
          links: flows.flatMap(flow => [
            { source: `source:${flow.source}`, target: flow.id, value: flow.packets, detail: detail(flow) },
            { source: flow.id, target: `verdict:${flow.verdict}`, value: flow.packets, detail: detail(flow), lineStyle: { color: verdictColor(flow.verdict) } },
          ]),
          lineStyle: { color: 'source', opacity: 0.22, curveness: 0.5 },
        }],
      })
    }
    draw()
    const resize = new ResizeObserver(() => chart.resize())
    resize.observe(element)
    const theme = new MutationObserver(draw)
    theme.observe(document.documentElement, { attributes: true, attributeFilter: ['class', 'style'] })
    return () => { resize.disconnect(); theme.disconnect(); chart.dispose() }
  }, [flows])

  if (!flows.length) return null
  return (
    <div>
      <h3 className="text-sm font-medium">{title}</h3>
      <div className="mt-2 overflow-x-auto focus-visible:outline-2 focus-visible:outline-primary" tabIndex={0} role="region" aria-label={`${title}: source, traffic category and decision`}>
        <div ref={container} aria-hidden="true" style={{ minWidth: 900, height: Math.max(280, flows.length * 80 + 48) }} />
      </div>
      <details className="mt-2 text-sm">
        <summary className="cursor-pointer focus-visible:outline-2 focus-visible:outline-primary">Matching conditions and counts</summary>
        <ul className="mt-4 divide-y">
          {flows.map(flow => (
            <li key={flow.id} className="flex min-h-16 flex-wrap items-center justify-between gap-x-6 gap-y-2 py-4">
              <div className="min-w-0 flex-1 basis-64">
                <h4 className="text-sm font-medium">{flow.category}</h4>
                <p className="mt-1 text-muted-foreground">{sourceLabels[flow.source]} · {flow.destinations.map(destination => destination === 'router' ? 'To router' : 'Forwarded').join(' / ')}</p>
                <p className="mt-1 break-words text-muted-foreground">{flow.descriptions.join(' ')}</p>
              </div>
              <div className="text-sm">
                <span className={flow.verdict === 'allowed' ? 'text-success-foreground' : 'text-warning-foreground'}>{flow.verdict === 'allowed' ? 'Allowed' : 'Blocked'}</span>
                <p className="mt-1 tabular-nums text-muted-foreground">{totals(flow)}</p>
              </div>
            </li>
          ))}
        </ul>
      </details>
    </div>
  )
}

export function TrafficPermissions({ status }: { status: FirewallStatus }) {
  const verified = status.known && !status.drifted && !status.problem
  const flows = useMemo(() => trafficFlows(status.traffic ?? []), [status.traffic])
  const outside = useMemo(() => flows.filter(flow => flow.source === 'outside'), [flows])
  const inside = useMemo(() => flows.filter(flow => flow.source !== 'outside'), [flows])

  return (
    <section className="rounded-2xl border bg-card p-4 shadow-xs sm:p-6" aria-label="Traffic permissions">
      <h2 className="font-semibold">Traffic permissions</h2>
      {!status.enabled ? (
        <p className="mt-4 text-sm text-muted-foreground">The firewall is off. OLR is not enforcing traffic permissions.</p>
      ) : status.native ? (
        <p className="mt-4 text-sm text-muted-foreground">Native rules define the traffic permissions. <Link className="text-primary underline" to="/firewall/rules">Inspect rules</Link></p>
      ) : !verified ? (
        <p className="mt-4 text-sm text-muted-foreground">Traffic permissions could not be verified. <Link className="text-primary underline" to="/firewall/rules">Inspect current rules</Link></p>
      ) : !status.traffic_known ? (
        <p className="mt-4 text-sm text-muted-foreground">Traffic counters are unavailable.</p>
      ) : flows.length === 0 ? (
        <p className="mt-4 text-sm text-muted-foreground">No traffic recorded since the rules were last written.</p>
      ) : (
        <>
          <div className="mt-4 space-y-8">
            <SourceSankey flows={outside} title="From external networks" />
            <SourceSankey flows={inside} title="From LAN / trusted networks and this router" />
          </div>
          <p className="mt-4 text-sm text-muted-foreground">Packet counts since the last rule update, attributed to the first matching rule. Each diagram scales independently. Trusted networks include remote-access WireGuard; other interfaces are external. Other firewalls can further restrict allowed traffic.</p>
        </>
      )}
    </section>
  )
}
