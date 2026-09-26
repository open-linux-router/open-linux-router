import { chainTo, OTHER_KEY, type MapGroup, type MapTree } from '@/features/topology/model'
import type { DeviceRow } from '@/lib/api-types'

/**
 * Where everything on the network map goes, worked out as numbers.
 *
 * Nothing here touches the DOM. Every box on the map has a size this module
 * decides — nodes are fixed within a range, text inside them truncates rather
 * than wraps, and a container is exactly as big as what it holds — so the whole
 * picture can be computed before anything is drawn, and the one thing the
 * browser has to be asked is how wide the map is and how big the router's box
 * came out. That is what makes the rest possible:
 *
 *   - Lines are drawn from the same numbers the boxes are placed with, so a
 *     line cannot end anywhere but on its box, on the first frame or any
 *     other. The previous map measured its cards after layout and drew from
 *     the measurement, and the frame between the two was the one where lines
 *     pointed at nothing.
 *
 *   - Moving between layouts — expanding a fold, switching density, focusing a
 *     group — is interpolating between two sets of numbers, and every box and
 *     every line is interpolated with the same curve over the same time, so
 *     they arrive together. See network-map.tsx for why that is enough.
 *
 *   - Harmony is a rule that can be written down and enforced, rather than
 *     whatever CSS grid made of the content that week.
 *
 * **One rule at every level: show at most K, fold the rest.** A container
 * shows at most K children, busiest first, and the rest go into one full-width
 * "+N more" row that opens in place. A container at or under K is never
 * folded: a network of a few dozen devices is seen whole.
 *
 * **Harmony is a grid.** Every top-level container is the same width, on
 * columns the page shares; every container in a row is the same height; inside
 * each, devices sit in two columns and a subgroup takes the full width. An
 * earlier version sized each container to its contents by scoring shapes, and
 * the result was a row of boxes that were each reasonable and together a mess —
 * no two edges lined up. Uniform is calmer than optimal.
 *
 * **Lines reach one row.** Groups that fit in one row each get a curve from
 * the router, thickness by traffic. More than one row is drawn as a grid inside
 * one outline with one link, because a line to a second row would have to pass
 * between the cards of the first.
 */

export type Density = 'compact' | 'detail'

export interface NodeSize {
  /** The narrowest a container of these may be: room for its header. */
  minW: number
  /** The natural width; nodes stretch from here up to maxW to fill a row. */
  w: number
  maxW: number
  h: number
  /** Children a container shows before folding the rest. */
  k: number
}

/**
 * The two node sizes. Compact is for seeing structure — icon and name — and
 * shows more before folding because each one costs less space; detail carries
 * the address and the rate, and folds sooner. A detail node loses its third
 * line, the traffic, when nothing is being counted.
 */
export function nodeSize(density: Density, counting: boolean): NodeSize {
  return density === 'compact'
    ? { minW: 168, w: 144, maxW: 184, h: 40, k: 16 }
    : { minW: 208, w: 232, maxW: 288, h: counting ? 68 : 56, k: 10 }
}

/** Below this the map stops branching and stacks. */
export const NARROW = 640

// Container anatomy.
const PAD = 12
/** The header band: title and count, including the padding above them. */
export const HEAD = 40
const GAP = 8
const SUB_GAP = 12
const MORE_H = 32
const EMPTY_H = 36
/**
 * A fold never hides fewer than this. "+2 more" costs a row to save two, and
 * reads as the map being stingy rather than tidy.
 */
const MIN_FOLD = 3

// The router's level.
const ROW_GAP = 16
const DROP = 64
const OVERFLOW_GAP = 24

// Focus.
export const CHIP_H = 36
const CHIP_W = 164
const CHIP_GAP = 12
const SIDE_GAP = 28
const FOCUS_W = 264
export const FOCUS_H = 56
const CHAIN_GAP = 36
const FAN_GAP = 16

// Narrow.
export const RAIL_X = 15
const RAIL_INSET = 32

/** Line thickness, from a share of the heaviest sibling's traffic. */
export const EDGE_MIN = 1.5
export const EDGE_MAX = 5

export interface Box {
  x: number
  y: number
  w: number
  h: number
}

