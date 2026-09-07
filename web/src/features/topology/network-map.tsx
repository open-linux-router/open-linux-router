import { Router } from 'lucide-react'
import { useMemo } from 'react'

import { Skeleton } from '@/components/ui/skeleton'
import { DeviceIcon } from '@/features/devices/device-icon'
import type { AssignmentStatus, DeviceRow, ExitStatus } from '@/lib/api-types'
import type { Pool } from '@/lib/config-types'
import { cn } from '@/lib/utils'

/**
 * The network, drawn top-down the way an architecture diagram is.
 *
 * The rule the shape follows: a *node* is a thing, a *container* is a grouping.
 * The router and each device get a node because you could put a hand on them.
 * A network does not — `lan0` is a segment, not a box in the hall — so it is a
 * tinted rectangle its devices sit inside. Drawn as a node it made readers ask
 * which of the circles was a real machine, and that question costs more than a
 * uniform tree is worth.
 *
 * A way out is not a node either, and more strongly: "No internet" is a rule,
 * not a hop. It rides in the group's header as a chip, so a network carries
 * where it goes out as a property rather than an edge — which also keeps the
 * picture a tree, since two networks sharing one exit would otherwise need two
 * edges converging on one box.
 *
 * What it does not claim: this is *logical* structure. With no link module,
 * "inside lan0" means "on that network", not "plugged into that port" — half of
 * them may be on wifi behind one access point and nothing here can tell.
 */
export function NetworkMap({
  devices: all,
  assignments,
  exits,
  pools,
  pending,
  filter = '',
  onSelect,
}: {
  devices: DeviceRow[]
  assignments?: AssignmentStatus[]
  exits?: ExitStatus[]
  pools?: Pool[]
  pending: boolean
  filter?: string
  onSelect?: (device: DeviceRow) => void
}) {
  // Filtering hides devices, never groups: a network whose every device was
  // filtered out still shows, because "nothing of yours is on iot0" is an answer
  // and an absent iot0 is not.
  const devices = useMemo(() => {
    const q = filter.trim().toLowerCase()
    if (!q) return all
    return all.filter((d) =>
      [d.name, d.mac, d.hostname ?? '', d.vendor ?? '', d.network ?? '', ...(d.ips ?? [])]
        .join(' ')
        .toLowerCase()
        .includes(q),
    )
  }, [all, filter])

  const groups = useMemo(
    () => buildGroups(devices, assignments, exits, pools),
    [devices, assignments, exits, pools],
  )

  if (pending) return <Skeleton className="h-64 w-full rounded-xl" />
  if (groups.length === 0) {
    return (
      <div className="rounded-xl border border-dashed px-4 py-10 text-center text-sm text-muted-foreground">
        No networks yet.
      </div>
    )
  }

  return (
    <div className="flex flex-col items-center">
      <div className="inline-flex items-center gap-2 rounded-xl border bg-card px-4 py-2.5 shadow-sm">
        <Router className="size-4 text-muted-foreground" aria-hidden />
        <span className="text-sm font-medium">This router</span>
      </div>

      {/* The trunk down out of the router. */}
      <div className="h-5 w-px bg-border" aria-hidden />

      {/*
        One bounding box rather than a rail with a stub per group.
        A rail has to be re-drawn for every wrapped row, and the second row's
        stubs then rise into empty space — a line to nowhere on any network with
        more than two of these. Nesting the boxes says the same thing (these are
        the router's networks), survives wrapping, and is what an architecture
        diagram does anyway.
      */}
      <ul className="grid w-full gap-3 rounded-xl border border-dashed p-3 sm:grid-cols-2">
        {groups.map((group) => (
          <li key={group.key} className="min-w-0">
            <NetworkGroup group={group} onSelect={onSelect} />
          </li>
        ))}
      </ul>
    </div>
  )
}

/* -------------------------------------------------------------------------- */

interface Group {
  key: string
  name: string
  devices: DeviceRow[]
  pool?: Pool
  /** The way out, as a property of the network rather than a node of its own. */
  exit?: string
  inherited: boolean
  status?: ExitStatus
  down: boolean
  kind: 'network' | 'unplaced' | 'unseen'
}

