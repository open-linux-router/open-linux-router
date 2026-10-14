import { useEffect, useRef } from 'react'
import { init, use as registerCharts } from 'echarts/core'
import { SankeyChart } from 'echarts/charts'
import { TooltipComponent } from 'echarts/components'
import { SVGRenderer } from 'echarts/renderers'
import { Link } from 'react-router'
import type { FirewallStatus } from '@/lib/api-types'

type Category = { title: string; reason: string; blocked?: number }

registerCharts([SankeyChart, TooltipComponent, SVGRenderer])

function SemanticSankey({ categories }: { categories: Category[] }) {
  const container = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const element = container.current
    if (!element) return
    const chart = init(element, undefined, { renderer: 'svg' })
    function draw() {
      const styles = getComputedStyle(element!)
      const color = (token: string) => styles.getPropertyValue(token).trim()
      const nodes = categories.map((category, index) => ({
        name: `category:${index}`, title: category.title, reason: category.reason, depth: 0,
        itemStyle: { color: color(category.blocked === undefined ? '--primary' : '--warning-foreground') },
      }))
      chart.setOption({
        animation: false,
        tooltip: { trigger: 'item', renderMode: 'richText', formatter: (params: { data: { title?: string; reason?: string } }) => params.data.reason ?? params.data.title ?? '' },
        series: [{
          type: 'sankey', left: 8, right: 240, top: 20, bottom: 20,
          nodeWidth: 12, nodeGap: 32, nodeAlign: 'left', layoutIterations: 0, draggable: false,
          emphasis: { focus: 'adjacency' },
          label: { color: color('--foreground'), fontFamily: styles.fontFamily, fontSize: 14, width: 210, overflow: 'break', formatter: (params: { data: { title: string } }) => params.data.title },
          data: [
            ...nodes,
            { name: 'firewall', title: 'Firewall', depth: 1, itemStyle: { color: color('--transmit') } },
            { name: 'allowed', title: 'Allowed', depth: 2, itemStyle: { color: color('--success-foreground') } },
            ...categories.flatMap((category, index) => category.blocked === undefined ? [] : [{
              name: `blocked:${index}`, title: `Blocked: ${category.title.toLowerCase()}\n${category.blocked.toLocaleString()} packets`, depth: 2,
              itemStyle: { color: color('--warning-foreground') },
            }]),
          ],
          links: [
            ...categories.map((_, index) => ({ source: `category:${index}`, target: 'firewall', value: 1 })),
            { source: 'firewall', target: 'allowed', value: categories.filter(category => category.blocked === undefined).length, lineStyle: { color: color('--success-foreground') } },
            ...categories.flatMap((category, index) => category.blocked === undefined ? [] : [{ source: 'firewall', target: `blocked:${index}`, value: 1, lineStyle: { color: color('--warning-foreground') } }]),
          ],
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
  }, [categories])
  return (
    <div className="mt-4 overflow-x-auto focus-visible:outline-2 focus-visible:outline-primary" tabIndex={0} role="region" aria-label="Traffic categories through the firewall to allowed or blocked decisions">
      <div ref={container} style={{ minWidth: 960, height: Math.max(480, categories.length * 64) }} />
    </div>
  )
}

export function TrafficPermissions({ status }: { status: FirewallStatus }) {
  const verified = status.known && !status.drifted && !status.problem
  const categories: Category[] = [
    { title: 'Replies and related traffic', reason: 'Established connections and related traffic to the router or through it.' },
    { title: 'Inside-initiated access', reason: 'Connections from trusted inside interfaces, including enabled remote-access WireGuard.' },
    { title: 'Router-local traffic', reason: 'Loopback traffic to services on this router.' },
    { title: 'ICMP', reason: 'IPv4 errors and ping to the router.' },
    { title: 'ICMPv6', reason: 'Required IPv6 control messages to the router; permitted errors and ping to inside devices.' },
    { title: 'DHCP', reason: 'IPv4 lease replies for the router’s DHCP client.' },
    { title: 'DHCPv6', reason: 'IPv6 lease replies for the router’s DHCPv6 client.' },
    ...(status.openings ?? []).map(opening => ({
      title: `${opening.for} · ${opening.protocol === '6in4' ? 'IP protocol 41' : `${opening.protocol.toUpperCase()} ${opening.port}`}`,
      reason: opening.protocol === '6in4'
        ? `IP protocol 41 to the router, only from tunnel broker ${opening.from}.`
        : `${opening.protocol.toUpperCase()} ${opening.port} to the router, opened by this service.`,
    })),
    { title: 'Port-forwarded connections', reason: 'Connections translated by a configured port forward (DNAT).' },
    { title: 'Other forwarded traffic', reason: 'Traffic whose destination interface is outside the trusted inside set; this firewall does not restrict it.' },
    ...(status.iptv?.upstream ? [
      { title: 'IPTV · IGMP', reason: `Membership messages on ${[status.iptv.upstream, ...(status.iptv.downstream ?? [])].join(', ')}.` },
      ...(status.iptv.downstream ?? []).map(downstream => ({ title: 'IPTV · IPv4 multicast', reason: `Streams from ${status.iptv!.upstream} to ${downstream}.` })),
    ] : []),
    { title: 'Unsolicited access to the router', reason: 'Outside traffic that matches none of the permissions above.', blocked: status.blocked_input },
    { title: 'Unsolicited access to inside devices', reason: 'Outside traffic headed into trusted networks that matches none of the permissions above.', blocked: status.blocked_forward },
  ]

  return (
    <section className="rounded-2xl border bg-card p-4 shadow-xs sm:p-6" aria-label="Traffic permissions">
      <h2 className="font-semibold">Traffic permissions</h2>
      {!status.enabled ? (
        <p className="mt-4 text-sm text-muted-foreground">The firewall is off. OLR is not enforcing traffic permissions.</p>
      ) : status.native ? (
        <p className="mt-4 text-sm text-muted-foreground">Native rules define the traffic permissions. <Link className="text-primary underline" to="/firewall/rules">Inspect rules</Link></p>
      ) : !verified ? (
        <p className="mt-4 text-sm text-muted-foreground">Traffic permissions could not be verified. <Link className="text-primary underline" to="/firewall/rules">Inspect current rules</Link></p>
      ) : (
        <>
          <p className="mt-1 text-sm text-muted-foreground">Why traffic is allowed or blocked by OLR.</p>
          <SemanticSankey categories={categories} />
          <p className="mt-4 text-sm text-muted-foreground">Equal-width paths represent traffic categories, not measured traffic volumes.</p>
          <details className="mt-4 text-sm">
            <summary className="cursor-pointer focus-visible:outline-2 focus-visible:outline-primary">Matching conditions</summary>
          <ul className="mt-4 divide-y">
            {categories.map((category, index) => (
              <li key={index} className="flex min-h-16 flex-wrap items-center justify-between gap-x-6 gap-y-2 py-4">
                <div className="min-w-0 flex-1 basis-64">
                  <h3 className="text-sm font-medium">{category.title}</h3>
                  <p className="mt-1 break-words text-sm text-muted-foreground">{category.reason}</p>
                </div>
                <div className="text-sm">
                  <span className={category.blocked === undefined ? 'text-success-foreground' : 'text-warning-foreground'}>{category.blocked === undefined ? 'Allowed' : 'Blocked'}</span>
                  {category.blocked !== undefined && <p className="mt-1 tabular-nums text-muted-foreground">{category.blocked.toLocaleString()} packets</p>}
                </div>
              </li>
            ))}
          </ul>
          </details>
          <p className="mt-4 text-sm text-muted-foreground">Blocked counts are cumulative since the rules were last written. Allowed categories have no individual traffic counters. Other firewalls can further restrict allowed traffic.</p>
        </>
      )}
    </section>
  )
}
