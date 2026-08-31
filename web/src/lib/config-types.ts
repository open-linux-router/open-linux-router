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
