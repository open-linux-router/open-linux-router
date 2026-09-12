// Response shapes for the read and apply surfaces.
//
// KNOWN GAP, worth fixing rather than living with: these are hand-written,
// while config-types.ts is generated. design.md §3.2 rule 3 makes the Go struct
// the single source for every surface, and §6.2 says the read surface includes
// observed resources "declared alongside config" — but core reflects only the
// module's *config* struct today, so the view types in internal/dhcp/view.go
// reach TypeScript by being retyped here. A field renamed in Go breaks this
// file silently at runtime rather than loudly at compile time.
//
// The fix is to have core publish response schemas next to the config schema
// and extend scripts/gen-types.mjs to consume them. Until then, this file and
// internal/dhcp/view.go, internal/devices/view.go and internal/dns/view.go must
// be changed together — three copies now, not two.
//
// What closing it actually costs, recorded here so it is not re-derived:
//   - The view structs are unexported in all three modules (planView, queryView,
//     …), so core needs them exported, or a registration point beside the config
//     value Mount already takes.
//   - Impact is an int with MarshalText, so reflection publishes a bare `string`
//     and the unions below would *regress* unless Impact, ChangeKind and
//     ServiceAction each grow a JSONSchema() method — the move
//     internal/dns/schema.go already makes for UpstreamMode.
//   - `Plan` collides across modules in one flat generated file, so the
//     generator needs a per-module namespace it does not have today.

import type { Problem } from '@/lib/api'
import type {
  DeviceCategory,
  IngressConfig,
  UpstreamScheme,
  DevicesConfig,
  ExitForm,
  FirewallConfig,
  LinkConfig,
  Protocol,
  GatewayConfig,
} from '@/lib/config-types'

/** What applying a change will cost (design.md §5.3.3, internal/dhcp Impact). */
export type Impact = 'none' | 'reload' | 'restart' | 'disruptive'

/** What the backend needs after the files are written. */
export type ServiceAction = 'none' | 'start' | 'stop' | 'reload' | 'restart'

export type ChangeKind = 'create' | 'update' | 'delete'

export interface Change {
  path: string
  kind: ChangeKind
  impact: Impact
  /**
   * Which backend reads this file. Only modules driving more than one daemon
   * set it — dns writes unbound.conf and the relay's config, and signalling the
   * wrong one is the bug this field exists to prevent.
   */
  unit?: string
  /**
   * Whether this file's contents are withheld. Set by ingress alone, on the file
   * holding the DNS provider credential: the change is reported and compared
   * byte for byte, and only its *display* is suppressed. A diff viewer must not
   * decide on its own what is safe to render — `diff` already contains a
   * placeholder rather than the bytes, so nothing here has to remember.
   */
  secret?: boolean
  diff: string
}

/**
 * What every module's plan carries.
 *
 * Split out because the tail is not shared: a module with one daemon reports a
 * single `action`, and one with two reports a list. Flattening them into a
 * single `Plan` is how a dns page ends up reading `plan.action`, getting
 * `undefined`, and reporting a bare "Applied" for a change that restarted the
 * resolver.
 */
export interface PlanCore {
  backend: string
  changes: Change[]
  impact: Impact
  reasons?: string[]
  empty: boolean
  warnings?: Problem[]
}

/**
 * The answer to "what would applying this do?" for a module with one backend —
 * internal/dhcp planView, and internal/devices, whose view.go says it mirrors
 * dhcp's shape deliberately so one renderer serves both.
 */
export interface Plan extends PlanCore {
  action: ServiceAction
  /**
   * The boot-time state the unit will be moved to, absent when it already
   * matches. Separate from `action` because "running now" and "running after a
   * reboot" are different promises: a unit that is active but not enabled looks
   * healthy until the power goes out.
   */
  enable?: boolean
}

/** One unit's worth of pending work — internal/dns ServicePlan. */
export interface ServicePlan {
  unit: string
  action: ServiceAction
  /** As {@link Plan.enable}, but per unit. */
  enable?: boolean
}

/**
 * internal/dns planView.
 *
 * `services` rather than `action` because this module drives two daemons:
 * unbound resolves and olr-dnsd owns :53. They fail differently and are
 * signalled independently — editing a blocklist reloads the relay and must not
 * touch the resolver at all — so a single action could not say what happened.
 */
export interface DnsPlan extends PlanCore {
  services: ServicePlan[]
}

