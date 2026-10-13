import { useEffect, useRef } from 'react'
import { ShieldCheck } from 'lucide-react'
import { init, use as registerCharts } from 'echarts/core'
import { SankeyChart } from 'echarts/charts'
import { SVGRenderer } from 'echarts/renderers'
import type { FirewallStatus } from '@/lib/api-types'

registerCharts([SankeyChart, SVGRenderer])

type Arrival = { label: string; detail?: string; result: string; token: string }

function ArrivalChart({ arrivals, upward = false }: { arrivals: Arrival[]; upward?: boolean }) {
  const container = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const element = container.current!
    const chart = init(element, undefined, { renderer: 'svg' })
    function draw() {
      const styles = getComputedStyle(element)
      chart.setOption({
        animation: false,
        series: [{
          type: 'sankey', orient: 'vertical', left: 0, right: 0, top: 0, bottom: 0,
          nodeWidth: 5, nodeGap: 24, layoutIterations: 0, draggable: false, silent: true,
          label: { show: false },
          data: [
            ...arrivals.map((arrival, index) => ({
              name: String(index), depth: 0,
              itemStyle: { color: styles.getPropertyValue(arrival.token).trim(), borderWidth: 0 },
            })),
            { name: 'Firewall', depth: 1, itemStyle: { color: 'transparent', borderWidth: 0 } },
          ],
          links: arrivals.map((_, index) => ({ source: String(index), target: 'Firewall', value: 1 })),
          lineStyle: { color: 'source', opacity: 0.28, curveness: 0.5 },
        }],
      })
    }
    draw()
    const resize = new ResizeObserver(() => chart.resize())
    resize.observe(element)
    const theme = new MutationObserver(draw)
    theme.observe(document.documentElement, { attributes: true, attributeFilter: ['class', 'style'] })
    return () => { resize.disconnect(); theme.disconnect(); chart.dispose() }
  }, [arrivals])

  const labels = (
    <div className="flex gap-2 py-4 text-center">
      {arrivals.map(arrival => (
        <div key={`${arrival.label}:${arrival.detail ?? ''}`} className="min-w-0 flex-1 text-xs sm:text-sm">
          <div className="break-words font-medium">{arrival.label}</div>
          {arrival.detail && <div className="mt-1 break-words text-xs text-muted-foreground">{arrival.detail}</div>}
          <div className="mt-1 text-xs text-muted-foreground">{arrival.result}</div>
        </div>
      ))}
    </div>
  )
  return (
    <div className="overflow-x-auto focus-visible:outline-2 focus-visible:outline-primary" tabIndex={0} role="region" aria-label={upward ? 'Inside rule categories' : 'Outside rule categories'}>
      <div style={{ minWidth: arrivals.length * 88 }}>
        {!upward && labels}
        <div ref={container} aria-hidden="true" className={`h-44 w-full sm:h-56 ${upward ? '-scale-y-100' : ''}`} />
        {upward && labels}
      </div>
    </div>
  )
}

export function FirewallSankey({ status }: { status: FirewallStatus }) {
  const verified = status.known && !status.drifted && !status.problem
  const uncertain = !verified || status.native
  function arrival(label: string, blocked = false): Arrival {
    if (uncertain) return { label, result: 'Review rules', token: '--muted-foreground' }
    if (!status.enabled) return { label, result: 'Firewall off', token: '--muted-foreground' }
    return { label, result: blocked ? 'Blocked' : 'Allowed', token: blocked ? '--warning-foreground' : '--success-foreground' }
  }
  const external = [
    arrival('Unsolicited input', true),
    arrival('Unsolicited forward', true),
    arrival('Established / related'),
    arrival('ICMP'),
    arrival('ICMPv6'),
    arrival('DHCP client'),
    arrival('DHCPv6 client'),
    ...status.openings.map(opening => ({
      ...arrival(opening.for),
      detail: opening.protocol === '6in4'
        ? `6in4 from ${opening.from}`
        : `${opening.protocol.toUpperCase()} ${opening.port}${opening.from ? ` from ${opening.from}` : ''}`,
    })),
    arrival('Port forwards (DNAT)'),
    arrival('Forward to non-inside'),
    ...(status.iptv?.upstream ? [arrival('IPTV IGMP'), arrival('IPTV multicast')] : []),
  ]
  const internal = status.inside.length
    ? status.inside.map(name => arrival(name))
    : [arrival('Inside networks')]

  return (
    <section className="rounded-2xl border bg-card p-4 shadow-xs sm:p-6" aria-label="Connections arriving at the firewall">
      <div className="mb-2 text-sm font-medium">Outside ↓</div>
      <ArrivalChart arrivals={status.native ? [arrival('Native rules')] : external} />
      <div className="relative flex h-16 items-center justify-center rounded-xl border bg-muted">
        <div className="flex items-center gap-3 rounded-xl border bg-card px-5 py-3">
          <ShieldCheck className="size-9 text-primary" aria-hidden="true" />
          <div><h2 className="font-semibold">Firewall</h2><p className="text-xs text-muted-foreground">{uncertain ? 'Rule decisions not summarized' : status.enabled ? 'Allow or block' : 'Off'}</p></div>
        </div>
      </div>
      <ArrivalChart arrivals={internal} upward />
      <div className="mt-2 text-sm font-medium">Inside ↑</div>
      <p className="mt-6 text-xs text-muted-foreground">Paths arrive at the firewall. Widths show categories, not traffic volume. Only connections through this router are shown.</p>
      {status.enabled && verified && !status.native && <p className="mt-2 text-xs text-muted-foreground">{status.blocked_input.toLocaleString()} packets blocked to the router · {status.blocked_forward.toLocaleString()} to inside devices, since rules were applied.</p>}
    </section>
  )
}