/**
 * How a group is drawn. `container` holds its children; `chip` is a group set
 * aside while another is focused, or a rung on the way down to it; `focused`
 * is the group being looked at; `card` is a subgroup fanned out beneath it.
 * All four are the same element under the same key, so one turning into
 * another is an animation, not a swap.
 */
export type GroupVariant = 'container' | 'chip' | 'focused' | 'card'

export interface Hidden {
  devices: number
  groups: number
}

export type Item =
  | { kind: 'group'; key: string; group: MapGroup; box: Box; variant: GroupVariant; open: boolean }
  | { kind: 'device'; key: string; device: DeviceRow; box: Box }
  | {
      kind: 'more'
      key: string
      box: Box
      /** A full-width row inside a container, or a node-sized card in a row. */
      variant: 'row' | 'card'
      /** The expansion key it toggles. */
      target: string
      open: boolean
      hidden: Hidden
      /** Devices behind a top-level fold, for its second line. */
      devices?: number
      /** Whether "Other devices" is among what a top-level fold hides. */
      others?: boolean
    }
  | { kind: 'pick'; key: string; box: Box; groups: MapGroup[] }
  | { kind: 'note'; key: string; box: Box; note: 'empty' | 'hint' | 'nothing' }

export interface Link {
  key: string
  x0: number
  y0: number
  x1: number
  y1: number
  width: number
  /** For links to something that is not a group of the operator's. */
  dashed?: boolean
  /** For links to groups set aside while another is focused. */
  faint?: boolean
}

export interface Rail {
  x: number
  y0: number
  y1: number
  dots: { key: string; y: number }[]
}

export interface Layout {
  width: number
  height: number
  narrow: boolean
  /**
   * Whether this is the whole picture, comfortably: nothing folded, and every
   * top-level container inside the shape band. The automatic density asks
   * this of the detail layout and falls back to compact when the answer is
   * no — detail that has to hide devices or squeeze a group into a column is
   * worse than compact that shows them all.
   */
  fits: boolean
  router: Box
  items: Item[]
  links: Link[]
  rail?: Rail
  /**
   * The outline round a wall of groups — drawn when the groups need more than
   * one row, so the router has one thing to link to. Not a node: it is not
   * clickable and has nothing of its own to say.
   */
  frame?: Box
}

export interface LayoutInput {
  tree: MapTree
  width: number
  density: Density
  router: { w: number; h: number }
  /** Container keys, TOP_KEY and fanKey()s whose fold is open. */
  expanded: ReadonlySet<string>
  focus?: string
  counting: boolean
  rated: boolean
}

/** The expansion key of the router's own "+N more groups". */
export const TOP_KEY = ' top'
/** The expansion key of a focused group's fan. */
export const fanKey = (key: string) => `fan:${key}`

export function layout(input: LayoutInput): Layout {
  if (input.width < NARROW) return layoutNarrow(input)
  if (input.focus && input.tree.byKey.has(input.focus)) return layoutFocus(input, input.focus)
  return layoutTree(input)
}

/* -------------------------------------------------------------------------- */
/* Containers                                                                  */
/* -------------------------------------------------------------------------- */

/** One way to arrange a container: which children, in how many columns. */
interface Plan {
  group: MapGroup
  w: number
  h: number
  cost: number
  cols: number
  devices: DeviceRow[]
  /** Subgroups, as rows of plans. */
  shelves: Plan[][]
  more?: { hidden: Hidden; open: boolean }
  note?: 'empty' | 'hint'
  /** Whether it is showing everything because it was asked to. */
  open?: boolean
}

interface Ctx {
  node: NodeSize
  /** A container alone in the router's row, which may show twice as many. */
  solo?: string
  expanded: ReadonlySet<string>
  filtering: boolean
  narrow: boolean
  /** The container that gets the "make your first group" hint. */
  hint?: string
  cache: Map<string, Plan[]>
}

const gridSpan = (n: number, size: number, gap: number) => (n > 0 ? n * size + (n - 1) * gap : 0)

function chunk<T>(list: T[], size: number): T[][] {
  const out: T[][] = []
  for (let i = 0; i < list.length; i += size) out.push(list.slice(i, i + size))
  return out
}

