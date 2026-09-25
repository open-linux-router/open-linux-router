import { Router } from 'lucide-react'
import { useMemo } from 'react'

import { Skeleton } from '@/components/ui/skeleton'
import { DeviceIcon } from '@/features/devices/device-icon'
import { useInterfaces } from '@/features/link/queries'
import type { AssignmentStatus, DeviceRow, ExitStatus, NetworkRow } from '@/lib/api-types'
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
 * not a hop. It rides in the container's header as a chip, so a network carries
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
  // Filtering hides devices, never networks: a network whose every device was
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

  // The networks, for joining a pool (keyed by network) to a gateway assignment
  // (keyed by interface). Read here rather than passed in because every caller
  // of this component would otherwise have to fetch it only to hand it back.
  const interfaces = useInterfaces()
  const networks = interfaces.data?.networks

  const containers = useMemo(
    () => buildContainers(devices, assignments, exits, pools, networks),
    [devices, assignments, exits, pools, networks],
  )

  if (pending) return <Skeleton className="h-64 w-full rounded-xl" />
  if (containers.length === 0) {
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
        One bounding box rather than a rail with a stub per network.
        A rail has to be re-drawn for every wrapped row, and the second row's
        stubs then rise into empty space — a line to nowhere on any network with
        more than two of these. Nesting the boxes says the same thing (these are
        the router's networks), survives wrapping, and is what an architecture
        diagram does anyway.
      */}
      <ul className="grid w-full gap-3 rounded-xl border border-dashed p-3 sm:grid-cols-2">
        {containers.map((container) => (
          <li key={container.key} className="min-w-0">
            <NetworkContainer container={container} onSelect={onSelect} />
          </li>
        ))}
      </ul>
    </div>
  )
}

/* -------------------------------------------------------------------------- */

interface Container {
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

function buildContainers(
  devices: DeviceRow[],
  assignments?: AssignmentStatus[],
  exits?: ExitStatus[],
  pools?: Pool[],
  networks?: NetworkRow[],
): Container[] {
  // Networks are the union of what gateway knows and what dhcp serves: one with
  // a pool but no assignment has no way out chosen yet, one with an assignment
  // but no pool is served statically, and the intersection would drop both.
  //
  // Everything is keyed by *network name*. Pools and devices already are;
  // gateway still names a kernel interface (internal/gateway/config.go: "the
  // source network, named by its kernel interface"), so its assignments are
  // translated through the network's members on the way in. Until gateway keys
  // off networks too — design.md §4.4's remaining retrofit — this is where the
  // two vocabularies meet, and it has to be exactly one place: two nodes for
  // one network, called `lan` and `bridge0`, is the bug this shape prevents.
  const networkOfInterface = new Map(
    (networks ?? []).flatMap((n) => n.members.map((m) => [m, n.name] as const)),
  )
  const assignmentOf = new Map(
    (assignments ?? []).map((a) => [networkOfInterface.get(a.interface) ?? a.interface, a] as const),
  )
  const poolOf = new Map((pools ?? []).map((p) => [p.network, p] as const))

  const names = new Set<string>()
  for (const name of assignmentOf.keys()) names.add(name)
  for (const name of poolOf.keys()) names.add(name)
  for (const d of devices) if (d.network) names.add(d.network)

  const containers: Container[] = [...names].map((name) => {
    const assignment = assignmentOf.get(name)
    const exit = assignment?.exit || ''
    const status = exit ? exits?.find((e) => e.name === exit) : undefined
    return {
      key: name,
      name,
      devices: devices
        .filter((d) => d.network === name)
        .sort((a, b) => Number(b.online) - Number(a.online) || a.name.localeCompare(b.name)),
      pool: poolOf.get(name),
      exit,
      inherited: assignment?.source === 'default',
      status,
      down: Boolean(status && status.probed && !status.up),
      kind: 'network' as const,
    }
  })

  // Busiest first, so the network the household lives on leads.
  containers.sort((a, b) => b.devices.length - a.devices.length || a.name.localeCompare(b.name))

  const unplaced = devices.filter((d) => !d.network && d.seen)
  if (unplaced.length) {
    containers.push({
      key: ' unplaced',
      name: 'Not on a known network',
      devices: unplaced,
      inherited: false,
      down: false,
      kind: 'unplaced',
    })
  }

  // Stored but never observed. Without a container of its own, a device somebody
  // typed in by hand would appear nowhere at all.
  const unseen = devices.filter((d) => !d.seen)
  if (unseen.length) {
    containers.push({
      key: ' unseen',
      name: 'Never seen',
      devices: unseen,
      inherited: false,
      down: false,
      kind: 'unseen',
    })
  }

  return containers
}

/* -------------------------------------------------------------------------- */

function NetworkContainer({
  container,
  onSelect,
}: {
  container: Container
  onSelect?: (device: DeviceRow) => void
}) {
  const here = container.devices.filter((d) => d.online).length
  const real = container.kind === 'network'

  return (
    <section
      className={cn(
        'h-full rounded-xl border p-3',
        real ? 'bg-muted/40' : 'border-dashed',
        container.down && 'border-destructive/50 bg-destructive/5',
      )}
    >
      <header className="mb-2.5 flex flex-wrap items-baseline gap-x-2 gap-y-1 px-0.5">
        <span className={cn('text-sm font-medium', !real && 'text-muted-foreground')}>
          {container.name}
        </span>
        <span className="text-xs text-muted-foreground">
          {container.devices.length === 0 ? 'empty' : `${here}/${container.devices.length} here`}
        </span>
        {real && <ExitChip container={container} />}
      </header>

      {container.devices.length > 0 && (
        <ul className="grid gap-1.5 sm:grid-cols-2">
          {container.devices.map((device) => (
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
function ExitChip({ container }: { container: Container }) {
  const label = container.exit || 'direct'
  const unchecked = Boolean(container.exit) && !container.status?.probed

  return (
    <span
      title={container.inherited ? 'Follows the box-wide setting' : `Set on ${container.name}`}
      className={cn(
        'ml-auto inline-flex shrink-0 items-center gap-1.5 rounded-md border px-1.5 py-0.5 text-xs',
        container.inherited && 'border-dashed',
        container.down ? 'border-destructive/50 text-destructive' : 'bg-background text-muted-foreground',
      )}
    >
      <span
        aria-hidden
        className={cn(
          'size-1.5 rounded-full',
          container.down ? 'bg-destructive' : unchecked ? 'bg-muted-foreground/40' : 'bg-success',
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
      <DeviceIcon
        category={device.category}
        vendor={device.vendor}
        vendorKey={device.vendor_key}
        online={device.online}
        size="sm"
      />
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
