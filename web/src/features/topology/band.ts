import type { Item, Layout, LayoutInput } from './layout'

/**
 * The band above the router, and the two primitives every line and every box on
 * the map is measured with.
 *
 * It is a pass of its own rather than a part of each layout because it is the
 * same band on all of them: the wide map centres it on the router, the narrow
 * one starts it at the left edge where the router is, and every layout below it
 * only has to be moved down by its height. Nothing here reads the device tree.
 *
 * **Everything outside the router is in it.** Observed main-table next hops,
 * and the devices dialed in over WireGuard: both are boxes on the far side of
 * this router, and the band is the only place a box can join the router with a
 * line of its own. A card hung under the router beside a group, or on a row of
 * its own between them, would need the lines to those groups to run through it
 * or squeeze past it. Gateway policy exits stay off the map: an exit this box
 * chooses among is not a thing outside it.
 *
 * Each box in it is directly connected to this router: a next hop because that
 * is where traffic leaves, a dialed-in device because that is where it arrives.
 */

export const EXIT_H = 52
const HOP_W = 240
const BAND_ROW_GAP = 28
const EXIT_DROP = 48

/** Line thickness, from a share of the heaviest sibling's traffic. */
export const EDGE_MIN = 1.5
export const EDGE_MAX = 5

export interface Box {
  x: number
  y: number
  w: number
  h: number
}

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
  /** For the line from a next hop that is not answering. */
  fault?: boolean
}

export function chunk<T>(list: T[], size: number): T[][] {
  const out: T[][] = []
  for (let i = 0; i < list.length; i += size) out.push(list.slice(i, i + size))
  return out
}

/**
 * A line from the bottom of one box to the top-centre of another.
 *
 * Lines leave from along the parent's bottom edge, shifted toward where they
 * are heading, rather than all from one point: from one point the thick ones
 * lie on top of each other for their first inch.
 */
export function link(key: string, from: Box, to: Box, width: number, opts: { dashed?: boolean; faint?: boolean; fault?: boolean } = {}): Link {
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
export function edgeWidth(weight: number, heaviest: number, rated: boolean) {
  if (!rated || heaviest <= 0) return EDGE_MIN
  return EDGE_MIN + (EDGE_MAX - EDGE_MIN) * Math.sqrt(Math.max(0, weight) / heaviest)
}

/** One box in the band over the router, before it has a place. */
type BandNode = { kind: 'hop' | 'dialin'; key: string; w: number; h: number; down: boolean }

/**
 * Everything above the router, in rows, and everything else moved down to make
 * room. Done after the rest is laid out rather than inside each layout, because
 * it is the same band on every one of them.
 *
 * Only a next hop that is not answering is marked: a peer that is asleep, or
 * has never connected, is not a fault of the network, and its card says which
 * of the four states it is in.
 */
export function withBand(below: Layout, input: LayoutInput): Layout {
  const A = input.width
  const gap = below.narrow ? 12 : 40

  const boxes: Omit<BandNode, 'w' | 'h'>[] = []
  for (const hop of input.outside?.hops ?? []) boxes.push({ kind: 'hop', key: `o:${hop.key}`, down: hop.down })
  for (const peer of input.dialIn?.peers ?? []) boxes.push({ kind: 'dialin', key: `p:${peer.key}`, down: false })

  const rows: BandNode[][] = []
  if (boxes.length) {
    const perRow = Math.max(1, Math.floor((A + gap) / (HOP_W + gap)))
    for (const row of chunk(boxes, perRow)) {
      const w = Math.min(HOP_W, (A - (row.length - 1) * gap) / row.length)
      rows.push(row.map((node) => ({ ...node, w, h: EXIT_H })))
    }
  }
  if (rows.length === 0) return below

  const heights = rows.map((row) => Math.max(...row.map((n) => n.h)))
  const band = heights.reduce((a, b) => a + b, 0) + (rows.length - 1) * BAND_ROW_GAP + EXIT_DROP

  const down = (b: Box): Box => ({ ...b, y: b.y + band })
  const router = down(below.router)
  const centre = router.x + router.w / 2
  const items: Item[] = []
  const links: Link[] = []

  // Each row is centred on the router, with one direct line per box in it.
  const placed: { node: BandNode; box: Box }[][] = []
  let y = 0
  rows.forEach((row, r) => {
    const rowW = row.reduce((sum, n) => sum + n.w, 0) + (row.length - 1) * gap
    const wanted = centre - rowW / 2
    const x0 = below.narrow ? 0 : Math.max(0, Math.min(A - rowW, wanted))
    let x = x0
    placed.push(
      row.map((node) => {
        const box = { x, y, w: node.w, h: node.h }
        x += node.w + gap
        return { node, box }
      }),
    )
    y += heights[r] + BAND_ROW_GAP
  })

  for (const row of placed) {
    for (const { node, box } of row) {
      items.push(node.kind === 'hop'
        ? { kind: 'hop', key: node.key, box }
        : { kind: 'dialin', key: node.key, box })
    }
  }

  for (const row of placed) {
    for (const { node, box } of row) {
      links.push(link(node.key, box, router, edgeWidth(1, 1, input.rated), { fault: node.down }))
    }
  }

  for (const item of below.items) items.push({ ...item, box: down(item.box) } as Item)
  for (const l of below.links) links.push({ ...l, y0: l.y0 + band, y1: l.y1 + band })
  const rail = below.rail && {
    ...below.rail,
    y0: below.rail.y0 + band,
    y1: below.rail.y1 + band,
    dots: below.rail.dots.map((d) => ({ ...d, y: d.y + band })),
  }
  return { ...below, height: below.height + band, router, items, links, rail }
}
