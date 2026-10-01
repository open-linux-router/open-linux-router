import type { WayKey } from '@/features/remote/way-marks'
import type {
  DeviceRow,
  DialStatus,
  GeoPlace,
  GeoStatus,
  ProxyStatus,
  RemoteClients,
  RemoteStatus,
  SocksStatus,
} from '@/lib/api-types'

/**
 * What is above the router: the internet, whatever sits between it and this
 * box, and the people reaching in from outside.
 *
 * **Whether there is something in between is read from the router's own
 * address on its way out.** A public address means this box *is* the edge and
 * the next hop is the provider's. A carrier-grade NAT address (100.64/10) means
 * the provider is translating, which is also the provider's. A private address
 * means another router is doing that — usually a modem or a router of the
 * operator's own, and then it earns a node of its own, with the address this
 * box reaches it on. That last reading is a default, not a fact: some
 * providers hand out private addresses directly, and nothing on this box can
 * tell the two apart.
 *
 * **The public address is only shown when it is known**: sitting on this box's
 * own interface, or read by a dynamic DNS record that asks the outside. Nothing
 * here goes and looks it up; a router that told a third party its address on
 * a timer because a page wanted a number would be doing something its owner
 * did not ask for.
 */
export interface Outside {
  /** The internet node. */
  internet: {
    /** The address the world sees, when something on the box already knows it. */
    publicAddress?: string
    /** Where that came from, for the tooltip. */
    publicFrom?: 'interface' | 'ddns'
    /** This box's way-out address is inside the provider's NAT. */
    cgnat: boolean
    /** There is no default route at all. */
    noRoute: boolean
  }
  /**
   * The router in between, when this box's way out is a private address. It
   * is drawn as one node with the internet: the public address is that
   * router's, not this one's, so two nodes stacked one on the other were two
   * halves of one box.
   */
  upstream?: {
    /** The next hop. Absent on a point-to-point link, which has none. */
    via?: string
    dev: string
    /** This box's own address on that link. */
    address?: string
    /** The next hop was asked and has not answered. */
    down: boolean
    /**
     * The device that holds the next hop's address, when the device list has
     * one — the modem the operator already named. Then the node is that
     * device, not an anonymous "upstream router".
     */
    device?: DeviceRow
  }
}

export function buildOutside(dial?: DialStatus, devices?: DeviceRow[]): Outside | undefined {
  if (!dial) return undefined
  const route = dial.route
  const out: Outside = { internet: { cgnat: false, noRoute: !route } }
  if (!route) return out

  const address = route.addresses?.[0]?.split('/')[0]
  const kind = address ? classify(address) : undefined
  if (kind === 'public') {
    out.internet.publicAddress = address
    out.internet.publicFrom = 'interface'
  } else if (kind === 'cgnat') {
    out.internet.cgnat = true
  } else {
    const via = route.via
    out.upstream = {
      via,
      dev: route.dev,
      address,
      down: route.gateway_state === 'silent',
      device: via ? devices?.find((d) => d.fixed_ip === via || d.ips?.includes(via)) : undefined,
    }
  }

  if (!out.internet.publicAddress) {
    // A record that asks the outside is the only thing on the box that knows
    // the address past a NAT. One that reads the interface reports what is
    // already above, and under a NAT that is the private address.
    const seen = dial.records.find((r) => r.address && classify(r.address) === 'public')
    if (seen) {
      out.internet.publicAddress = seen.address
      out.internet.publicFrom = 'ddns'
    }
  }
  return out
}

type AddressKind = 'public' | 'cgnat' | 'private'

/** IPv4 only, which is what a way out's address is here. */
export function classify(address: string): AddressKind {
  const p = address.split('.').map(Number)
  if (p.length !== 4 || p.some((n) => !Number.isInteger(n) || n < 0 || n > 255)) return 'public'
  const [a, b] = p
  if (a === 100 && b >= 64 && b < 128) return 'cgnat'
  if (a === 10 || (a === 172 && b >= 16 && b < 32) || (a === 192 && b === 168) || (a === 169 && b === 254)) {
    return 'private'
  }
  return 'public'
}