function hintHeight(narrow: boolean) {
  return narrow ? 92 : 56
}

/**
 * The narrow version of a container: exactly `width` wide, as many columns
 * of nodes as fit, subgroups stacked full-width underneath. No scoring — at
 * this width there is one sensible answer.
 */
function narrowPlan(g: MapGroup, width: number, ctx: Ctx): Plan {
  const { node } = ctx
  const userOpen = ctx.expanded.has(g.key)
  const open = ctx.filtering || userOpen
  const nd = g.devices.length
  const ns = g.groups.length
  const n = nd + ns
  const k = g.key === ctx.solo ? node.k * 2 : node.k
  const s = open || n - k < MIN_FOLD ? n : k
  const sg = Math.min(ns, s)
  const sd = s - sg
  const hidden = { devices: nd - sd, groups: ns - sg }
  const folded = hidden.devices + hidden.groups > 0
  const more = folded ? { hidden, open: false } : userOpen ? { hidden, open: true } : undefined
  const note = g.key === ctx.hint ? 'hint' : n === 0 ? 'empty' : undefined

  const inner = width - 2 * PAD
  const cols = sd > 0 ? Math.max(1, Math.floor((inner + GAP) / (node.w + GAP))) : 0
  const subs = g.groups.slice(0, sg).map((sub) => narrowPlan(sub, inner, ctx))
  const rows = cols > 0 ? Math.ceil(sd / cols) : 0
  const parts = [
    gridSpan(rows, node.h, GAP),
    subs.reduce((t, p) => t + p.h, 0) + Math.max(0, subs.length - 1) * SUB_GAP,
    note === 'hint' ? hintHeight(true) : note ? EMPTY_H : 0,
  ].filter((p) => p > 0)
  let bodyH = parts.reduce((t, p) => t + p, 0) + Math.max(0, parts.length - 1) * SUB_GAP
  if (more) bodyH += (bodyH > 0 ? GAP : 0) + MORE_H
  return {
    group: g,
    w: width,
    h: HEAD + bodyH + PAD,
    cost: 0,
    cols,
    devices: g.devices.slice(0, sd),
    shelves: subs.map((p) => [p]),
    more,
    note,
  }
}

/**
 * Puts a planned container and everything in it at (x, y), `w` wide — which
 * may be wider than the plan, in which case nodes and subgroups stretch to
 * fill it — and at least `minH` tall.
 */
function place(plan: Plan, x: number, y: number, w: number, minH: number, ctx: Ctx, out: Item[]) {
  const { node } = ctx
  const h = Math.max(plan.h, minH)
  out.push({
    kind: 'group',
    key: plan.group.key,
    group: plan.group,
    box: { x, y, w, h },
    variant: 'container',
    open: ctx.expanded.has(plan.group.key),
  })

  const inner = w - 2 * PAD
  let cy = y + HEAD
  const gap = () => (cy > y + HEAD ? SUB_GAP : 0)

  // The note leads: a hint about making groups is read before the devices,
  // and "nothing in it yet" is the whole body anyway.
  if (plan.note) {
    const nh = plan.note === 'hint' ? hintHeight(ctx.narrow) : EMPTY_H
    out.push({ kind: 'note', key: `note:${plan.group.key}`, box: { x: x + PAD, y: cy, w: inner, h: nh }, note: plan.note })
    cy += nh
  }

  if (plan.cols > 0) {
    cy += gap()
    const fill = (inner - (plan.cols - 1) * GAP) / plan.cols
    const nw = ctx.narrow ? fill : Math.min(node.maxW, fill)
    plan.devices.forEach((d, i) => {
      const col = i % plan.cols
      const row = Math.floor(i / plan.cols)
      out.push({
        kind: 'device',
        key: `d:${d.mac}`,
        device: d,
        box: { x: x + PAD + col * (nw + GAP), y: cy + row * (node.h + GAP), w: nw, h: node.h },
      })
    })
    cy += gridSpan(Math.ceil(plan.devices.length / plan.cols), node.h, GAP)
  }

  for (const shelf of plan.shelves) {
    cy += gap()
    const natural = shelf.reduce((t, p) => t + p.w, 0)
    const spare = inner - natural - (shelf.length - 1) * SUB_GAP
    const shelfH = Math.max(...shelf.map((p) => p.h))
    let sx = x + PAD
    for (const p of shelf) {
      // Spare width is shared in proportion, so a shelf fills its container
      // and the boxes in it keep their relative sizes — but no box grows past
      // what its own nodes can stretch to fill, because a subgroup of two
      // stretched across a parent of eight is mostly empty frame. Heights are
      // evened out so a shelf reads as one row.
      const reach = p.cols > 0 ? p.cols * node.maxW + (p.cols - 1) * GAP + 2 * PAD : p.w
      const sw = ctx.narrow
        ? p.w + (spare * p.w) / natural
        : Math.min(p.w + (spare * p.w) / natural, Math.max(p.w, reach))
      place(p, sx, cy, sw, shelfH, ctx, out)
      sx += sw + SUB_GAP
    }
    cy += shelfH
  }

  if (plan.more) {
    cy += cy > y + HEAD ? GAP : 0
    out.push({
      kind: 'more',
      key: `more:${plan.group.key}`,
      box: { x: x + PAD, y: cy, w: inner, h: MORE_H },
      variant: 'row',
      target: plan.group.key,
      open: plan.more.open,
      hidden: plan.more.hidden,
    })
  }
}