/** One unit of work and how it went. */
export interface Step {
  description: string
  done: boolean
  error?: string
  /**
   * The step could not be attempted rather than having failed — ingress checking
   * a rendered config with no proxy binary to check it with. `done` stays true
   * because nothing is outstanding, so a reader that only looks at `done` is not
   * misled about the outcome, only about whether a check happened.
   */
  skipped?: boolean
}

/**
 * The result of an apply. Carries the steps whether it succeeded or not:
 * design.md §5.3.2 has no rollback, so a half-finished change stays
 * half-finished and the honest thing is to show which steps landed.
 */
export interface ApplyResult {
  plan: Plan
  steps?: Step[]
  error?: { message: string; problems?: Problem[] }
}

/** What systemd knows about a unit — core.UnitStatus. */
export interface UnitStatus {
  unit: string
  active: boolean
  /** Whether it starts at boot — not the same question as `active`. */
  enabled: boolean
  /**
   * Whether the unit file exists at all. Distinguished from `enabled` because
   * the two need different answers: disabled is something olr fixes on the next
   * apply, missing means the package is incomplete and applying will not help.
   */
  installed: boolean
  state: string
  sub_state?: string
  since?: string
  main_pid?: number
}

export interface DhcpStatus {
  enabled: boolean
  service?: UnitStatus
  service_error?: string
  drifted: boolean
  drift?: Plan
  drift_error?: string
  as_of: string
}

export interface Lease {
  ip: string
  mac?: string
  iaid?: string
  hostname?: string
  client_id?: string
  /** null for a lease that never expires. */
  expires: string | null
  active: boolean
}

export interface PoolUsage {
  interface: string
  size: number
  active: number
  expired: number
  free: number
  percent: number
}

export interface DhcpLeases {
  leases: Lease[]
  usage: PoolUsage[]
  problems?: Problem[]
  as_of: string
}

// --- link ------------------------------------------------------------------

/**
 * One row of the interface list (internal/link interfaceView).
 *
 * The observed half — addresses, state — is read from the kernel on every
 * request and never stored, so this is what is true now rather than what was
 * last written (design.md §4.5).
 */
export interface InterfaceRow {
  name: string

  /** The operator has handed this interface to olr. */
  adopted: boolean

  /**
   * The kernel currently has it. False with `adopted` true is the typo case —
   * a name that was stored and has nothing behind it — and the UI has to say
   * so rather than showing a row that looks like any other.
   */
  present: boolean

  /**
   * `up` is administrative state, `running` is carrier. Both, because "up with
   * no cable in it" is the most common reason a freshly configured DHCP server
   * appears to do nothing, and one boolean cannot say it.
   */
  up: boolean
  running: boolean

  loopback: boolean
  mac?: string

  /** Every address on the interface, link-local excluded. */
  prefixes?: string[]

  /**
   * The first IPv4 prefix split into the two halves an operator reads
   * separately. Absent when the interface has no IPv4 address — which is
   * exactly when no address range can be served on it.
   */
  address?: string
  subnet?: string

  /**
   * A range inside `subnet` excluding the network, broadcast and this
   * interface's own address. A hint for prefilling a form; dhcp validates any
   * range independently, so this cannot become a second opinion.
   */
  suggested_start?: string
  suggested_end?: string
}

export interface InterfaceList {
  interfaces: InterfaceRow[]
  problems?: Problem[]
  as_of: string
}

/**
 * The result of storing adoption.
 *
 * No `steps`, like devices: one atomic document write, nothing half-finished
 * to report. The impact is always `none` — adopting an interface changes
 * nothing on the box by itself.
 */
export interface LinkApplyResult {
  plan: Plan
  config: LinkConfig
  error?: { message: string; problems?: Problem[] }
}

// --- devices ---------------------------------------------------------------

/**
 * Where a resolved value came from — internal/devices Origin.
 *
 * The distinction is the point of the screen: what an operator was told and
 * what we inferred must never look the same, or a guess reads as a fact.
 */
export type Origin = 'operator' | 'detected' | 'observed' | ''

/** Which source saw a device — internal/devices Source. */
export type PresenceSource = 'dhcp-lease' | 'arp'

/**
 * One row of the device list: identity joined to presence
 * (design.md §4.4, internal/devices deviceView).
 */
export interface DeviceRow {
  mac: string

  /** Already resolved: stored name, else observed hostname, else empty. */
  name: string
  name_origin?: Origin

  category: DeviceCategory
  category_origin?: Origin

  /** What detection produced, whether or not it won. */
  detected_category?: DeviceCategory
  detect_reason?: string
  vendor?: string