/**
 * The people reaching in, one node apiece — only the ones connected now.
 *
 * A tunnel device is a device: it has its own key, so it has a name. A proxy
 * client is an address: everybody shares one password, and the most the box
 * can say is who has connections open and where that address is. Either way
 * it is someone arriving at the router, drawn like every other node on the
 * map, and the way it came in is its second line.
 *
 * Nobody connected draws nothing. The map is about who is here; that a way
 * in is switched on, and whether it is running, is the Remote access page's.
 */
export interface RemoteClient {
  key: string
  /** The way in: "WireGuard", "Shadowsocks", "SOCKS5". */
  via: string
  /**
   * The same way in, as a key rather than a word — what picks the mark the
   * node wears. Kept beside the label rather than derived from it, because a
   * label is prose and gets edited: "SOCKS5" was "SOCKS" once, and the mark
   * lookup would have gone quietly blank with it.
   */
  way: WayKey
  /** A device's name, or an address. */
  who: string
  /** `who` is an address rather than a name. */
  address?: boolean
  /** Where it is, already worded. */
  where?: string
}

export interface Remote {
  clients: RemoteClient[]
  /** Present when any client carries a place: the attribution goes with it. */
  locations?: GeoStatus
}

export function buildRemote({
  tunnel,
  shadowsocks,
  socks,
  clients,
}: {
  tunnel?: RemoteStatus
  shadowsocks?: ProxyStatus
  socks?: SocksStatus
  clients?: RemoteClients
}): Remote | undefined {
  const places = clients?.places ?? {}
  const out: RemoteClient[] = []
  let placed = false
  const where = (addr?: string) => {
    const p = addr ? places[addr] : undefined
    if (p && !p.local) placed = true
    return p ? describePlace(p) : undefined
  }

  if (tunnel?.enabled) {
    for (const p of tunnel.peers.filter((p) => p.online)) {
      const addr = p.endpoint ? hostOf(p.endpoint) : undefined
      out.push({
        key: `wg:${p.name}`,
        via: 'WireGuard',
        way: 'wireguard',
        who: p.name,
        where: where(addr) ?? addr,
      })
    }
  }

  const proxies: [string, WayKey, ProxyStatus | SocksStatus | undefined][] = [
    ['Shadowsocks', 'shadowsocks', shadowsocks],
    ['SOCKS5', 'socks5', socks],
  ]
  for (const [label, name, status] of proxies) {
    if (!status?.enabled || (status.service && !status.service.active)) continue
    for (const c of clients?.proxies.find((p) => p.proxy === name)?.clients ?? []) {
      out.push({
        key: `${name}:${c.address}`,
        via: label,
        way: name,
        who: c.address,
        address: true,
        where: where(c.address),
      })
    }
  }

  if (out.length === 0) return undefined
  return { clients: out, locations: placed ? clients?.locations : undefined }
}

/** "China Telecom · CN". The network owner leads: it is what someone recognises as theirs. */
export function describePlace(p: GeoPlace): string | undefined {
  if (p.local) return 'local network'
  const org = p.org ? shortOrg(p.org) : undefined
  return [org, p.country].filter(Boolean).join(' · ') || undefined
}

/**
 * The network owner, without the registry's legal suffixes: "Google LLC" reads
 * as Google, and "China Mobile Communications Group Co., Ltd." does not fit a
 * line.
 */
export function shortOrg(org: string): string {
  let out = org.replace(/,?\s+(co\.?,?\s*)?(ltd|llc|inc|corp|corporation|limited|gmbh|s\.?a\.?|b\.?v\.?|plc|ag)\.?$/i, '')
  // Then the generic words registries pile on after the name, one at a time,
  // never down to nothing: "Comcast Cable Communications" is Comcast.
  for (;;) {
    const next = out.replace(/\s+(group|holdings?|communications?|telecommunications?|company|corporation|cable)$/i, '')
    if (next === out || !next.trim()) break
    out = next
  }
  return out.trim()
}

function hostOf(endpoint: string): string {
  // "[2001:db8::1]:51820" or "203.0.113.7:51820".
  const v6 = endpoint.match(/^\[(.+)\]:\d+$/)
  if (v6) return v6[1]
  const i = endpoint.lastIndexOf(':')
  return i > 0 ? endpoint.slice(0, i) : endpoint
}