function makeCtx(input: LayoutInput, narrow: boolean): Ctx {
  const { tree } = input
  return {
    node: nodeSize(input.density, input.counting),
    solo: tree.top.length === 1 ? tree.top[0].key : undefined,
    expanded: input.expanded,
    filtering: tree.filtering,
    narrow,
    hint: !tree.grouped && !tree.filtering ? OTHER_KEY : undefined,
    cache: new Map(),
  }
}

/* -------------------------------------------------------------------------- */
/* The tree                                                                    */
/* -------------------------------------------------------------------------- */

function layoutTree(input: LayoutInput): Layout {
  const { tree, width: A } = input
  const ctx = makeCtx(input, false)
  const items: Item[] = []
  const links: Link[] = []

  const router: Box = { x: (A - input.router.w) / 2, y: 0, w: input.router.w, h: input.router.h }
  const rowY = router.h + DROP
  const top = tree.top

  if (tree.nothing && top.length === 0) {
    items.push(nothingNote(A, rowY))
    return finish(input, false, router, items, links)
  }

  // Every container the same width, on one grid. The column count is what
  // fits at the density's container width, and never more than there are
  // containers — so two groups are two columns, centred, not two cards pinned
  // to the left of a three-column grid.
  const colMin = containerWidth(ctx)
  const fitIn = (w: number) => Math.max(1, Math.min(MAX_COLS, Math.floor((w + ROW_GAP) / (colMin + ROW_GAP))))
  // More than one row is drawn inside an outline, which needs its padding
  // inside the map's width rather than hanging off both sides of it.
  const wall = top.length > fitIn(A)
  const room = wall ? A - 2 * FRAME_PAD : A
  const fit = fitIn(room)
  const cols = Math.min(fit, Math.max(1, top.length))
  const colW = (Math.min(room, fit * COL_MAX + (fit - 1) * ROW_GAP) - (fit - 1) * ROW_GAP) / fit
  const rows = chunk(top, cols)

  // Past SHOW_ALL devices, rows beyond the first fold into "+N more groups"
  // until opened; below it, everything is shown.
  const everyone = top.reduce((t, g) => t + g.total, 0)
  const openTop = input.expanded.has(TOP_KEY) || tree.filtering
  const shownRows = everyone > SHOW_ALL && !openTop && rows.length > 1 ? rows.slice(0, 1) : rows
  const hidden = rows.slice(shownRows.length).flat()

  const gridW = cols * colW + (cols - 1) * ROW_GAP
  const x0 = (A - gridW) / 2
  let y = rowY + (wall ? FRAME_PAD : 0)
  const heaviest = Math.max(0, ...top.map((g) => g.weight))

  for (const row of shownRows) {
    const plans = row.map((g) => gridPlan(g, colW, ctx))
    // One height per row, the tallest: the grid reads as rows of equal cards.
    const rh = Math.max(...plans.map((p) => p.h))
    // A short last row is centred under the grid rather than left-aligned,
    // the way a single row is centred under the router.
    let x = x0 + ((cols - row.length) * (colW + ROW_GAP)) / 2
    for (const p of plans) {
      place(p, x, y, colW, rh, ctx, items)
      if (!wall) {
        links.push(
          link(p.group.key, router, { x, y, w: colW, h: rh }, edgeWidth(p.group.weight, heaviest, input.rated), {
            dashed: tree.grouped && p.group.key === OTHER_KEY,
          }),
        )
      }
      x += colW + ROW_GAP
    }
    y += rh + ROW_GAP
  }

  if (hidden.length > 0 || (openTop && everyone > SHOW_ALL && rows.length > 1)) {
    const box = { x: x0, y, w: gridW, h: MORE_H }
    items.push({
      kind: 'more',
      key: 'more: top',
      box,
      variant: 'row',
      target: TOP_KEY,
      open: openTop,
      hidden: { devices: 0, groups: hidden.filter((g) => g.key !== OTHER_KEY).length },
      others: hidden.some((g) => g.key === OTHER_KEY),
      devices: hidden.reduce((t, g) => t + g.total, 0),
    })
    y += MORE_H + ROW_GAP
  }

  let frame: Box | undefined
  if (wall) {
    // More than one row: one outline round the grid and one link to it. A
    // line to a second row would have to pass between the cards of the first.
    frame = { x: x0 - FRAME_PAD, y: rowY, w: gridW + 2 * FRAME_PAD, h: y - ROW_GAP + FRAME_PAD - rowY }
    const load = top.reduce((t, g) => t + Math.max(0, g.weight), 0)
    links.push(link('frame', router, frame, edgeWidth(load, load, input.rated)))
  }

  const out = finish(input, false, router, items, links, !wall && hidden.length === 0 && !anyFold(items))
  out.frame = frame
  return out
}