  model?: string
  notes?: string

  /** Whether a human has described this device, as opposed to it merely being seen. */
  stored: boolean

  /** Any source considers it current. */
  online: boolean

  /**
   * Any source has ever seen it. Distinct from `online`: a stored device that
   * has never been seen is a typo'd MAC or a machine that has not been plugged
   * in, and neither is the same as "away".
   */
  seen: boolean

  ips?: string[]
  hostname?: string
  sources?: PresenceSource[]

  /** null for no lease, and for a lease that never expires. */
  expires: string | null

  /** The reserved address. Owned by dhcp, joined here so no client repeats it. */
  fixed_ip?: string

  /**
   * Which of this router's networks the device is on, and how we know.
   *
   * `observed` is the neighbour table reporting the interface the device
   * answered on; `detected` is it having been placed by which pool range its
   * address falls in. Absent means neither could answer — a stored device
   * nothing has seen, or one whose address sits outside every range — and the
   * map has to say so rather than file it under a plausible network.
   */
  network?: string
  network_origin?: Origin
}

export interface DeviceList {
  devices: DeviceRow[]
  problems?: Problem[]
  as_of: string
}

/**
 * The result of storing identity.
 *
 * No `steps`, unlike dhcp's ApplyResult: this module writes one document
 * atomically, so there is no half-finished state to report.
 */
export interface DevicesApplyResult {
  plan: Plan
  config: DevicesConfig
  error?: { message: string; problems?: Problem[] }
}

// --- dns -------------------------------------------------------------------
//
// Mirrors internal/dns/view.go. Everything below `DnsStatus.stats` is observed:
// never stored, never cached, read through the relay's socket on every request
// and stamped with `as_of` (design.md §4.5).

/** As {@link ApplyResult}, for the module whose plan carries two units. */
export interface DnsApplyResult {
  plan: DnsPlan
  steps?: Step[]
  error?: { message: string; problems?: Problem[] }
}

/**
 * One backend's liveness — internal/dns serviceView.
 *
 * `status` is absent with `error` set when the query itself failed, which is
 * normal on a box with no system bus and must not read as "stopped".
 */
export interface DnsService {
  unit: string
  status?: UnitStatus
  error?: string
}

export interface DnsStatus {
  enabled: boolean
  /**
   * Both backends, always resolver first then relay. Reported separately on
   * purpose: "DNS is broken" has two very different causes and only one of them
   * is ours.
   */
  services: DnsService[]
  drifted: boolean
  drift?: DnsPlan
  drift_error?: string
  /** Absent with `stats_error` set when the relay is not answering — which is
   *  itself the most useful thing the reply can say. */
  stats?: DnsStats
  stats_error?: string
  as_of: string
}

/** One address the relay has recently answered — internal/dns clientView. */
export interface DnsClient {
  address: string
  queries: number
  last_seen: string
}

/**
 * The relay's account of itself, gaps included — internal/dns statsView.
 *
 * `dropped` and `unparsed` are published rather than kept internal, and that is
 * the point: a query log that silently shed entries under load would be worse
 * than none, because it would look complete.
 */
export interface DnsStats {
  /** When the relay started. The log does not survive a restart. */
  since: string
  queries: number
  blocked: number
  refused: number
  failed: number
  /** Observations lost because the tee was full. */
  dropped: number
  /** Responses the observer could not read. */
  unparsed: number
  /** Entries the log currently holds, and the bound it holds them under. */
  held: number
  capacity: number
  clients?: DnsClient[]
}

/** One answered query — internal/dns queryView. */
export interface QueryRow {
  at: string
  client: string
  name: string
  type: string
  rcode: string
  /**
   * `blocked` and `policy` together answer "why can this device not reach that
   * site". A blocked entry without the rule that blocked it sends the operator
   * hunting.
   */
  blocked: boolean
  policy?: string
  answers?: string[]
  /** The CNAME chain. Its tail is why a device that asked for one name shows up
   *  talking to a CDN. */
  chain?: string[]
}

export interface DnsQueries {
  queries: QueryRow[]
  stats?: DnsStats
  as_of: string
}

/** One domain→address pairing — internal/dns nameView. */
export interface NameRow {
  client: string
  name: string
  address: string
  chain?: string[]
  expires: string
  last_seen: string
}

export interface DnsNames {
  names: NameRow[]
  stats?: DnsStats
  as_of: string
}

// --- gateway ---------------------------------------------------------------

