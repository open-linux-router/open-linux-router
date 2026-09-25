import {
  ArrowDown,
  ArrowUp,
  CircleDashed,
  FolderInput,
  MoreHorizontal,
  Pencil,
  Plus,
  Router,
  Tag,
  Trash2,
} from 'lucide-react'
import { useCallback, useLayoutEffect, useMemo, useRef, useState } from 'react'

import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Skeleton } from '@/components/ui/skeleton'
import { DeviceIcon } from '@/features/devices/device-icon'
import { useInterfaces } from '@/features/link/queries'
import { magnitude, sumFlows, type Flow, type TrafficView } from '@/features/topology/traffic'
import type { AssignmentStatus, DeviceRow, ExitStatus, NetworkRow } from '@/lib/api-types'
import type { DevicesGroup, Pool } from '@/lib/config-types'
import { cn, formatBytes, formatRate } from '@/lib/utils'

/**
 * The network, drawn top-down: this router, then the operator's groups, then
 * the devices in each.
 *
 * It used to be grouped by *network* — `lan`, `iot0` — and that was the wrong
 * question for the page you land on. A network is how a device is attached;
 * a group is what the operator thinks it is for ("Serving", "Personal", "IoT"),
 * and the second is what somebody looking at their house wants organised. The
 * network did not disappear: it rides on every device row as a chip, it is a
 * filter above the tree, and the router node carries each one with the way it
 * goes out. It just stopped being the shape.
 *
 * Groups are only ever the operator's. None is invented, nothing is sorted
 * into one by category, and a device nobody placed sits in a display-only
 * "Ungrouped" card at the end — which is also what a box with no groups at all
 * shows, with a way to make the first one.
 *
 * Strictly top-down. Every edge leaves the bottom of the router and enters the
 * top of a card, and the cards are laid side by side; nothing is ever drawn
 * left to right, because a diagram that mixes the two directions makes the
 * reader stop to work out which axis means "is inside". On a narrow screen the
 * tree does not branch at all: the cards stack, and one rail runs down their
 * left edge.
 *
 * What it does not claim, still: this is logical structure. "In Serving" says
 * what the operator decided, not what is plugged into what.
 */
export function NetworkMap({
  devices: all,
  groups = [],
  traffic,
  assignments,
  exits,
  pools,
  pending,
  filter = '',
  network = '',
  onSelect,
  onCreateGroup,
  onRenameGroup,
  onDeleteGroup,
  onMoveDevice,
}: {
  devices: DeviceRow[]
  groups?: DevicesGroup[]
  traffic: TrafficView
  assignments?: AssignmentStatus[]
  exits?: ExitStatus[]
  pools?: Pool[]
  pending: boolean
  filter?: string
  /** A network name, NO_NETWORK, or empty for every network. */
  network?: string
  onSelect?: (device: DeviceRow) => void
  onCreateGroup?: () => void
  onRenameGroup?: (name: string) => void
  onDeleteGroup?: (name: string, members: number) => void
  onMoveDevice?: (device: DeviceRow, group: string) => void
}) {
  // The networks, for joining a pool (keyed by network) to a gateway assignment
  // (keyed by interface). Read here rather than passed in because every caller
  // of this component would otherwise have to fetch it only to hand it back.
  const interfaces = useInterfaces()
  const networkRows = interfaces.data?.networks

  const networks = useMemo(
    () => buildNetworks(all, assignments, exits, pools, networkRows),
    [all, assignments, exits, pools, networkRows],
  )

  const filtering = filter.trim() !== '' || network !== ''
  const cards = useMemo(
    () => buildCards(all, groups, traffic, filter, network),
    [all, groups, traffic, filter, network],
  )

  // Bars are scaled against the busiest device on the whole map, not the busiest
  // in the card and not the busiest that survived the search box: a bar has to
  // mean the same length everywhere on screen, and it should not grow because
  // the operator typed something.
  const busiest = useMemo(
    () => Math.max(0, ...all.map((d) => magnitude(traffic.flowOf(d), traffic.rated))),
    [all, traffic],
  )

  if (pending) {
    return (
      <div className="flex flex-col items-center gap-16">
        <Skeleton className="h-16 w-60 rounded-xl" />
        <div className="grid w-full gap-4 sm:grid-cols-3">
          <Skeleton className="h-64 rounded-xl" />
          <Skeleton className="h-64 rounded-xl" />
          <Skeleton className="h-64 rounded-xl" />
        </div>
      </div>
    )
  }

  return (
    <Tree
      networks={networks}
      cards={cards}
      traffic={traffic}
      busiest={busiest}
      filtering={filtering}
      groups={groups}
      onSelect={onSelect}
      onCreateGroup={onCreateGroup}
      onRenameGroup={onRenameGroup}
      onDeleteGroup={onDeleteGroup}
      onMoveDevice={onMoveDevice}
    />
  )
}

/** The network filter's value for devices on none of this router's networks. */
export const NO_NETWORK = ' none'