/** Past this many devices, rows of groups after the first start folded. */
const SHOW_ALL = 80
const FRAME_PAD = 16
/** Containers per row at most, and how wide one may grow. */
const MAX_COLS = 4
const COL_MAX = 400

/** The narrowest a container may be: two columns of nodes and its padding. */
function containerWidth(ctx: Ctx) {
  return 2 * ctx.node.w + GAP + 2 * PAD
}

function anyFold(items: Item[]) {
  return items.some((i) => i.kind === 'more' && !i.open)
}

/**
 * A container at a given width: its devices in two columns (one, if the width
 * cannot take two), then each subgroup as a full-width box of its own, then
 * the fold. Nothing is chosen by score — every container is laid out the same
 * way, which is what lets a row of them line up.
 */
function gridPlan(g: MapGroup, w: number, ctx: Ctx): Plan {
  const { node } = ctx
  const inner = w - 2 * PAD
  const userOpen = ctx.expanded.has(g.key)
  const open = ctx.filtering || userOpen
  const nd = g.devices.length
  const ns = g.groups.length
  const n = nd + ns
  const k = node.k

  // Subgroups first: folding a device hides one thing, a subgroup a branch.
  const shownN = open || n <= k ? n : Math.min(k, n - MIN_FOLD)
  const sg = Math.min(ns, shownN)
  const sd = shownN - sg
  const hidden = { devices: nd - sd, groups: ns - sg }
  const folded = hidden.devices + hidden.groups > 0
  const more = folded ? { hidden, open: false } : userOpen && n > k ? { hidden, open: true } : undefined

  const cols = sd === 0 ? 0 : inner >= 2 * node.minW + GAP || inner >= 2 * node.w + GAP ? 2 : 1
  const subs = g.groups.slice(0, sg).map((sub) => gridPlan(sub, inner, ctx))

  const note = g.key === ctx.hint ? 'hint' : n === 0 ? 'empty' : undefined
  const parts = [
    note ? (note === 'hint' ? hintHeight(ctx.narrow) : EMPTY_H) : 0,
    cols > 0 ? gridSpan(Math.ceil(sd / cols), node.h, GAP) : 0,
    ...subs.map((p) => p.h),
  ].filter((p) => p > 0)
  let bodyH = parts.reduce((t, p) => t + p, 0) + Math.max(0, parts.length - 1) * SUB_GAP
  if (more) bodyH += (bodyH > 0 ? GAP : 0) + MORE_H

  return {
    group: g,
    w,
    h: HEAD + bodyH + PAD,
    cost: 0,
    cols,
    devices: g.devices.slice(0, sd),
    shelves: subs.map((p) => [p]),
    more,
    note,
    open,
  }
}

