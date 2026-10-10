import { ArrowDown, Ban } from 'lucide-react'
import { lazy, Suspense, useMemo } from 'react'
import { Link } from 'react-router'

import wall from '@/assets/firewall-wall.webp'
import type { FirewallStatus } from '@/lib/api-types'
import { groupOpenings, trafficBranches } from './traffic-model'

const Sankey = lazy(() => import('./sankey'))
const TONES = {
  normal: '#8cb5a1',
  configured: '#78a9da',
  attention: '#d5a066',
  inactive: '#a8afb7',
}

export function TrafficMap({ status }: { status: FirewallStatus }) {
  const branches = useMemo(() => trafficBranches(status), [status])
  const colors = useMemo(() => branches.map((branch) => TONES[branch.tone]), [branches])
  const outgoingColors = useMemo(() => [TONES.inactive, status.enabled ? TONES.configured : TONES.inactive], [status.enabled])
  const groups = groupOpenings(status.openings)
  const verified = status.enabled && status.known && !status.drifted && !status.problem

  return (
    <section aria-labelledby="traffic-map-title" className="space-y-5">
      <div className="flex flex-wrap items-baseline justify-between gap-2 px-1">
        <h2 id="traffic-map-title" className="text-sm font-medium">Traffic at the firewall</h2>
        <span className="text-xs text-muted-foreground">
          {!status.enabled ? 'Off - policy not enforced' : verified ? 'Rules in force' : 'Intended policy - not verified'}
        </span>
      </div>

      <div className="mx-auto max-w-4xl">
        <h3 className="mb-5 text-center text-[0.7rem] font-medium uppercase tracking-[0.18em] text-muted-foreground">Inbound</h3>
        <div className="grid grid-cols-3 gap-2 text-center">
          {branches.map((branch) => (
            <div key={branch.id} className="min-w-0 px-1">
              <div className={`text-xs font-medium sm:text-sm ${branch.tone === 'attention' ? 'text-warning-foreground' : ''}`}>{branch.title}</div>
              <div className="mt-1 text-[0.65rem] text-muted-foreground sm:text-xs">{branch.detail}</div>
            </div>
          ))}
        </div>
        <div className="relative mt-3 h-36 sm:h-44">
          <Suspense fallback={<div className="h-full" />}><Sankey colors={colors} /></Suspense>
          {branches.map((branch, i) => (
            <span key={branch.id} className="absolute bottom-0 -translate-x-1/2" style={{ left: `${[40, 50, 60][i]}%`, color: colors[i] }} aria-hidden="true">
              {i === 2 && status.enabled ? <Ban className="size-5 rounded-full bg-background" /> : <ArrowDown className="size-5" />}
            </span>
          ))}
        </div>
        <div className="relative z-10 mx-auto flex h-14 w-[70%] max-w-xl items-center sm:h-20" aria-hidden="true">
          <img src={wall} alt="" className="h-full w-full object-contain opacity-60 dark:invert dark:opacity-40" />
        </div>
        <div className="relative h-36 sm:h-44">
          <Suspense fallback={<div className="h-full" />}><Sankey colors={outgoingColors} outgoing /></Suspense>
          {[27, 73].map((left, i) => <ArrowDown key={left} aria-hidden="true" className="absolute bottom-0 size-5 -translate-x-1/2 translate-y-1/2" style={{ left: `${left}%`, color: outgoingColors[i] }} />)}
        </div>
        <div className="mt-4 grid grid-cols-2 text-center">
          <div><div className="text-xs font-medium sm:text-sm">Inside-originated traffic</div><div className="mt-1 text-[0.65rem] text-muted-foreground sm:text-xs">{status.enabled ? 'Allowed by policy' : 'Not filtered by OLR'}</div></div>
          <div><div className="text-xs font-medium sm:text-sm">Gateway routing</div><div className="mt-1 text-[0.65rem] text-muted-foreground sm:text-xs"><Link to="/gateway" className="underline decoration-border underline-offset-4 hover:decoration-foreground">Review outbound paths</Link></div></div>
        </div>
        <h3 className="mt-5 text-center text-[0.7rem] font-medium uppercase tracking-[0.18em] text-muted-foreground">Outbound</h3>
      </div>

      <p className="px-1 text-xs leading-relaxed text-muted-foreground">
        Paths show policy, not packet proportions. Gateway routing is a separate layer, not an exclusive traffic category. Accepted traffic is not counted here; outbound anomaly detection is not available.
      </p>

      <div className="divide-y rounded-xl border">
        <details className="group px-4 py-3">
          <summary className="cursor-pointer text-sm font-medium focus-visible:outline-2 focus-visible:outline-ring">Review inbound traffic</summary>
          <div className="mt-4 space-y-4 text-xs leading-relaxed text-muted-foreground">
            {branches.map((branch) => <p key={branch.id}><strong className="font-medium text-foreground">{branch.title}. </strong>{branch.explanation}</p>)}
            {groups.map((group) => <div key={group.name}><div className="font-medium text-foreground">{group.name}</div><div className="mt-1 break-words font-mono">{group.matches.join(' / ')}</div></div>)}
            {!groups.length && <p>No configured router service openings.</p>}
            <Link to="/advanced/forwards" className="inline-block text-foreground underline decoration-border underline-offset-4 hover:decoration-foreground">Review port-forward destinations and ports</Link>
          </div>
        </details>
        <div className="px-4 py-3 text-xs leading-relaxed text-muted-foreground">
          <span className="font-medium text-foreground">Inside: </span><span className="break-words font-mono">{status.inside.join(', ') || 'No trusted interfaces'}</span>. All other interfaces count as outside, including ones added later.
        </div>
      </div>
    </section>
  )
}
