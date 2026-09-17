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
 * How IPv6 is served on this pool's interface. off serves no IPv6; slaac advertises the prefix and answers DHCPv6 information requests; stateful additionally hands out addresses over DHCPv6. Empty means off.
 */
export type RouterAdvertisementMode = '' | 'off' | 'slaac' | 'stateful'
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
export type IPAddress5 = string
/**
 * Which provider hosts the zone this name lives in. cloudflare takes an API token. alidns is Alibaba Cloud DNS and tencentcloud is Tencent Cloud DNSPod; both take a key ID and a secret. callback is the generic form: olr requests a URL you supply with the address substituted into it, which is how the DynDNS-style endpoints — No-IP, DuckDNS, Dynu — are reached. The list grows by request rather than by completeness; a name outside it is refused rather than guessed at.
 */
export type DNSProvider = 'alidns' | 'callback' | 'cloudflare' | 'tencentcloud'
/**
 * interface reads the address off an uplink this router owns, which is right when olr terminates the WAN — PPPoE, or DHCP from the ISP. reflector asks an HTTPS endpoint what address the internet sees, which is right when olr sits behind a modem, and is the only form that can tell you your ISP has put you behind carrier-grade NAT. There is no default: olr will not choose between talking to a third party and not.
 */
export type WhereTheAddressComesFrom = 'interface' | 'reflector'
/**
 * A duration with a unit, such as 5m, 30s or 1m30s. Units are ns, us, ms, s, m and h.
 */
export type Duration1 = string
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
 * An IPv4 or IPv6 address, such as 192.168.1.1 or 2001:db8::1.
 */
export type IPAddress6 = string
/**
 * An address and prefix length in CIDR form, such as 192.168.1.0/24.
 */
export type IPPrefix1 = string
/**
 * What a blocked name answers with. nxdomain says the name does not exist, which is the honest answer and the one clients cache and back off from. zero answers 0.0.0.0 and ::, which some networks prefer because an app that reads NXDOMAIN as "the network is down" will retry forever, where a refused connection fails at once. Empty means nxdomain.
 */
export type BlockedNameResponse = '' | 'nxdomain' | 'zero'
/**
 * Which kind of traffic this forward carries. tcp covers web, SSH and most services. udp covers games, voice and VPNs. both carries each, as two rules sharing one counter. Empty means tcp.
 */
export type Protocol = '' | 'tcp' | 'udp' | 'both'
/**
 * A port, such as 8080, or an inclusive range, such as 30000-30010. A range is forwarded to the identical range inside; only a single port may be remapped to a different one.
 */
export type PortOrPortRange = string
export type AddressAndPort2 = string
/**
 * How this exit delivers traffic. interface sends it out a device — a WireGuard or Tailscale interface, a PPPoE session, a proxy's TUN. next_hop hands it to another box on the network, such as the modem or a machine running a proxy. blocked refuses it, so applications fail immediately and visibly rather than hanging.
 */
export type ExitForm = 'interface' | 'next_hop' | 'blocked'
export type IPAddress7 = string
/**
 * What happens to IPv6 traffic from sources assigned to this exit. via carries it through the exit, which needs the exit to actually have IPv6. block refuses it, so clients fall back to IPv4 immediately. direct lets it take the box's normal path, which leaks every site with an AAAA record around the exit. Empty means block.
 */
export type IPv6Handling = '' | 'via' | 'block' | 'direct'
/**
 * What happens to assigned traffic when the health check fails. block stops it, so the problem is visible and diagnosable. direct sends it out the box's normal path instead, which silently leaks exactly the traffic that was meant to be routed. Empty means block.
 */
export type BehaviourWhenTheExitIsDown = '' | 'block' | 'direct'
export type AddressAndPort3 = string
/**
 * A duration with a unit, such as 30s, 5s or 1m30s. Units are ns, us, ms, s, m and h.
 */
export type Duration2 = string
/**
 * Who hosts the DNS for your domain. olr writes a temporary record through their API to prove the domain is yours, which is the only way to get a certificate for a name that does not resolve from the internet. The names your proxy supports are listed by `olr ingress show providers`.
 */
export type DNSProvider1 = string
/**
 * An API credential for the DNS provider. Scope it to this one zone if the provider allows it: it is stored on the router and can change your DNS. It is never shown again after it is set.
 */
export type ProviderAPIToken = string
/**
 * Optional. Used by the certificate authority to warn you before a certificate expires.
 */
export type ContactAddress = string
/**
 * Public DNS servers used only to confirm the challenge record has published. This must not be this router: olr answers your local domain authoritatively, so asking it would return "no such record" forever. Leave empty for sensible defaults.
 */
export type PropagationCheckResolvers = string[]
/**
 * How olr speaks to the service being published. http is almost always right: the connection runs over your own LAN to a device you named, and the HTTPS a browser sees is terminated here. Use https only when the service refuses plain HTTP — many NAS and hypervisor UIs do — in which case its own certificate is not checked, because those are self-signed and demanding a valid one would make the case this option exists for impossible. Empty means http.
 */
export type UpstreamScheme = '' | 'http' | 'https'
export type IPPrefix2 = string
export type IPAddress8 = string
export type IPPrefix3 = string
export type IPAddress9 = string
export type IPAddress10 = string
/**
 * `home` sends only your home networks, so the device reaches the things on them while everything else keeps going out of whatever network the device is actually on. `everything` sends all of the device's traffic here, so it appears to be at home for every purpose including its public address — at the cost of every byte crossing your home upload twice, and of needing address translation olr does not yet write. Empty means home.
 */