function buildGroups(
  devices: DeviceRow[],
  assignments?: AssignmentStatus[],
  exits?: ExitStatus[],
  pools?: Pool[],
): Group[] {
  // Networks are the union of what routing knows and what dhcp serves: one with
  // a pool but no assignment has no way out chosen yet, one with an assignment
  // but no pool is served statically, and the intersection would drop both.
  const names = new Set<string>()
  for (const a of assignments ?? []) names.add(a.interface)
  for (const p of pools ?? []) names.add(p.interface)
  for (const d of devices) if (d.network) names.add(d.network)

  const groups: Group[] = [...names].map((name) => {
    const assignment = assignments?.find((a) => a.interface === name)
    const exit = assignment?.exit || ''
    const status = exit ? exits?.find((e) => e.name === exit) : undefined
    return {
      key: name,
      name,
      devices: devices
        .filter((d) => d.network === name)
        .sort((a, b) => Number(b.online) - Number(a.online) || a.name.localeCompare(b.name)),
      pool: pools?.find((p) => p.interface === name),
      exit,
      inherited: assignment?.source === 'default',
      status,
      down: Boolean(status && status.probed && !status.up),
      kind: 'network' as const,
    }
  })

  // Busiest first, so the network the household lives on leads.
  groups.sort((a, b) => b.devices.length - a.devices.length || a.name.localeCompare(b.name))

  const unplaced = devices.filter((d) => !d.network && d.seen)
  if (unplaced.length) {
    groups.push({
      key: ' unplaced',
      name: 'Not on a known network',
      devices: unplaced,
      inherited: false,
      down: false,
      kind: 'unplaced',
    })
  }

  // Stored but never observed. Without a group of its own, a device somebody
  // typed in by hand would appear nowhere at all.
  const unseen = devices.filter((d) => !d.seen)
  if (unseen.length) {
    groups.push({
      key: ' unseen',
      name: 'Never seen',
      devices: unseen,
      inherited: false,
      down: false,
      kind: 'unseen',
    })
  }

  return groups
}

/* -------------------------------------------------------------------------- */

function NetworkGroup({
  group,
  onSelect,
}: {
  group: Group
  onSelect?: (device: DeviceRow) => void
}) {
  const here = group.devices.filter((d) => d.online).length
  const real = group.kind === 'network'

  return (
    <section
      className={cn(
        'h-full rounded-xl border p-3',
        real ? 'bg-muted/40' : 'border-dashed',
        group.down && 'border-destructive/50 bg-destructive/5',
      )}
    >
      <header className="mb-2.5 flex flex-wrap items-baseline gap-x-2 gap-y-1 px-0.5">
        <span className={cn('text-sm font-medium', !real && 'text-muted-foreground')}>
          {group.name}
        </span>
        <span className="text-xs text-muted-foreground">
          {group.devices.length === 0 ? 'empty' : `${here}/${group.devices.length} here`}
        </span>
        {real && <ExitChip group={group} />}
      </header>

      {group.devices.length > 0 && (
        <ul className="grid gap-1.5 sm:grid-cols-2">
          {group.devices.map((device) => (
            <li key={device.mac}>
              <DeviceTile device={device} onSelect={onSelect} />
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

/**
 * Where this network goes out.
 *
 * A chip and not a node: "No internet" is a rule, and an exit two networks share
 * would need two edges into one box. Inheritance is a dashed outline rather than
 * the words "from the box-wide setting" — same fact, no sentence.
 */
function ExitChip({ group }: { group: Group }) {
  const label = group.exit || 'direct'
  const unchecked = Boolean(group.exit) && !group.status?.probed

  return (
    <span
      title={group.inherited ? 'Follows the box-wide setting' : `Set on ${group.name}`}
      className={cn(
        'ml-auto inline-flex shrink-0 items-center gap-1.5 rounded-md border px-1.5 py-0.5 text-xs',
        group.inherited && 'border-dashed',
        group.down ? 'border-destructive/50 text-destructive' : 'bg-background text-muted-foreground',
      )}
    >
      <span
        aria-hidden
        className={cn(
          'size-1.5 rounded-full',
          group.down ? 'bg-destructive' : unchecked ? 'bg-muted-foreground/40' : 'bg-success',
        )}
      />
      {label}
    </span>
  )
}

function DeviceTile({
  device,
  onSelect,
}: {
  device: DeviceRow
  onSelect?: (device: DeviceRow) => void
}) {
  const address = device.fixed_ip ?? device.ips?.[0]

  const body = (
    <>
      <DeviceIcon category={device.category} online={device.online} size="sm" />
      <span className="min-w-0 flex-1">
        <span className="block truncate text-xs font-medium">{device.name || device.mac}</span>
        {address && (
          <span className="block truncate font-mono text-[0.65rem] text-muted-foreground">
            {address}
          </span>
        )}
      </span>
      {/* Presence as a dot, paired with the dimming — never colour alone. */}
      <span
        aria-label={device.online ? 'here' : 'away'}
        className={cn(
          'size-1.5 shrink-0 rounded-full',
          device.online ? 'bg-success' : 'bg-muted-foreground/30',
        )}
      />
    </>
  )

  const shape = cn(
    'flex min-h-11 w-full items-center gap-2 rounded-lg border bg-background px-2 py-1.5 text-left',
    !device.online && 'opacity-60',
    device.fixed_ip && 'border-foreground/25',
  )

  if (!onSelect) return <div className={shape}>{body}</div>
  return (
    <button
      type="button"
      onClick={() => onSelect(device)}
      className={cn(
        shape,
        'transition-colors hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring',
      )}
    >
      {body}
    </button>
  )
}
