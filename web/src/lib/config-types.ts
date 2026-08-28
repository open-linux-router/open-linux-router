// Code generated from olrd's published JSON Schema. DO NOT EDIT.
//
// Regenerate with `make types` (olrd must be running).
// Source of truth: the Go config structs — see design.md §3.2 rule 3.

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

export interface DhcpConfig {
  $schema?: string
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