export type WhatTheDeviceSendsThroughTheTunnel = '' | 'home' | 'everything'
/**
 * The `2022-blake3-` ciphers are the current design and the right choice unless a device is too old to speak it. Prefer the chacha20 one on hardware with no AES instructions — an older ARM board — where it is several times faster. The other two are the previous generation, kept only for a client that cannot manage the newer ones. Changing this regenerates the password, because a 2022 cipher's password is a fixed-length key rather than a passphrase; every device then needs a new link. Empty means 2022-blake3-aes-128-gcm.
 */
export type HowProxyTrafficIsEncrypted =
  | ''
  | '2022-blake3-aes-128-gcm'
  | '2022-blake3-aes-256-gcm'
  | '2022-blake3-chacha20-poly1305'
  | 'aes-256-gcm'
  | 'chacha20-ietf-poly1305'

/**
 * The whole box's configuration, one property per module — the shape of /etc/open-linux-router/olr.json.
 */
export interface OlrDocument {
  devices?: DevicesConfig
  dhcp?: DhcpConfig
  dial?: DialConfig
  dns?: DnsConfig
  firewall?: FirewallConfig
  gateway?: GatewayConfig
  ingress?: IngressConfig
  link?: LinkConfig
  remote?: RemoteConfig
  system?: SystemConfig
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
  group: string
  ipv4?: PoolIPv4
  ipv6?: PoolIPv6
  lease_time?: Duration
  gateway?: IPAddress2
  dns?: IPAddress3[]
  domain?: string
  ntp?: IPAddress4[]
  options?: Option[]
}
export interface PoolIPv4 {
  start?: IPAddress
  end?: IPAddress1
}
export interface PoolIPv6 {
  mode?: RouterAdvertisementMode
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
export interface DialConfig {
  records?: Record[]
}
export interface Record {
  name: string
  zone?: string
  provider: DNSProvider
  provider_key_id?: string
  provider_token?: string
  callback_url?: string
  source: WhereTheAddressComesFrom
  interface?: string
  reflector_url?: string
  interval?: Duration1
  ttl?: number
}
export interface DnsConfig {
  enabled: boolean
  listen?: AddressAndPort[]
  allow_from?: IPPrefix[]
  upstream: Upstream
  local_domain?: string
  hosts?: Host[]
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
export interface Host {
  name: string
  addresses: IPAddress6[]
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
export interface FirewallConfig {
  enabled: boolean
  forwards?: Forward[]
}
export interface Forward {
  name: string
  in: string
  protocol?: Protocol
  port: PortOrPortRange
  to: AddressAndPort2
  hairpin?: boolean
  slot: number
}
export interface GatewayConfig {
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
  next_hop?: IPAddress7
  dev?: string
}
export interface Probe {
  target: AddressAndPort3
  interval?: Duration2
  timeout?: Duration2
  failures?: number
  successes?: number
}
export interface Assignment {
  interface: string
  exit?: string
}
export interface IngressConfig {
  enabled: boolean
  certificate: Certificate
  services?: Service[]
  raw_caddyfile?: string
}
/**
 * How the one wildcard certificate that serves every published name is obtained.
 */
export interface Certificate {
  provider?: DNSProvider1
  provider_token?: ProviderAPIToken
  acme_email?: ContactAddress
  resolvers?: PropagationCheckResolvers
}
export interface Service {
  name: string
  upstream: UpstreamIngress
}
export interface UpstreamIngress {
  device?: string
  host?: string
  port: number
  scheme?: UpstreamScheme
}
export interface LinkConfig {
  adopted?: string[]
  groups?: Group[]
}
export interface Group {
  name: string
  members: string[]
  ipv4?: GroupIPv4
}
export interface GroupIPv4 {
  subnet: IPPrefix2
  router?: IPAddress8
}
export interface RemoteConfig {
  endpoint?: string
  wireguard: WireGuard
  shadowsocks: Shadowsocks
}
export interface WireGuard {
  enabled: boolean
  interface?: string
  listen_port?: number
  public_port?: number
  subnet?: IPPrefix3
  address?: IPAddress9
  /**
   * Generated by olr and never shown. It is redacted on every surface; sending the mask back means 'unchanged'.
   */
  private_key?: string
  peers?: Peer[]
  raw_wireguard_conf?: string
}
export interface Peer {
  name: string
  public_key: string
  address?: IPAddress10
  routes?: WhatTheDeviceSendsThroughTheTunnel
}
export interface Shadowsocks {
  enabled: boolean
  listen_port?: number
  public_port?: number
  cipher?: HowProxyTrafficIsEncrypted
  /**
   * Generated by olr. Redacted on config surfaces; the client route hands it over deliberately. Sending the mask back means 'unchanged'.
   */
  password?: string
  udp?: boolean
  raw_shadowsocks_conf?: string
}
export interface SystemConfig {
  access?: Access
}
export interface Access {
  /**
   * When this box was claimed.
   */
  claimed_at: string
  password: Password
}
/**
 * The password required over the network
 */
export interface Password {
  /**
   * Derived form of the password. Never the password.
   */
  hash: string
}