/* -------------------------------------------------------------------------- */
/* Focus                                                                       */
/* -------------------------------------------------------------------------- */

/**
 * One group, looked at closely.
 *
 * The focused group moves to the centre directly under the router, and what it
 * holds fans out beneath it as separate nodes, each on its own line whose
 * thickness is that child's traffic — the router's-eye view, one level down.
 * The other top-level groups shrink to chips either side of it, still hanging
 * off the router by thin lines, so the rest of the network is one click away
 * rather than gone.
 *
 * A focused subgroup is reached through its parents: the top-level group sits
 * under the router as a chip, each rung below it another chip, and the focused
 * group last — a breadcrumb drawn in the same direction as everything else.
 *
 * The fan obeys the same rule as a container, with one row standing in for K:
 * as many as fit side by side get lines, and the rest wait behind "+N more",
 * which opens into rows below without lines, for the same reason as the
 * router's own overflow.
 */
function layoutFocus(input: LayoutInput, focus: string): Layout {
  const { tree, width: A } = input
  const node = nodeSize(input.density, input.counting)
  const items: Item[] = []
  const links: Link[] = []
  const chain = chainTo(tree, focus)
  const head = chain[0]
  const focused = chain[chain.length - 1]

  const router: Box = { x: (A - input.router.w) / 2, y: 0, w: input.router.w, h: input.router.h }
  const rowY = router.h + DROP
  const heaviestTop = Math.max(0, ...tree.top.map((g) => g.weight))

  // The row under the router: the head of the chain, centred, and the others
  // set aside as chips — busiest nearest, alternating left and right.
  const centerW = chain.length === 1 ? FOCUS_W : CHIP_W
  const centerH = chain.length === 1 ? FOCUS_H : CHIP_H
  const centerX = (A - centerW) / 2
  const others = tree.top.filter((g) => g.key !== head.key)
  const perSide = Math.max(0, Math.floor(((A - centerW) / 2 - SIDE_GAP + CHIP_GAP) / (CHIP_W + CHIP_GAP)))
  const slots = perSide * 2
  const shown = others.length > slots ? others.slice(0, Math.max(0, slots - 1)) : others
  const picked = others.length > slots ? others.slice(Math.max(0, slots - 1)) : []

  const chipY = rowY + (centerH - CHIP_H) / 2
  const leftX = (j: number) => centerX - SIDE_GAP - (j + 1) * CHIP_W - j * CHIP_GAP
  const rightX = (j: number) => centerX + centerW + SIDE_GAP + j * (CHIP_W + CHIP_GAP)
  let left = 0
  let right = 0
  shown.forEach((g, i) => {
    // Alternate while both sides have room; once one side is full, the other
    // takes the rest.
    const goLeft = (i % 2 === 0 && left < perSide) || right >= perSide
    const x = goLeft ? leftX(left++) : rightX(right++)
    const box = { x, y: chipY, w: CHIP_W, h: CHIP_H }
    items.push({ kind: 'group', key: g.key, group: g, box, variant: 'chip', open: false })
    links.push(link(g.key, router, box, EDGE_MIN, { faint: true, dashed: g.key === OTHER_KEY }))
  })
  if (picked.length > 0) {
    const box = { x: rightX(right), y: chipY, w: CHIP_W, h: CHIP_H }
    items.push({ kind: 'pick', key: 'pick: side', box, groups: picked })
    links.push(link('pick: side', router, box, EDGE_MIN, { faint: true, dashed: true }))
  }

  // The chain, top to bottom.
  let y = rowY
  let above: Box = router
  let aboveWeight = heaviestTop
  chain.forEach((g, i) => {
    const last = i === chain.length - 1
    const w = last ? FOCUS_W : CHIP_W
    const h = last ? FOCUS_H : CHIP_H
    const box = { x: (A - w) / 2, y, w, h }
    items.push({ kind: 'group', key: g.key, group: g, box, variant: last ? 'focused' : 'chip', open: false })
    links.push(link(g.key, above, box, edgeWidth(g.weight, aboveWeight, input.rated), { dashed: g.key === OTHER_KEY }))
    above = box
    aboveWeight = Math.max(g.weight, 0)
    y += h + CHAIN_GAP
  })

  // The fan.
  const fanY = above.y + above.h + DROP
  const children: ({ group: MapGroup } | { device: DeviceRow })[] = [
    ...focused.groups.map((group) => ({ group })),
    ...focused.devices.map((device) => ({ device })),
  ]
  const weightOf = (c: (typeof children)[number]) => ('group' in c ? c.group.weight : (input.tree.weights.get(`d:${c.device.mac}`) ?? 0))

  if (children.length === 0) {
    items.push({
      kind: 'note',
      key: `note:${focused.key}`,
      box: { x: (A - 320) / 2, y: fanY - DROP / 2, w: 320, h: EMPTY_H },
      note: 'empty',
    })
    return finish(input, false, router, items, links)
  }

  const perRow = Math.max(1, Math.floor((A + FAN_GAP) / (node.w + FAN_GAP)))
  const limit = Math.min(node.k, perRow)
  // Past the limit, the last place goes to "+N more" — which then always
  // hides at least two.
  const first = children.length <= limit ? children : children.slice(0, limit - 1)
  const rest = children.slice(first.length)
  const count = first.length + (rest.length > 0 ? 1 : 0)
  const rowW = count * node.w + (count - 1) * FAN_GAP
  const heaviest = Math.max(0, ...first.map(weightOf))
  let fx = (A - rowW) / 2
  for (const c of first) {
    const box = { x: fx, y: fanY, w: node.w, h: node.h }
    if ('group' in c) {
      items.push({ kind: 'group', key: c.group.key, group: c.group, box, variant: 'card', open: false })
      links.push(link(c.group.key, above, box, edgeWidth(c.group.weight, heaviest, input.rated)))
    } else {
      const key = `d:${c.device.mac}`
      items.push({ kind: 'device', key, device: c.device, box })
      links.push(link(key, above, box, edgeWidth(weightOf(c), heaviest, input.rated)))
    }
    fx += node.w + FAN_GAP
  }

  if (rest.length > 0) {
    const target = fanKey(focused.key)
    const open = input.expanded.has(target) || tree.filtering
    const box = { x: fx, y: fanY, w: node.w, h: node.h }
    items.push({
      kind: 'more',
      key: `more:${target}`,
      box,
      variant: 'card',
      target,
      open,
      hidden: {
        devices: rest.filter((c) => 'device' in c).length,
        groups: rest.filter((c) => 'group' in c).length,
      },
    })
    links.push(link(`more:${target}`, above, box, EDGE_MIN, { dashed: true }))

    if (open) {
      let ry = fanY + node.h + OVERFLOW_GAP
      for (const row of chunk(rest, perRow)) {
        const w = row.length * node.w + (row.length - 1) * FAN_GAP
        let rx = (A - w) / 2
        for (const c of row) {
          const box = { x: rx, y: ry, w: node.w, h: node.h }
          if ('group' in c) items.push({ kind: 'group', key: c.group.key, group: c.group, box, variant: 'card', open: false })
          else items.push({ kind: 'device', key: `d:${c.device.mac}`, device: c.device, box })
          rx += node.w + FAN_GAP
        }
        ry += node.h + GAP + 4
      }
    }
  }

  return finish(input, false, router, items, links)
}