/* -------------------------------------------------------------------------- */
/* Shape                                                                      */
/* -------------------------------------------------------------------------- */

interface Card {
  key: string
  /** The group's name, or undefined for the Ungrouped card. */
  group?: string
  devices: DeviceRow[]
  /** Members before the search box and the network filter, for the counts. */
  members: DeviceRow[]
  flow?: Flow
  tint: Tint
}

function buildCards(
  all: DeviceRow[],
  groups: DevicesGroup[],
  traffic: TrafficView,
  filter: string,
  network: string,
): Card[] {
  const q = filter.trim().toLowerCase()
  const visible = (d: DeviceRow) =>
    (!network || (network === NO_NETWORK ? !d.network : d.network === network)) &&
    (!q ||
      [d.name, d.mac, d.hostname ?? '', d.vendor ?? '', d.network ?? '', d.group ?? '', ...(d.ips ?? [])]
        .join(' ')
        .toLowerCase()
        .includes(q))

  // Online first, then by name. Not by traffic, though the mockup this was
  // drawn from did: rates are re-read every ten seconds, and a list that
  // re-sorts itself under the pointer is a list nobody can click.
  const order = (a: DeviceRow, b: DeviceRow) =>
    Number(b.online) - Number(a.online) || (a.name || a.mac).localeCompare(b.name || b.mac)

  const known = new Set(groups.map((g) => g.name))
  const card = (key: string, group: string | undefined, members: DeviceRow[], index: number): Card => ({
    key,
    group,
    members,
    devices: members.filter(visible).sort(order),
    flow: sumFlows(members.map((d) => traffic.flowOf(d))),
    tint: group === undefined ? UNGROUPED_TINT : TINTS[index % TINTS.length],
  })

  // Groups keep their stored order (by name, from the server) and every one is
  // drawn even when the filters empty it: "nothing of yours in IoT matches" is
  // an answer, and an IoT card that vanished is a question.
  const out = groups.map((g, i) =>
    card(g.name, g.name, all.filter((d) => d.group === g.name), i),
  )

  // A device naming a group that no longer exists cannot happen through the
  // API — validation refuses it — but a hand-edited olr.json can say anything,
  // and such a device is better shown as ungrouped than not shown.
  const loose = all.filter((d) => !d.group || !known.has(d.group))
  // Ungrouped is display-only and last. It appears whenever something is in
  // it, and always on a box with no groups yet, where it is the whole map.
  if (loose.length > 0 || groups.length === 0) out.push(card(' ungrouped', undefined, loose, 0))
  return out
}

interface NetworkInfo {
  name: string
  exit: string
  inherited: boolean
  down: boolean
  unchecked: boolean
}

/**
 * Every network, with where it goes out.
 *
 * Networks are the union of what link declares, what gateway routes, what dhcp
 * serves and what devices were seen on: any one alone drops a network the
 * others know about. Everything is keyed by *network name*; gateway still names
 * a kernel interface, so its assignments are translated through the network's
 * members on the way in — the one place the two vocabularies meet, and it has
 * to be exactly one place: two chips for one network, called `lan` and
 * `bridge0`, is the bug this shape prevents.
 */
function buildNetworks(
  devices: DeviceRow[],
  assignments?: AssignmentStatus[],
  exits?: ExitStatus[],
  pools?: Pool[],
  networks?: NetworkRow[],
): NetworkInfo[] {
  const networkOfInterface = new Map(
    (networks ?? []).flatMap((n) => n.members.map((m) => [m, n.name] as const)),
  )
  const assignmentOf = new Map(
    (assignments ?? []).map((a) => [networkOfInterface.get(a.interface) ?? a.interface, a] as const),
  )

  const names = new Set<string>()
  for (const n of networks ?? []) names.add(n.name)
  for (const name of assignmentOf.keys()) names.add(name)
  for (const p of pools ?? []) names.add(p.network)
  for (const d of devices) if (d.network) names.add(d.network)

  const count = (name: string) => devices.filter((d) => d.network === name).length
  return [...names]
    .map((name) => {
      const assignment = assignmentOf.get(name)
      const exit = assignment?.exit || ''
      const status = exit ? exits?.find((e) => e.name === exit) : undefined
      return {
        name,
        exit,
        inherited: assignment?.source === 'default',
        down: Boolean(status && status.probed && !status.up),
        unchecked: Boolean(exit) && !status?.probed,
      }
    })
    .sort((a, b) => count(b.name) - count(a.name) || a.name.localeCompare(b.name))
}

/**
 * A group's colour.
 *
 * A small, quiet palette cycled by position — a group has no colour of its own
 * to store, and inventing a field for one would be a setting nobody asked for.
 * Each entry is whole class strings rather than a hue spliced into a template,
 * because Tailwind only emits classes it can find written out.
 */
interface Tint {
  /** The edge, via currentColor. */
  edge: string
  header: string
  tile: string
  bar: string
}