/**
 * A change to the kernel's gateway state — internal/gateway changeView.
 *
 * A line rather than a file path and a diff, because this module configures the
 * kernel rather than a backend's config file. The text is the same canonical
 * form `olr gateway show --dry-run` prints and the same one stored in each
 * nftables rule's comment, so what the screen shows, what the CLI shows and
 * what `nft list ruleset` shows are one string.
 */
export interface GatewayChange {
  kind: 'add' | 'remove'
  line: string
}

/**
 * Somebody else's `ip rule` — internal/gateway ForeignRule.
 *
 * Reported rather than hidden (design.md §3.4): a hand-rolled setup that is
 * visible is one an operator can reason about, and a second owner of the
 * gateway table is something they have to go and resolve elsewhere.
 */
export interface ForeignRule {
  priority: number
  family: string
  table: number
  selector: string
  has_default: boolean
}

/** What applying a gateway change would do — internal/gateway planView. */
export interface GatewayPlan {
  changes: GatewayChange[]
  impact: Impact
  foreign?: ForeignRule[]
  reasons?: string[]
  /**
   * Set when the change cannot proceed at all. A string rather than a boolean,
   * because the operator has to find another program's configuration file and
   * "blocked: true" does not say where to look.
   */
  blocked?: string
  empty: boolean
  /**
   * Whether the kernel could be read. Without it a client cannot tell "nothing
   * to do" from "we could not look", which need different words on screen.
   */
  known: boolean
  diff?: string
  warnings?: Problem[]
}

export interface GatewayApplyResult {
  plan: GatewayPlan
  steps?: Step[]
  config: GatewayConfig
  error?: { message: string; problems?: Problem[] }
}

/** One exit and what is true of it right now — internal/gateway exitStatusView. */
export interface ExitStatus {
  name: string
  via: ExitForm

  /**
   * `up` and `probed` together: an exit nobody probes reads as up, and saying
   * so without saying it was never checked would claim knowledge we do not have
   * (design.md §5.6 — faults must not hide inside a default).
   */
  up: boolean
  probed: boolean

  used_by?: string[]

  /** The kernel resources it holds, published so they can be planned around. */
  mark: string
  table: number
  priority: number
}

/**
 * One network's effective exit and where it came from — internal/gateway
 * assignmentStatusView.
 *
 * The source is what makes inheritance usable: an effective value with no
 * visible origin is one nobody can reason about, which is the whole argument
 * for a property rather than an ordered rule list.
 */
export interface AssignmentStatus {
  interface: string
  exit: string
  source: 'default' | 'interface'
  reason?: string
}

export interface GatewayStatus {
  enabled: boolean
  known: boolean
  exits: ExitStatus[]
  assignments: AssignmentStatus[]
  drifted: boolean
  foreign?: ForeignRule[]
  problems?: Problem[]
  as_of: string
}

/** One device's traffic through one way out — internal/gateway usageView. */
export interface Usage {
  address: string

  /**
   * Empty for the residual — traffic no assignment matched. A row rather than
   * an omission: per-exit totals only reconcile against the box total if what
   * matched nothing is visible too.
   */
  exit: string

  /** Traffic still carrying the mark of an exit that has since been removed. */
  unknown?: boolean

  up_bytes: number
  down_bytes: number
  up_packets: number
  down_packets: number
}

/** internal/gateway trafficView. */
export interface GatewayTraffic {
  /** Intent. `counting` is whether the kernel actually has the table. */
  enabled: boolean
  counting: boolean

  usage: Usage[]

  /**
   * How full the accounting is. A full table stops recording devices it has
   * not seen before while going on counting the ones it has, so `held` nearing
   * `capacity` means rows are probably missing — not that nothing is using the
   * network.
   */
  held: number
  capacity: number

  /**
   * What these numbers cannot see, from the server rather than written into
   * this app — so the CLI and any agent reading the endpoint get the same
   * caveats. Every one explains a number being smaller than expected.
   */
  limits?: string[]

  as_of: string
}

// --- firewall --------------------------------------------------------------

/**
 * A change to the kernel's NAT table — internal/firewall changeView.
 *
 * A line rather than a file path and a diff, for the same reason gateway's is:
 * this module configures the kernel rather than a backend's config file. The
 * text is the same canonical form `olr firewall show --dry-run` prints and the
 * same one stored in each nftables rule's comment, so what the screen shows,
 * what the CLI shows and what `nft list table inet olr_nat` shows are one
 * string.
 */
export interface FirewallChangeLine {
  kind: 'add' | 'remove'
  line: string
}