/* -------------------------------------------------------------------------- */
/* Narrow                                                                      */
/* -------------------------------------------------------------------------- */

/**
 * The phone layout: no branching at all. The router sits top left, the
 * containers stack full-width beneath it, and one rail runs down their left
 * edge with a dot level with each header. At this width a branch would have
 * nowhere to go but sideways.
 *
 * Focus does not exist here — there is no room to set anything aside — so the
 * header opens the container's fold in place instead.
 */
function layoutNarrow(input: LayoutInput): Layout {
  const { tree, width: A } = input
  const ctx = makeCtx(input, true)
  const items: Item[] = []
  const w = Math.min(input.router.w, A)
  const router: Box = { x: 0, y: 0, w, h: input.router.h }
  let y = router.h + 24
  const W = A - RAIL_INSET
  const dots: Rail['dots'] = []

  if (tree.nothing && tree.top.length === 0) {
    items.push({ kind: 'note', key: 'note: nothing', box: { x: RAIL_INSET, y, w: W, h: EMPTY_H }, note: 'nothing' })
    return finish(input, true, router, items, [])
  }

  const k = ctx.node.k
  const open = input.expanded.has(TOP_KEY) || tree.filtering
  const fold = !open && tree.top.length - k >= MIN_FOLD
  const shown = fold ? tree.top.slice(0, k) : tree.top
  for (const g of shown) {
    const plan = narrowPlan(g, W, ctx)
    place(plan, RAIL_INSET, y, W, 0, ctx, items)
    dots.push({ key: g.key, y: y + HEAD / 2 })
    y += plan.h + 12
  }
  if (fold || (open && tree.top.length - k >= MIN_FOLD && !tree.filtering)) {
    const hidden = tree.top.slice(k)
    items.push({
      kind: 'more',
      key: 'more: top',
      box: { x: RAIL_INSET, y, w: W, h: MORE_H + 8 },
      variant: 'row',
      target: TOP_KEY,
      open,
      hidden: { devices: 0, groups: hidden.length },
    })
    dots.push({ key: 'more: top', y: y + (MORE_H + 8) / 2 })
  }

  const rail =
    dots.length > 0 ? { x: RAIL_X, y0: router.h, y1: dots[dots.length - 1].y, dots } : undefined
  return { ...finish(input, true, router, items, []), rail }
}

