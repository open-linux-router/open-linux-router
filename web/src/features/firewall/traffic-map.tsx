import { ArrowDown, Ban } from 'lucide-react'
import { lazy, Suspense, useMemo, useState } from 'react'
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
  const allBranches = useMemo(() => trafficBranches(status), [status])
  const branches = useMemo(() => allBranches.filter((branch) => branch.group === 'incoming'), [allBranches])
  const outgoing = useMemo(() => allBranches.filter((branch) => branch.group !== 'incoming'), [allBranches])
  const [selected, setSelected] = useState('replies')
  const active = allBranches.find((branch) => branch.id === selected) ?? allBranches[0]
  const colors = useMemo(() => branches.map((branch) => TONES[branch.tone]), [branches])
  const outgoingColors = useMemo(() => outgoing.map((branch) => TONES[branch.tone]), [outgoing])
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

      <div className="mx-auto hidden max-w-5xl md:block">
        <h3 className="mb-5 text-center text-[0.7rem] font-medium uppercase tracking-[0.18em] text-muted-foreground">Inbound</h3>
        <div className="grid grid-cols-4 gap-2 text-center">
          {branches.map((branch) => (
            <button type="button" onClick={() => setSelected(branch.id)} aria-pressed={selected === branch.id} key={branch.id} className="min-h-20 min-w-0 cursor-pointer rounded-lg px-1 py-2 hover:bg-muted aria-pressed:bg-muted focus-visible:outline-2 focus-visible:outline-ring">
              <div className={`text-xs font-medium sm:text-sm ${branch.tone === 'attention' ? 'text-warning-foreground' : ''}`}>{branch.title}</div>
              <div className="mt-1 text-[0.65rem] text-muted-foreground sm:text-xs">{branch.detail}</div>
            </button>
          ))}
        </div>
        <div className="relative mt-3 h-36 sm:h-44">
          <Suspense fallback={<div className="h-full" />}><Sankey colors={colors} /></Suspense>
          {branches.map((branch, i) => (
            <span key={branch.id} className="absolute bottom-0 -translate-x-1/2" style={{ left: `${100 * (0.35 + 0.3 * (i + 0.5) / branches.length)}%`, color: colors[i] }} aria-hidden="true">
              {i === 2 && status.enabled ? <Ban className="size-5 rounded-full bg-background" /> : <ArrowDown className="size-5" />}
            </span>
          ))}
        </div>
        <div className="relative z-10 mx-auto flex h-14 w-[70%] max-w-xl items-center sm:h-20" aria-hidden="true">
          <img src={wall} alt="" className="h-full w-full object-contain opacity-60 dark:invert dark:opacity-40" />
        </div>
        <div className="relative h-36 sm:h-44">
          <Suspense fallback={<div className="h-full" />}><Sankey colors={outgoingColors} outgoing /></Suspense>
          {outgoing.map((branch, i) => <ArrowDown key={branch.id} aria-hidden="true" className="absolute bottom-0 size-5 -translate-x-1/2 translate-y-1/2" style={{ left: `${100 * (i + 0.5) / outgoing.length}%`, color: outgoingColors[i] }} />)}
        </div>
        <div className="mt-4 grid grid-cols-4 gap-2 text-center">
          {outgoing.map((branch) => <button type="button" key={branch.id} onClick={() => setSelected(branch.id)} aria-pressed={selected === branch.id} className="min-h-20 min-w-0 cursor-pointer rounded-lg px-1 py-2 hover:bg-muted aria-pressed:bg-muted focus-visible:outline-2 focus-visible:outline-ring"><span className="block text-xs font-medium sm:text-sm">{branch.title}</span><span className="mt-1 block text-[0.65rem] text-muted-foreground sm:text-xs">{branch.detail}</span></button>)}
        </div>
        <div className="mt-5 grid grid-cols-2 text-center text-xs font-medium uppercase tracking-widest text-muted-foreground"><h3>Outgoing</h3><h3>Local</h3></div>
      </div>

      <div className="space-y-4 md:hidden">
        {(['incoming', 'outgoing', 'local'] as const).map((group) => <div key={group}>
          <h3 className="mb-2 text-sm font-medium capitalize">{group}</h3>
          <div className="space-y-2">{allBranches.filter((branch) => branch.group === group).map((branch) =>
            <button type="button" key={branch.id} aria-pressed={selected === branch.id} onClick={() => setSelected(branch.id)} className="flex min-h-14 w-full cursor-pointer items-center gap-3 rounded-lg border px-3 py-2 text-left hover:bg-muted aria-pressed:bg-muted focus-visible:outline-2 focus-visible:outline-ring">
              {branch.id === 'blocked' && status.enabled ? <Ban aria-hidden="true" className="size-4 shrink-0" /> : <ArrowDown aria-hidden="true" className="size-4 shrink-0" />}
              <span><span className="block text-sm font-medium">{branch.title}</span><span className="block text-xs text-muted-foreground">{branch.detail}</span></span>
            </button>)}</div>
        </div>)}
      </div>

      <div className="rounded-xl border p-4" aria-live="polite"><h3 className="text-sm font-medium">{active.title}</h3><p className="mt-2 text-sm leading-relaxed text-muted-foreground">{active.explanation}</p></div>

      <p className="px-1 text-xs leading-relaxed text-muted-foreground">
        Paths show policy, not packet proportions. Accepted traffic is not counted here. Replies and network essentials can travel in either direction; categories describe their role rather than exclusive packet counters. <Link to="/gateway" className="underline underline-offset-4">Review gateway routing</Link>.
      </p>

      <div className="divide-y rounded-xl border">
        <details className="group px-4 py-3">
          <summary className="cursor-pointer text-sm font-medium focus-visible:outline-2 focus-visible:outline-ring">Review all traffic categories</summary>
          <div className="mt-4 space-y-4 text-xs leading-relaxed text-muted-foreground">
            {allBranches.map((branch) => <p key={branch.id}><strong className="font-medium text-foreground">{branch.title}. </strong>{branch.explanation}</p>)}
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