const TINTS: Tint[] = [
  {
    edge: 'text-teal-500/70 dark:text-teal-400/60',
    header: 'bg-teal-500/[0.05] dark:bg-teal-400/[0.07]',
    tile: 'bg-teal-500/12 text-teal-700 dark:bg-teal-400/15 dark:text-teal-300',
    bar: 'bg-teal-500 dark:bg-teal-400',
  },
  {
    edge: 'text-blue-500/70 dark:text-blue-400/60',
    header: 'bg-blue-500/[0.05] dark:bg-blue-400/[0.07]',
    tile: 'bg-blue-500/12 text-blue-700 dark:bg-blue-400/15 dark:text-blue-300',
    bar: 'bg-blue-500 dark:bg-blue-400',
  },
  {
    edge: 'text-amber-500/70 dark:text-amber-400/60',
    header: 'bg-amber-500/[0.06] dark:bg-amber-400/[0.07]',
    tile: 'bg-amber-500/15 text-amber-700 dark:bg-amber-400/15 dark:text-amber-300',
    bar: 'bg-amber-500 dark:bg-amber-400',
  },
  {
    edge: 'text-violet-500/70 dark:text-violet-400/60',
    header: 'bg-violet-500/[0.05] dark:bg-violet-400/[0.07]',
    tile: 'bg-violet-500/12 text-violet-700 dark:bg-violet-400/15 dark:text-violet-300',
    bar: 'bg-violet-500 dark:bg-violet-400',
  },
  {
    edge: 'text-rose-500/70 dark:text-rose-400/60',
    header: 'bg-rose-500/[0.05] dark:bg-rose-400/[0.07]',
    tile: 'bg-rose-500/12 text-rose-700 dark:bg-rose-400/15 dark:text-rose-300',
    bar: 'bg-rose-500 dark:bg-rose-400',
  },
  {
    edge: 'text-emerald-500/70 dark:text-emerald-400/60',
    header: 'bg-emerald-500/[0.05] dark:bg-emerald-400/[0.07]',
    tile: 'bg-emerald-500/12 text-emerald-700 dark:bg-emerald-400/15 dark:text-emerald-300',
    bar: 'bg-emerald-500 dark:bg-emerald-400',
  },
]

/** Ungrouped is not a group, and is drawn in the one colour no group gets. */
const UNGROUPED_TINT: Tint = {
  edge: 'text-muted-foreground/45',
  header: 'bg-muted/40',
  tile: 'bg-muted text-muted-foreground',
  bar: 'bg-muted-foreground/60',
}

/* -------------------------------------------------------------------------- */
/* Layout                                                                     */
/* -------------------------------------------------------------------------- */

/** A card narrower than this cannot fit a name, an address and a rate on one row. */
const MIN_CARD = 280
/** …and one wider than this is mostly whitespace between the name and the rate. */
const MAX_CARD = 420
const GAP_X = 16
/** Room between rows for an edge to bend and a rate label to sit. */
const GAP_Y = 64
/** Room between the router and the first row, for the same two things. */
const DROP = 72
/** Where the rail runs in the stacked layout, from the tree's left edge. */
const RAIL_X = 15

interface Geometry {
  /** The column count the boxes were measured under. */
  cols: number
  width: number
  height: number
  router: { x: number; bottom: number; half: number }
  cards: { key: string; cx: number; top: number; bottom: number; left: number; right: number }[]
}

/**
 * The tree: the router, the cards, and the edges between them.
 *
 * Edges are an SVG laid over the whole thing and drawn from *measured* boxes,
 * not from assumed ones. Cards are as tall as their contents and wrap with the
 * width, so any edge computed from a formula would be wrong the first time a
 * group held one more device than its neighbour. A ResizeObserver re-measures
 * whenever the tree or any card changes size.
 *
 * The column count is chosen here rather than by CSS breakpoints: as many
 * columns as fit at MIN_CARD, never more than there are cards, and the grid
 * capped at MAX_CARD per column and centred. Two groups on a wide screen are
 * two comfortable cards under the router, not two stretched ones pinned left.
 *
 * When cards wrap onto a second row, their edges cannot cut across the first
 * row's cards, so they run down the gutter between two columns — the gap CSS
 * grid guarantees is empty on every row — and turn in the space above their
 * own row. Still top-down: nothing ever leaves the side of a card.
 */