/**
 * Somebody else's chain on the forward hook — internal/firewall ForeignFilter.
 *
 * Reported rather than hidden (design.md §3.4), and — unlike gateway's
 * ForeignRule — it never blocks the change. In nftables a drop is final, so olr
 * cannot override one; but the foreign chain may also be accepting exactly this
 * traffic in a rule olr cannot evaluate, so refusing would block a legitimate
 * setup on a guess (docs/firewall.md §5.2).
 */
export interface ForeignFilter {
  table: string
  family: string
  chain: string
  policy: string
}

/** What applying a firewall change would do — internal/firewall planView. */
export interface FirewallPlan {
  changes: FirewallChangeLine[]
  impact: Impact
  foreign?: ForeignFilter[]
  reasons?: string[]
  empty: boolean
  /**
   * Whether the kernel could be read. Without it a client cannot tell "nothing
   * to do" from "we could not look", which need different words on screen.
   */
  known: boolean
  diff?: string
  warnings?: Problem[]
}

export interface FirewallApplyResult {
  plan: FirewallPlan
  steps?: Step[]
  config: FirewallConfig
  error?: { message: string; problems?: Problem[] }
}

/** One forward and what is true of it right now — internal/firewall forwardStatusView. */
export interface ForwardStatus {
  name: string
  in: string
  protocol: Protocol
  port: string
  to: string
  hairpin: boolean

  /**
   * `counted` and the totals together. A forward nobody could count reads as
   * zero, and saying so without saying it was never measured would claim
   * knowledge we do not have (design.md §5.6 — faults must not hide inside a
   * default). The two states also point in opposite directions when a port does
   * not work: "nothing arrived" points outward at the ISP or somebody else's
   * filter, "not counted" points at this box.
   */
  counted: boolean

  /**
   * Since the table was last built, not since boot: any change to this module
   * rebuilds it and resets these (docs/firewall.md §3.6).
   */
  packets: number
  bytes: number
}

export interface FirewallStatus {
  enabled: boolean
  known: boolean
  forwards: ForwardStatus[]
  drifted: boolean
  foreign?: ForeignFilter[]
  problems?: Problem[]
  as_of: string
}

// --- ingress --------------------------------------------------------------

/**
 * What the ingress plan carries. The same shape as `Plan` — one backend, files
 * on disk, a unit to signal — so the shared renderer in features/plan serves it
 * unchanged.
 */
export type IngressPlan = Plan

/**
 * One published service, joined with where it currently points.
 *
 * `upstream` is not in the config: the config names a device, and where that
 * device is belongs to another module (design.md §4.1). It is read per request,
 * which is why it can be absent with `upstream_error` set while the service
 * itself is perfectly well configured.
 */
export interface IngressServiceView {
  name: string
  /** Rendered by the daemon rather than assembled here, so the client does not
   *  have to fetch the local domain to know what a service's address is. */
  url: string
  device?: string
  host?: string
  port: number
  scheme: UpstreamScheme
  upstream?: string
  upstream_error?: string
}

export interface IngressServices {
  services: IngressServiceView[]
  domain?: string
  as_of: string
}

/** The certificate this module's whole clock-dependent half hangs off. */
export interface IngressCertState {
  found: boolean
  certificate?: {
    path: string
    names: string[]
    issued_at: string
    expires_at: string
  }
  expires_in_days?: number
  /**
   * The certificate should already have been replaced and was not — so renewal
   * has been failing quietly, and there is still time to fix it. This is the
   * field the module exists to be able to set: everything about this failure
   * looks fine until it suddenly does not.
   */
  renewal_overdue: boolean
}

export interface IngressStatus {
  enabled: boolean
  /** The suffix published names live under, owned by the dns module. */
  domain?: string
  service?: UnitStatus
  service_error?: string
  /**
   * The proxy olr found, and why it found none. olr ships no proxy, so "is one
   * present" is part of this module's health rather than something to discover
   * when an apply fails.
   */
  binary?: string
  binary_error?: string
  certificate: IngressCertState
  certificate_error?: string
  certificate_warnings?: string[]
  published: number
  drifted: boolean
  drift?: IngressPlan
  drift_error?: string
  as_of: string
}

export interface IngressApplyResult {
  plan: IngressPlan
  steps?: Step[]
  error?: { message: string; problems?: Problem[] }
  /** What is stored now, redacted. Present on the refusal path especially: it
   *  says the document did not move. */
  config?: IngressConfig
}

export interface IngressProviders {
  binary?: string
  providers: string[]
}
