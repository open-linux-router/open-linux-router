// Code generated from olrd's published JSON Schema. DO NOT EDIT.
//
// Regenerate with `make types` (olrd must be running).
// Source of truth: the Go config structs — see design.md §3.2 rule 3.

/**
 * What kind of device this is. It selects the picture shown in the device list, and an operator-set value always beats a detected one. Empty means nothing has been set, so detection may answer; "unknown" means the device was looked at and could not be placed.
 */
export type DeviceCategory =
  | ''
  | 'unknown'
  | 'phone'
  | 'tablet'
  | 'laptop'
  | 'desktop'
  | 'watch'
  | 'ereader'
  | 'tv'
  | 'speaker'
  | 'console'
  | 'camera'
  | 'doorbell'
  | 'thermostat'
  | 'sensor'
  | 'plug'
  | 'light'
  | 'vacuum'
  | 'printer'
  | 'nas'
  | 'server'
  | 'sbc'
  | 'router'
  | 'accesspoint'
  | 'switch'
  | 'hub'
export type IPAddress = string
export type IPAddress1 = string
/**
 * A duration written the way an operator would say it: a number and a unit, optionally repeated. Units are s, m, h, d and w. A bare number means seconds, matching dnsmasq.
 */
export type Duration = string
export type IPAddress2 = string
/**
 * An IPv4 or IPv6 address, such as 192.168.1.1 or 2001:db8::1.
 */
export type IPAddress3 = string
/**
 * An IPv4 or IPv6 address, such as 192.168.1.1 or 2001:db8::1.
 */
export type IPAddress4 = string
/**
 * How IPv6 is served on this pool's interface. off serves no IPv6; slaac advertises the prefix and answers DHCPv6 information requests; stateful additionally hands out addresses over DHCPv6. Empty means off.
 */
export type RouterAdvertisementMode = '' | 'off' | 'slaac' | 'stateful'
export type IPAddress5 = string
/**
 * An address and port, such as 192.168.1.1:53 or [2001:db8::1]:53.
 */
export type AddressAndPort = string
/**
 * An address and prefix length in CIDR form, such as 192.168.1.0/24.
 */
export type IPPrefix = string
/**
 * How names are resolved. recurse walks the DNS from the root, so no third party sees everything this network looks up and there is no forwarder to be down. forward sends every query to the servers listed instead, which is faster from cold and the only option where an upstream's own filtering is wanted. Empty means recurse.
 */
export type UpstreamMode = '' | 'recurse' | 'forward'
/**
 * An address and port, such as 192.168.1.1:53 or [2001:db8::1]:53.
 */
export type AddressAndPort1 = string
/**
 * An address and prefix length in CIDR form, such as 192.168.1.0/24.
 */
export type IPPrefix1 = string
/**
 * What a blocked name answers with. nxdomain says the name does not exist, which is the honest answer and the one clients cache and back off from. zero answers 0.0.0.0 and ::, which some networks prefer because an app that reads NXDOMAIN as "the network is down" will retry forever, where a refused connection fails at once. Empty means nxdomain.
 */
export type BlockedNameResponse = '' | 'nxdomain' | 'zero'
/**
 * How this exit delivers traffic. interface sends it out a device — a WireGuard or Tailscale interface, a PPPoE session, a proxy's TUN. next_hop hands it to another box on the network, such as the modem or a machine running a proxy. blocked refuses it, so applications fail immediately and visibly rather than hanging.
 */
export type ExitForm = 'interface' | 'next_hop' | 'blocked'
export type IPAddress6 = string
/**
 * What happens to IPv6 traffic from sources assigned to this exit. via carries it through the exit, which needs the exit to actually have IPv6. block refuses it, so clients fall back to IPv4 immediately. direct lets it take the box's normal path, which leaks every site with an AAAA record around the exit. Empty means block.
 */
export type IPv6Handling = '' | 'via' | 'block' | 'direct'
/**
 * What happens to assigned traffic when the health check fails. block stops it, so the problem is visible and diagnosable. direct sends it out the box's normal path instead, which silently leaks exactly the traffic that was meant to be routed. Empty means block.
 */
export type BehaviourWhenTheExitIsDown = '' | 'block' | 'direct'
export type AddressAndPort2 = string
/**
 * A duration with a unit, such as 30s, 5s or 1m30s. Units are ns, us, ms, s, m and h.
 */
export type Duration1 = string

/**
 * The whole box's configuration, one property per module — the shape of /etc/open-linux-router/olr.json.
 */
export interface OlrDocument {
  devices?: DevicesConfig
  dhcp?: DhcpConfig
  dns?: DnsConfig
  routing?: RoutingConfig
}
export interface DevicesConfig {
  devices?: Device[]
}
export interface Device {
  mac: string
  name?: string
  category?: DeviceCategory
  model?: string
  notes?: string
}
export interface DhcpConfig {
  enabled: boolean
  pools?: Pool[]
  reservations?: Reservation[]
  extra_dnsmasq_conf?: string
}
export interface Pool {
  interface: string
  start: IPAddress
  end: IPAddress1
  lease_time?: Duration
  gateway?: IPAddress2
  dns?: IPAddress3[]
  domain?: string
  ntp?: IPAddress4[]
  ra?: RouterAdvertisementMode
  options?: Option[]
}
export interface Option {
  option: string
  value: string
}
export interface Reservation {
  mac: string
  ip: IPAddress5
  hostname?: string
  lease_time?: Duration
}
export interface DnsConfig {
  enabled: boolean
  listen?: AddressAndPort[]
  allow_from?: IPPrefix[]
  upstream: Upstream
  policies?: Policy[]
  hijack: Hijack
  query_log: QueryLog
  extra_unbound_conf?: string
}
export interface Upstream {
  mode?: UpstreamMode
  servers?: AddressAndPort1[]
  tls?: boolean
  tls_name?: string
}
export interface Policy {
  name: string
  clients?: IPPrefix1[]
  block?: string[]
  allow?: string[]
  response?: BlockedNameResponse
}
export interface Hijack {
  enabled: boolean
  interfaces?: string[]
  block_dot?: boolean
}
export interface QueryLog {
  enabled: boolean
  entries?: number
}
export interface RoutingConfig {
  enabled: boolean
  exits?: Exit[]
  default?: string
  stats?: boolean
  interfaces?: Assignment[]
}
export interface Exit {
  name: string
  via: Via
  slot: number
  ipv6?: IPv6Handling
  on_failure?: BehaviourWhenTheExitIsDown
  snat?: boolean
  probe?: Probe
}
export interface Via {
  kind: ExitForm
  interface?: string
  next_hop?: IPAddress6
  dev?: string
}
export interface Probe {
  target: AddressAndPort2
  interval?: Duration1
  timeout?: Duration1
  failures?: number
  successes?: number
}
export interface Assignment {
  interface: string
  exit?: string
}