function Tree({
  networks,
  cards,
  traffic,
  busiest,
  filtering,
  groups,
  onSelect,
  onCreateGroup,
  onRenameGroup,
  onDeleteGroup,
  onMoveDevice,
}: {
  networks: NetworkInfo[]
  cards: Card[]
  traffic: TrafficView
  busiest: number
  filtering: boolean
  groups: DevicesGroup[]
  onSelect?: (device: DeviceRow) => void
  onCreateGroup?: () => void
  onRenameGroup?: (name: string) => void
  onDeleteGroup?: (name: string, members: number) => void
  onMoveDevice?: (device: DeviceRow, group: string) => void
}) {
  const wrap = useRef<HTMLDivElement>(null)
  const routerRef = useRef<HTMLDivElement>(null)
  const [geo, setGeo] = useState<Geometry | null>(null)

  const width = geo?.width ?? 0
  const narrow = width > 0 && width < 2 * MIN_CARD + GAP_X
  const fit = Math.max(1, Math.floor((width + GAP_X) / (MIN_CARD + GAP_X)))
  const cols = narrow ? 1 : Math.min(fit, Math.max(1, cards.length))

  const measure = useCallback(() => {
    const root = wrap.current
    const router = routerRef.current
    if (!root || !router) return
    const origin = root.getBoundingClientRect()
    const r = router.getBoundingClientRect()
    const next: Geometry = {
      cols: Number(root.dataset.cols),
      width: Math.round(origin.width),
      height: Math.round(origin.height),
      router: {
        x: r.left + r.width / 2 - origin.left,
        bottom: r.bottom - origin.top,
        half: r.width / 2,
      },
      cards: [...root.querySelectorAll<HTMLElement>('[data-tree-card]')].map((el) => {
        const b = el.getBoundingClientRect()
        return {
          key: el.dataset.treeCard ?? '',
          cx: b.left + b.width / 2 - origin.left,
          top: b.top - origin.top,
          bottom: b.bottom - origin.top,
          left: b.left - origin.left,
          right: b.right - origin.left,
        }
      }),
    }
    // Only a real change is stored, so measuring after every render cannot
    // turn into rendering after every measure.
    setGeo((prev) => (prev && JSON.stringify(prev) === JSON.stringify(next) ? prev : next))
  }, [])

  // After every render, before paint: a card's contents can change without
  // its own size changing, but its *position* moves when a neighbour's does.
  useLayoutEffect(measure)

  useLayoutEffect(() => {
    const root = wrap.current
    if (!root) return
    const observer = new ResizeObserver(measure)
    observer.observe(root)
    for (const el of root.querySelectorAll('[data-tree-card]')) observer.observe(el)
    return () => observer.disconnect()
  }, [measure, cards.length, cols])

  // Edges are drawn only from a measurement of the layout being shown: the
  // first render after the column count changes still holds boxes from the
  // old one, and the layout effect replaces them before anything is painted.
  const current = geo?.cols === cols
  const edges = geo && current && !narrow ? layoutEdges(geo, cards, cols, traffic) : []
  const rail = geo && narrow ? layoutRail(geo) : null
  const noGroups = groups.length === 0

  return (
    <div ref={wrap} data-cols={cols} className="relative">
      <svg
        aria-hidden
        className="pointer-events-none absolute inset-0 overflow-visible"
        width={geo?.width ?? 0}
        height={geo?.height ?? 0}
      >
        {edges.map((e) => (
          <path
            key={e.key}
            d={e.d}
            fill="none"
            stroke="currentColor"
            strokeWidth={e.width}
            strokeLinecap="round"
            strokeDasharray={e.dashed ? '4 5' : undefined}
            className={e.tint.edge}
          />
        ))}
        {rail && (
          <>
            <path d={rail.d} fill="none" stroke="currentColor" strokeWidth={1.5} className="text-border" />
            {rail.dots.map((y, i) => (
              <circle
                key={i}
                cx={RAIL_X}
                cy={y}
                r={3.5}
                className={cn('fill-card', cards[i]?.tint.edge)}
                stroke="currentColor"
                strokeWidth={2}
              />
            ))}
          </>
        )}
      </svg>

      {/* Rate labels are HTML over the SVG rather than SVG text, so they get
          the same type, colour tokens and dark mode as everything else. */}
      {edges.map(
        (e) =>
          e.label && (
            <div
              key={`${e.key}-label`}
              className="pointer-events-none absolute z-10 -translate-x-1/2 -translate-y-1/2"
              style={{ left: e.label.x, top: e.label.y }}
            >
              <RatePill flow={e.flow} />
            </div>
          ),
      )}

      <div className={cn('relative flex', narrow ? 'justify-start' : 'justify-center')}>
        <RouterNode ref={routerRef} networks={networks} traffic={traffic} />
      </div>

      <ul
        className="relative grid"
        style={{
          gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))`,
          columnGap: GAP_X,
          rowGap: narrow ? 12 : GAP_Y,
          marginTop: narrow ? 24 : DROP,
          maxWidth: narrow ? undefined : cols * MAX_CARD + (cols - 1) * GAP_X,
          marginInline: narrow ? undefined : 'auto',
          paddingLeft: narrow ? RAIL_X + 17 : undefined,
        }}
      >
        {cards.map((card) => (
          <li key={card.key} data-tree-card={card.key} className="min-w-0">
            <GroupCard
              card={card}
              traffic={traffic}
              busiest={busiest}
              filtering={filtering}
              groups={groups}
              emptyState={noGroups && card.group === undefined}
              onSelect={onSelect}
              onCreateGroup={onCreateGroup}
              onRenameGroup={onRenameGroup}
              onDeleteGroup={onDeleteGroup}
              onMoveDevice={onMoveDevice}
            />
          </li>
        ))}
      </ul>
    </div>
  )
}

interface Edge {
  key: string
  d: string
  width: number
  dashed: boolean
  tint: Tint
  flow?: Flow
  label?: { x: number; y: number }
}

/**
 * Edge thickness, from a group's share of the busiest group's traffic.
 *
 * Square-rooted so a group doing a tenth of the work still reads as a line
 * and not a hair, and clamped at both ends: with no rate, every edge is the
 * thin one, which is the honest drawing of "we do not know".
 */
const EDGE_MIN = 1.5
const EDGE_MAX = 5

function layoutEdges(geo: Geometry, cards: Card[], cols: number, traffic: TrafficView): Edge[] {
  const { x: rx, bottom: ry, half } = geo.router
  const boxes = new Map(geo.cards.map((c) => [c.key, c]))
  const heaviest = Math.max(0, ...cards.map((c) => magnitude(c.flow, traffic.rated)))

  // Row extents, for the gap above each row and the gutter down to it.
  const rows: { top: number; bottom: number }[] = []
  cards.forEach((card, i) => {
    const b = boxes.get(card.key)
    if (!b) return
    const r = Math.floor(i / cols)
    rows[r] = rows[r]
      ? { top: Math.min(rows[r].top, b.top), bottom: Math.max(rows[r].bottom, b.bottom) }
      : { top: b.top, bottom: b.bottom }
  })

  // The gutter nearest the router's centre line. With an even number of
  // columns it is exactly under the router, and the wrapped edges drop
  // straight down the middle.
  const firstRow = cards
    .slice(0, cols)
    .map((c) => boxes.get(c.key))
    .filter((b) => b !== undefined)
  let gutter = rx
  if (firstRow.length > 1) {
    let best = Infinity
    for (let i = 0; i < firstRow.length - 1; i++) {
      const g = (firstRow[i].right + firstRow[i + 1].left) / 2
      if (Math.abs(g - rx) < best) {
        best = Math.abs(g - rx)
        gutter = g
      }
    }
  }

  // An S-curve: straight down out of the upper point, straight down into the
  // lower, turning halfway — one shape for every edge, so a steeper one only
  // ever means a card further to the side.
  const s = (y0: number, x1: number, y1: number, x0: number) => {
    const m = (y0 + y1) / 2
    return `C ${x0} ${m} ${x1} ${m} ${x1} ${y1}`
  }

  return cards.flatMap((card, i) => {
    const b = boxes.get(card.key)
    const row = Math.floor(i / cols)
    // A geometry measured under a different column count (the render after a
    // resize, before the re-measure lands) has rows that do not line up with
    // this one's; draw nothing for a frame rather than an edge to nowhere.
    if (!b || !rows[row] || (row > 0 && !rows[row - 1])) return []

    const share = heaviest > 0 ? magnitude(card.flow, traffic.rated) / heaviest : 0
    const width =
      traffic.rated && heaviest > 0 ? EDGE_MIN + (EDGE_MAX - EDGE_MIN) * Math.sqrt(share) : EDGE_MIN

    // Each label sits on the last, vertical stretch of its edge, just above
    // the card it belongs to. Halfway along the curve read better in a mockup
    // with three groups and collided with four: every midpoint lands on the
    // same horizontal band under the router.
    const label = { x: b.cx, y: b.top - 16 }

    let d: string
    if (row === 0) {
      // Edges leave from along the router's bottom edge, spread toward the
      // side they are heading, rather than all from one point: from one
      // point the thick ones lie on top of each other for their first inch.
      const x0 = rx + Math.max(-half + 20, Math.min(half - 20, (b.cx - rx) * 0.15))
      d = `M ${x0} ${ry} ${s(ry, b.cx, b.top, x0)}`
    } else {
      // Down the gutter to the gap above this row, then into the card.
      const gapTop = rows[row - 1].bottom
      const row0 = rows[0].top
      d =
        `M ${rx} ${ry} ${s(ry, gutter, row0, rx)} ` +
        `L ${gutter} ${gapTop} ${s(gapTop, b.cx, b.top, gutter)}`
    }

    return [
      {
        key: card.key,
        d,
        width,
        dashed: card.group === undefined,
        tint: card.tint,
        flow: card.flow,
        // A label only where there is a rate to put on it: a pill reading "—"
        // on every edge would be noise saying what the missing thickness
        // already says.
        label: traffic.rated && card.flow ? label : undefined,
      },
    ]
  })
}

/**
 * The stacked layout's one line: from under the router, down the cards' left
 * side, with a dot level with each card's header. No branches — at this width
 * a branch would have nowhere to go but sideways.
 */
function layoutRail(geo: Geometry) {
  if (geo.cards.length === 0) return null
  const dots = geo.cards.map((c) => c.top + 28)
  return { d: `M ${RAIL_X} ${geo.router.bottom} L ${RAIL_X} ${dots[dots.length - 1]}`, dots }
}

/* -------------------------------------------------------------------------- */
/* Nodes                                                                      */
/* -------------------------------------------------------------------------- */

/**
 * The router, and — because a network is how the router reaches a device —
 * every network with the way it goes out.
 *
 * Those chips were the header of each network's container before groups took
 * over the shape. They moved here rather than away: whether `iot0` has a way
 * out, and whether that way is working, is still the most urgent fact on the
 * map when it is bad. Inheritance is a dashed outline, a way out that is not
 * answering is red, and one nobody checks has a grey dot rather than a green
 * one it never earned.
 */
function RouterNode({
  ref,
  networks,
  traffic,
}: {
  ref: React.Ref<HTMLDivElement>
  networks: NetworkInfo[]
  traffic: TrafficView
}) {
  return (
    <div
      ref={ref}
      className="relative z-10 max-w-full min-w-60 rounded-xl border bg-card px-4 py-3 shadow-sm"
    >
      <div className="flex items-center gap-3">
        <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted">
          <Router className="size-4.5 text-foreground/80" aria-hidden />
        </span>
        <div className="min-w-0">
          <div className="text-sm font-semibold">This router</div>
          {traffic.rated ? (
            <Rates flow={traffic.total} className="text-xs" />
          ) : (
            <div className="text-xs text-muted-foreground">
              {networks.length === 0
                ? 'No networks yet'
                : `${networks.length} ${networks.length === 1 ? 'network' : 'networks'}`}
            </div>
          )}
        </div>
      </div>

      {networks.length > 0 && (
        <ul className="mt-2.5 flex flex-wrap gap-1.5 border-t pt-2.5" aria-label="Networks">
          {networks.map((n) => (
            <li key={n.name}>
              <NetworkChip network={n} />
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

function NetworkChip({ network: n }: { network: NetworkInfo }) {
  return (
    <span
      title={
        (n.down ? `${n.exit} is not responding. ` : '') +
        (n.inherited ? 'Follows the box-wide setting' : `Set on ${n.name}`)
      }
      className={cn(
        'inline-flex items-center gap-1.5 rounded-md border px-1.5 py-0.5 text-xs',
        n.inherited && 'border-dashed',
        n.down ? 'border-destructive/50 text-destructive' : 'text-muted-foreground',
      )}
    >
      <span
        aria-hidden
        className={cn(
          'size-1.5 rounded-full',
          n.down ? 'bg-destructive' : n.unchecked ? 'bg-muted-foreground/40' : 'bg-success',
        )}
      />
      <span className="font-mono text-foreground/85">{n.name}</span>
      <span aria-hidden>→</span>
      <span className="sr-only">goes out</span>
      {n.exit || 'direct'}
    </span>
  )
}

/** A card shows this many devices before offering the rest. */
const COLLAPSED_ROWS = 8

function GroupCard({
  card,
  traffic,
  busiest,
  filtering,
  groups,
  emptyState,
  onSelect,
  onCreateGroup,
  onRenameGroup,
  onDeleteGroup,
  onMoveDevice,
}: {
  card: Card
  traffic: TrafficView
  busiest: number
  filtering: boolean
  groups: DevicesGroup[]
  /** The Ungrouped card on a box with no groups at all. */
  emptyState: boolean
  onSelect?: (device: DeviceRow) => void
  onCreateGroup?: () => void
  onRenameGroup?: (name: string) => void
  onDeleteGroup?: (name: string, members: number) => void
  onMoveDevice?: (device: DeviceRow, group: string) => void
}) {
  const [expanded, setExpanded] = useState(false)
  const here = card.members.filter((d) => d.online).length
  const title = card.group ?? 'Ungrouped'

  // Searching shows every match: a hit hidden behind "Show 12 more" is a
  // search that looks like it found nothing.
  const shown = expanded || filtering ? card.devices : card.devices.slice(0, COLLAPSED_ROWS)
  const hidden = card.devices.length - shown.length

  return (
    <section
      aria-label={title}
      className={cn(
        // A container, so the header can decide by its own width — not the
        // viewport's — whether the rates fit beside the name. A card in a
        // three-column row on a laptop is narrower than a phone's only card.
        '@container flex h-full flex-col overflow-hidden rounded-xl border bg-card shadow-xs',
        card.group === undefined && 'border-dashed',
      )}
    >
      <header className={cn('flex items-center gap-3 border-b px-3.5 py-3', card.tint.header)}>
        <span
          aria-hidden
          className={cn(
            'flex size-8 shrink-0 items-center justify-center rounded-lg text-sm font-semibold',
            card.tint.tile,
          )}
        >
          {card.group === undefined ? (
            <CircleDashed className="size-4" />
          ) : (
            [...card.group][0]?.toUpperCase()
          )}
        </span>
        <div className="min-w-0 flex-1">
          <h3 className="truncate text-sm font-semibold">{title}</h3>
          <p className="truncate text-xs text-muted-foreground tabular-nums">
            {card.members.length === 0
              ? 'No devices'
              : `${here} here · ${card.members.length} ${card.members.length === 1 ? 'device' : 'devices'}`}
          </p>
          {traffic.rated && card.flow && (
            <Rates flow={card.flow} className="mt-0.5 text-xs @xs:hidden" />
          )}
        </div>
        {traffic.rated && card.flow && (
          <Rates flow={card.flow} className="hidden text-xs @xs:inline-flex" />
        )}
        {card.group !== undefined && (onRenameGroup || onDeleteGroup) && (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="-mr-1.5 text-muted-foreground"
                  aria-label={`${title} options`}
                />
              }
            >
              <MoreHorizontal />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-40">
              {onRenameGroup && (
                <DropdownMenuItem onClick={() => onRenameGroup(card.group!)}>
                  <Pencil /> Rename
                </DropdownMenuItem>
              )}
              {onDeleteGroup && (
                <DropdownMenuItem
                  variant="destructive"
                  onClick={() => onDeleteGroup(card.group!, card.members.length)}
                >
                  <Trash2 /> Delete
                </DropdownMenuItem>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </header>

      {emptyState && (
        <div className="flex flex-col items-start gap-2.5 border-b bg-muted/30 px-3.5 py-3 text-xs text-muted-foreground">
          <p>
            Groups are yours to make — Serving, Personal, IoT, whatever fits your house. Make one,
            then move devices into it from each device&apos;s menu.
          </p>
          {onCreateGroup && (
            <Button size="sm" variant="outline" onClick={onCreateGroup}>
              <Plus /> Create a group
            </Button>
          )}
        </div>
      )}

      {shown.length > 0 ? (
        <ul className="divide-y">
          {shown.map((device) => (
            <li key={device.mac}>
              <DeviceRowView
                device={device}
                flow={traffic.flowOf(device)}
                traffic={traffic}
                busiest={busiest}
                tint={card.tint}
                groups={groups}
                onSelect={onSelect}
                onMove={onMoveDevice}
              />
            </li>
          ))}
        </ul>
      ) : (
        <p className="px-3.5 py-6 text-center text-xs text-muted-foreground">
          {card.members.length > 0
            ? 'Nothing here matches.'
            : card.group === undefined
              ? 'Every device is in a group.'
              : 'Nothing in it yet. Use a device’s menu to move it here.'}
        </p>
      )}

      {hidden > 0 && (
        <button
          type="button"
          onClick={() => setExpanded(true)}
          className="mt-auto border-t px-3.5 py-2 text-left text-xs font-medium text-muted-foreground transition-colors hover:bg-accent/60 hover:text-foreground"
        >
          Show {hidden} more
        </button>
      )}
    </section>
  )
}

function DeviceRowView({
  device,
  flow,
  traffic,
  busiest,
  tint,
  groups,
  onSelect,
  onMove,
}: {
  device: DeviceRow
  flow?: Flow
  traffic: TrafficView
  busiest: number
  tint: Tint
  groups: DevicesGroup[]
  onSelect?: (device: DeviceRow) => void
  onMove?: (device: DeviceRow, group: string) => void
}) {
  const address = device.fixed_ip ?? device.ips?.[0]
  const share = busiest > 0 ? magnitude(flow, traffic.rated) / busiest : 0

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
        <span className="flex items-center gap-1.5">
          <span className="truncate text-sm font-medium">{device.name || device.mac}</span>
          {/* Presence as a dot, paired with the dimming — never colour alone. */}
          <span
            aria-label={device.online ? 'here' : device.seen ? 'away' : 'never seen'}
            className={cn(
              'size-1.5 shrink-0 rounded-full',
              device.online ? 'bg-success' : 'bg-muted-foreground/30',
            )}
          />
        </span>
        <span className="mt-0.5 flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
          {!device.seen ? (
            <span className="italic">never seen</span>
          ) : (
            address && (
              <span
                className="flex min-w-0 items-center gap-1"
                // The whole address on hover: a narrow card truncates it, and
                // an address is the one field nobody can guess the end of.
                title={device.fixed_ip ? `${address} (fixed)` : address}
              >
                {device.fixed_ip && <Tag className="size-3 shrink-0" aria-label="fixed" />}
                <span className="truncate font-mono text-[0.7rem]">{address}</span>
              </span>
            )
          )}
          {device.network ? (
            <span className="shrink-0 rounded border px-1 font-mono text-[0.65rem] leading-4">
              {device.network}
            </span>
          ) : (
            device.seen && (
              <span
                title="Not on any of this router's networks"
                className="shrink-0 rounded border border-dashed px-1 text-[0.65rem] leading-4"
              >
                no network
              </span>
            )
          )}
        </span>
      </span>

      {traffic.counting && device.seen && (
        <span className="w-20 shrink-0 text-right sm:w-30">
          <span className="block truncate text-[0.68rem] text-muted-foreground tabular-nums">
            {flow ? <RowTraffic flow={flow} rated={traffic.rated} /> : '—'}
          </span>
          <span className="mt-1.5 block h-1 overflow-hidden rounded-full bg-muted">
            <span
              className={cn('block h-full rounded-full', tint.bar)}
              // A floor of 2% so a device that moved *something* is visibly not
              // the same as one that moved nothing.
              style={{ width: share > 0 ? `${Math.max(2, share * 100)}%` : 0 }}
            />
          </span>
        </span>
      )}
    </>
  )

  const shape = cn(
    'flex min-h-13 min-w-0 flex-1 items-center gap-2.5 py-2 pl-3.5 text-left',
    onMove ? 'pr-1' : 'pr-3.5',
    !device.online && 'opacity-65',
  )

  return (
    <div className="group/row flex items-center transition-colors hover:bg-accent/50">
      {onSelect ? (
        <button
          type="button"
          onClick={() => onSelect(device)}
          className={cn(
            shape,
            'focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring',
          )}
        >
          {body}
        </button>
      ) : (
        <div className={shape}>{body}</div>
      )}
      {onMove && <RowMenu device={device} groups={groups} onSelect={onSelect} onMove={onMove} />}
    </div>
  )
}

/**
 * The quick way to move a device, one click from the map.
 *
 * Revealed on hover where there is hover, always there where there is not —
 * on a phone there is no pointer to reveal it, and a control that only exists
 * for mice is one half the household cannot reach.
 */
function RowMenu({
  device,
  groups,
  onSelect,
  onMove,
}: {
  device: DeviceRow
  groups: DevicesGroup[]
  onSelect?: (device: DeviceRow) => void
  onMove: (device: DeviceRow, group: string) => void
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="ghost"
            size="icon-sm"
            className="mr-1.5 shrink-0 text-muted-foreground opacity-0 group-hover/row:opacity-100 focus-visible:opacity-100 data-popup-open:opacity-100 pointer-coarse:opacity-100"
            aria-label={`${device.name || device.mac} options`}
          />
        }
      >
        <MoreHorizontal />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-48">
        {onSelect && (
          <>
            <DropdownMenuItem onClick={() => onSelect(device)}>
              <Pencil /> Details…
            </DropdownMenuItem>
            <DropdownMenuSeparator />
          </>
        )}
        <DropdownMenuSub>
          <DropdownMenuSubTrigger>
            <FolderInput /> Move to group
          </DropdownMenuSubTrigger>
          <DropdownMenuSubContent className="min-w-40">
            {groups.length === 0 ? (
              <DropdownMenuItem disabled>No groups yet</DropdownMenuItem>
            ) : (
              <DropdownMenuRadioGroup
                value={device.group ?? ''}
                onValueChange={(value: string) => {
                  if (value !== (device.group ?? '')) onMove(device, value)
                }}
              >
                {groups.map((g) => (
                  <DropdownMenuRadioItem key={g.name} value={g.name}>
                    {g.name}
                  </DropdownMenuRadioItem>
                ))}
                <DropdownMenuSeparator />
                <DropdownMenuRadioItem value="">No group</DropdownMenuRadioItem>
              </DropdownMenuRadioGroup>
            )}
          </DropdownMenuSubContent>
        </DropdownMenuSub>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/* -------------------------------------------------------------------------- */
/* Numbers                                                                    */
/* -------------------------------------------------------------------------- */

/** ↓ and ↑ as rates. Down first: it is the number people mean by "busy". */
export function Rates({ flow, className }: { flow: Flow; className?: string }) {
  return (
    <span className={cn('inline-flex shrink-0 items-center gap-2 tabular-nums', className)}>
      <span className="inline-flex items-center gap-0.5">
        <ArrowDown className="size-3 text-blue-600 dark:text-blue-400" aria-label="down" />
        {formatRate(flow.downRate ?? 0)}
      </span>
      <span className="inline-flex items-center gap-0.5 text-muted-foreground">
        <ArrowUp className="size-3 text-teal-600 dark:text-teal-400" aria-label="up" />
        {formatRate(flow.upRate ?? 0)}
      </span>
    </span>
  )
}

function RatePill({ flow }: { flow?: Flow }) {
  if (!flow) return null
  return (
    <div className="rounded-full border bg-card px-2 py-0.5 text-[0.7rem] font-medium whitespace-nowrap shadow-xs">
      <Rates flow={flow} />
    </div>
  )
}

/**
 * A row's figure: the rate when there is one, and until then the running
 * total — one number, because "↓ 0 bps ↑ 0 bps" before the second sample
 * would claim an idleness nobody measured.
 */
function RowTraffic({ flow, rated }: { flow: Flow; rated: boolean }) {
  if (!rated) return <>{formatBytes(flow.down + flow.up)}</>
  const down = flow.downRate ?? 0
  const up = flow.upRate ?? 0
  if (down + up < 1) return <span className="text-muted-foreground/70">idle</span>
  return (
    <>
      <span className="sm:hidden">{formatRate(down + up)}</span>
      <span className="hidden sm:inline">
        ↓ {formatRate(down)} <span className="text-muted-foreground/70">↑ {formatRate(up)}</span>
      </span>
    </>
  )
}
