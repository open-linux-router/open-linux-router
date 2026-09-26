import { effectiveParents } from '@/features/devices/group-tree'
import { magnitude, sumFlows, type Flow, type TrafficView } from '@/features/topology/traffic'
import type { DeviceRow } from '@/lib/api-types'
import type { DevicesGroup } from '@/lib/config-types'

/** The network filter's value for devices on none of this router's networks. */
export const NO_NETWORK = ' none'

/**
 * The key of the display-only bucket for devices in no group. It cannot
 * collide with a group: names are trimmed on the server, so none starts with a
 * space.
 */
export const OTHER_KEY = ' other'

export const groupKey = (name: string) => `g:${name}`
export const deviceKey = (mac: string) => `d:${mac}`

/**
 * One group as the map draws it: what it holds after the filters, in the
 * order it is drawn, and what it holds before them, for the counts.
 */
export interface MapGroup {
  key: string
  /** The group's name, or undefined for the bucket. */
  name?: string
  title: string
  /** The parent's key, or undefined at the top. */
  parentKey?: string
  /** 0 at the top. */
  depth: number
  /** Devices directly in it that survived the filters, busiest first. */
  devices: DeviceRow[]
  /** Groups directly inside it that survived the filters, busiest first. */
  groups: MapGroup[]
  /** Devices directly in it, before the filters — what a delete would move. */
  directCount: number
  /** Groups directly inside it, before the filters. */
  subgroupCount: number
  /** Every device at any depth below it, before the filters. */
  total: number
  /** …and how many of those are here now. */
  online: number
  /** Traffic of every device at any depth below it. */
  flow?: Flow
  /** Its live traffic as one number: what links are drawn and it is ordered by. */
  weight: number
}

export interface MapTree {
  top: MapGroup[]
  byKey: Map<string, MapGroup>
  /** Whether the operator has made any groups at all. */
  grouped: boolean
  filtering: boolean
  /** Filtering, and nothing matched anywhere. */
  nothing: boolean
  /** Every group's and device's live weight, to freeze the order with. */
  weights: Map<string, number>
}

/**
 * The operator's groups as a tree, with the filters applied and everything in
 * drawing order.
 *
 * **Busiest first, at every level.** The map folds whatever does not fit into
 * "+N more", and the fold has to hide the least interesting things, which on a
 * page about what the network is doing are the quiet ones. By rate when there
 * is one, else by bytes moved, then by name so equal weights do not shuffle.
 *
 * That order changes every time the counters are read, and a node that moves
 * under the pointer is a node nobody can click — the reason the first version
 * of this map sorted by name and gave up on traffic. `frozen` is the answer
 * to both: while the pointer is over the map, the caller passes the weights as
 * they were when it arrived, so nothing re-sorts until it leaves. Rates on
 * screen keep updating; only the order holds still.
 *
 * **Filtering hides, where it used to empty.** A search used to leave every
 * group drawn with "Nothing here matches" inside, which was fine for three
 * cards and is not for twelve containers of which two hold the answer. A group
 * survives a filter when anything below it does, or when its own name is
 * what was typed. Under the filters every group is also shown whole — a hit
 * folded into "+N more" is a search that looks like it found nothing.
 */