/* -------------------------------------------------------------------------- */
/* Pieces                                                                      */
/* -------------------------------------------------------------------------- */

function nothingNote(A: number, y: number): Item {
  const w = Math.min(360, A)
  return { kind: 'note', key: 'note: nothing', box: { x: (A - w) / 2, y, w, h: EMPTY_H }, note: 'nothing' }
}

/**
 * A line from the bottom of one box to the top-centre of another.
 *
 * Lines leave from along the parent's bottom edge, shifted toward where they
 * are heading, rather than all from one point: from one point the thick ones
 * lie on top of each other for their first inch.
 */
function link(key: string, from: Box, to: Box, width: number, opts: { dashed?: boolean; faint?: boolean } = {}): Link {
  const px = from.x + from.w / 2
  const cx = to.x + to.w / 2
  const reach = Math.max(0, from.w / 2 - 16)
  const x0 = px + Math.max(-reach, Math.min(reach, (cx - px) * 0.15))
  // A dashed line is drawn no thicker than a hair over the thin one: thick
  // dashes read as a row of beads, not as a line with a weight.
  const w = opts.dashed ? Math.min(width, 2) : width
  return { key: `l:${key}`, x0, y0: from.y + from.h, x1: cx, y1: to.y, width: w, ...opts }
}

/**
 * Thickness from a share of the heaviest sibling's traffic, square-rooted so a
 * group doing a tenth of the work still reads as a line and not a hair. With
 * no rate every line is the thin one, which is the honest drawing of "we do
 * not know".
 */
function edgeWidth(weight: number, heaviest: number, rated: boolean) {
  if (!rated || heaviest <= 0) return EDGE_MIN
  return EDGE_MIN + (EDGE_MAX - EDGE_MIN) * Math.sqrt(Math.max(0, weight) / heaviest)
}

function finish(
  input: LayoutInput,
  narrow: boolean,
  router: Box,
  items: Item[],
  links: Link[],
  fits = true,
): Layout {
  const bottom = Math.max(router.y + router.h, ...items.map((i) => i.box.y + i.box.h))
  return { width: input.width, height: Math.ceil(bottom) + 2, narrow, fits, router, items, links }
}