export function buildTree({
  devices: all,
  groups,
  traffic,
  filter,
  network,
  frozen,
}: {
  devices: DeviceRow[]
  groups: DevicesGroup[]
  traffic: TrafficView
  filter: string
  network: string
  frozen?: Map<string, number> | null
}): MapTree {
  const q = filter.trim().toLowerCase()
  const filtering = q !== '' || network !== ''
  const matches = (d: DeviceRow) =>
    (!network || (network === NO_NETWORK ? !d.network : d.network === network)) &&
    (!q ||
      [d.name, d.mac, d.hostname ?? '', d.vendor ?? '', d.network ?? '', d.group ?? '', ...(d.ips ?? [])]
        .join(' ')
        .toLowerCase()
        .includes(q))

  // Live weights are recorded for the caller to freeze; the sort key is the
  // frozen one when there is a snapshot, and something that arrived after it
  // sorts after everything that was already there.
  const weights = new Map<string, number>()
  const sortKey = (key: string, live: number) => {
    weights.set(key, live)
    return frozen ? (frozen.get(key) ?? -1) : live
  }

  // Sort keys are computed once per item, not once per comparison.
  const rankDevices = (list: DeviceRow[]) => {
    const w = new Map(
      list.map((d) => [d.mac, sortKey(deviceKey(d.mac), magnitude(traffic.flowOf(d), traffic.rated))]),
    )
    return list.sort(
      (a, b) =>
        w.get(b.mac)! - w.get(a.mac)! ||
        Number(b.online) - Number(a.online) ||
        (a.name || a.mac).localeCompare(b.name || b.mac),
    )
  }
  const rankGroups = (list: MapGroup[]) => {
    const w = new Map(list.map((g) => [g.key, sortKey(g.key, g.weight)]))
    return list.sort((a, b) => w.get(b.key)! - w.get(a.key)! || a.title.localeCompare(b.title))
  }

  const parents = effectiveParents(groups)
  const known = new Set(groups.map((g) => g.name))
  const membersOf = new Map<string, DeviceRow[]>()
  const loose: DeviceRow[] = []
  for (const d of all) {
    // A device naming a group that no longer exists cannot happen through
    // the API, but a hand-edited olr.json can say anything, and such a device
    // is better shown in the bucket than not shown.
    if (d.group && known.has(d.group)) membersOf.set(d.group, [...(membersOf.get(d.group) ?? []), d])
    else loose.push(d)
  }
  const childrenOf = new Map<string, string[]>()
  for (const g of groups) {
    const p = parents.get(g.name) ?? ''
    childrenOf.set(p, [...(childrenOf.get(p) ?? []), g.name])
  }

  const byKey = new Map<string, MapGroup>()

  const build = (
    key: string,
    name: string | undefined,
    title: string,
    members: DeviceRow[],
    subNames: string[],
    depth: number,
    parentKey: string | undefined,
  ): MapGroup => {
    const subs = subNames.map((n) =>
      build(groupKey(n), n, n, membersOf.get(n) ?? [], childrenOf.get(n) ?? [], depth + 1, key),
    )
    const everyone = [...members]
    const flows = members.map((d) => traffic.flowOf(d))
    let total = members.length
    let online = members.filter((d) => d.online).length
    for (const s of subs) {
      total += s.total
      online += s.online
      flows.push(s.flow)
    }
    const flow = sumFlows(flows)
    const shown = members.filter(matches)
    const node: MapGroup = {
      key,
      name,
      title,
      parentKey,
      depth,
      devices: rankDevices(shown),
      groups: [],
      directCount: everyone.length,
      subgroupCount: subNames.length,
      total,
      online,
      flow,
      weight: magnitude(flow, traffic.rated),
    }
    // Subgroups that the filters emptied are dropped here, after they have
    // counted toward this group's totals: "12 devices" should not change
    // because the operator typed something.
    node.groups = rankGroups(subs.filter(survives))
    if (survives(node)) byKey.set(key, node)
    return node
  }
  const survives = (g: MapGroup) =>
    !filtering ||
    g.devices.length > 0 ||
    g.groups.length > 0 ||
    (q !== '' && g.name !== undefined && g.name.toLowerCase().includes(q))

  const grouped = groups.length > 0
  const top = rankGroups(
    (childrenOf.get('') ?? [])
      .map((n) => build(groupKey(n), n, n, membersOf.get(n) ?? [], childrenOf.get(n) ?? [], 0, undefined))
      .filter(survives),
  )

  // The bucket is last whatever its traffic: it is not a group, and a
  // display-only card jumping ahead of the operator's own would make it look
  // like one. With no groups at all it is the whole map, so it is always there.
  if (loose.length > 0 || !grouped) {
    const bucket = build(OTHER_KEY, undefined, grouped ? 'Other devices' : 'All devices', loose, [], 0, undefined)
    // With no groups, the bucket stays even when the filters empty it: it is
    // the only container, and the map keeps its shape around "nothing
    // matches" rather than collapsing to the router alone.
    if (survives(bucket) || !grouped) {
      byKey.set(OTHER_KEY, bucket)
      top.push(bucket)
    }
  }

  const nothing = filtering && top.every((g) => g.devices.length === 0 && g.groups.length === 0)
  return { top, byKey, grouped, filtering, nothing, weights }
}

/** The chain from the top down to a group, both included. */
export function chainTo(tree: MapTree, key: string): MapGroup[] {
  const out: MapGroup[] = []
  for (let g = tree.byKey.get(key); g; g = g.parentKey ? tree.byKey.get(g.parentKey) : undefined) {
    out.unshift(g)
    if (out.length > 8) break
  }
  return out
}
