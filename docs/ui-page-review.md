# Page content review baseline

The objective is fast identification of useful information, a visible range of
OLR capabilities, and a calm, consistent interface. This record accompanies the
[content guide](ui-content-guide.md) and [visual guide](ui-style-guide.md).

| Pages | Review decision |
| --- | --- |
| Overview | Show Status, Traffic, Latency and Map; uptime leads Status. Omit System, information icons and the map legend/top-ten hint; retain charts and separate direction limits. |
| Quality report | One column: the overall-score band, then the five categories. No standalone transfer card, scoring-explanation block or evidence dump: the reference transfer sits under Speed, each service's attempts open under its own row (timings for Latency, sequence for Stability, outcome for Functionality), and NAT observations are an unscored diagnostic under Functionality. Measured coverage is the only figure with a hover hint. |
| Device | Separate observed facts from editable settings within the existing card; keep all fields; place DNS activity before inspection and QoS. |
| WireGuard device | The tunnel's answer, not the device list's: the label edited in place in the heading with the name beside it, the picture from the same picker a device uses, then address, public key, what it sends home, last handshake and where from, with activity, current rate and totals from the address-keyed counters, the services published at its tunnel address in the same row a device page uses, and DNS queries by tunnel address. Omit vendor, group, reservation and per-device priority/limits — keyed by a MAC a tunnel does not have — and answer inspection with one word rather than a switch that cannot work. |
| Group | Preserve member lists and batch controls; remove repeated MAC/direct-membership prose; keep overwrite and non-inheritance consequences. |
| Device/group QoS | State that configuration is saved but not enforced; keep forms and shared group-cap semantics. |
| Inspection | Keep the existing panel and results; confirm privacy and CA consequences before starting; disclose lengthy preparation/scope and viewer details; retain session stop time and diagnostics. |
| Gateway | Interface cards in a wrapping grid with role, address and rates, each linking to the interface's own page, then two stacks — IPv4 and IPv6 — of six stage cards each (Upstream, LAN, DHCP, Forwarding, Exit, DNS) that state the stage's answer and link to the one page that changes it. No status strip and no switches — routing state, drift and all routing/forwarding switches live at interfaces#forwarding, and Overview reports faults. |
| Interfaces/routing and interface detail | Keep controls and all observed fields; carry link speed, every address, live rates, totals and the served DHCP range on the detail page, since the landing card no longer opens a dialog; shorten management and routing explanations without moving controls. |
| Exits | Preserve list/edit forms; distinguish blocking, unknown and failed checks; remove repeated failure prose. |
| Usage | Preserve measurement controls and counters; show a uniquely matched device name with its address; download precedes upload; disclose accounting limitations. |
| External routing rules | Distinguish loading, failure, unknown and empty; preserve readable raw evidence and provide retry. |
| DHCP details, ranges, reservations, advanced | Put status/details first; preserve lists, range forms and address fields; concise headings and managed-configuration constraints. |
| DNS details, blocking, names, upstream, listening, enforcement, advanced | Preserve filters, values and forms; group activity accounting metadata; shorten repeated explanations; retain binding, TLS verification and protocol limitations. |
| Firewall | One vertical Sankey with three visual rows: external semantic categories above one virtual OLR / Firewall node, trusted/LAN and router-local categories below. Both sides arrive at OLR; outer nodes state source and Allowed/Blocked decision. Use one actual packet scale and only nonzero categories. Matching conditions include source, destination, packets and bytes. Wrap labels and adapt chart width; overflowing charts scroll from the leading edge with a visible hint. Preserve switch and warnings; native, disabled, unverified and unavailable counters do not claim automatic traffic measurements. Editing lives at `/firewall/rules`. |
| Access | Remove duplicate hero introduction; keep the established directional groups, and link each tile straight to its configuration page or project documentation instead of a dialog. |
| SOCKS5 outbound | Keep status, authentication and address fields; concise support scope and routing next step. |
| WireGuard and SOCKS5 server | Keep separate status/settings/peer cards per view; remove duplicate WireGuard labels and onboarding prose; retain exposure and password information. |
| HTTPS ingress | Preserve certificate details and all service domains/targets; show configuration counts accurately; keep service-authentication warning. |
| Port forwards | Keep shared routing switch and counters; distinguish unknown rules from inactive rules; configuration counts do not promise public reachability. |
| External filtering | Distinguish loading/error/unknown/empty; retain policy and counters without claiming end-to-end connectivity. |
| Tools directory | Use five consistent Lucide outline icons on themed surfaces, full names and chevrons; remove baked-in raster backgrounds. |
| All five tool operations | Preserve inputs/results and router-originated measurement scope; remove duplicate page subtitle; shorten selector hints; retain bandwidth and privacy consequences. |
| Advanced | Keep existing capability entries; remove generic shell description. |
| DDNS | Preserve provider/source/publication information and forms; explicit loading/error/unknown status; short empty state. |
| IPTV | Keep signal-path/source-prefix forms and preview/apply flow; concise prerequisites; disclose long scope limitations; pending status is not off. |
| Administrator access | Keep password/security controls; remove redundant introduction. |
| Credits | One ungrouped table with no visible header row, no section rows and no website column: the first column is the project mark and name as one link, followed by version and usage. Keep every feature-carrying project and the libraries that implement a listed feature (pro-bing, for example), with full dependency and license lists linked from the manifests; stacked records below 768px. |
| Sign-in, initial setup, first-run, not-found | Preserve existing components and safety consequences; concise setup steps and missing-page message. |
| Dialogs | Retain existing common fields, advanced disclosures and apply previews; remove redundant exit-name help; inspection consent uses the shared Dialog. |

Review does not require changing every file. Keep useful existing content and
layouts where they meet the standard. Future iterations should validate empty,
populated, loading and error states as well as desktop/mobile and both themes.
Browser fixture checks validate presentation, not live router networking.

## Visual consistency review — 2026-10-12

Reviewed all 43 current pages and entry states at 1440px, 375px and 768px,
plus desktop dark mode (172 captures). Coverage includes device/group details,
all Gateway/DNS/DHCP pages, Firewall, all Access pages, all five diagnostics,
Advanced/DDNS/IPTV, administrator access, Credits, not-found, both sign-in
methods, setup and first-run. Opened the add dialogs for exits, DHCP ranges and
reservations, DNS blocking and local names, and port forwards on desktop and
phone. No browser exceptions or page-wide horizontal overflow were observed.
The synthetic ingress service icon is absent; its intended fallback is shown.

Corrections: use one centered 1280px shell for every route, align Overview metric
heading line boxes, keep the health dot beside its label on phones, replace
background-baked tool illustrations with consistent themed outline icons, and
let switch panels share the page boundaries while retaining readable form widths.
Administrator password controls keep a 720px maximum inside their full-width card.
Existing device imagery and useful monitoring/data space are preserved.

Validated with Node 24 web lint/build/tests and Go 1.27 Linux vet/tests. Lint has
existing warnings; production build retains the existing bundle-size warning.
The environment lacks make, so the Go commands behind make check were run directly.

## Gateway landing review — 2026-10-21

The page read as unfinished for structural reasons rather than colour: two
levels of border around the interface cards, a right-hand column 120px taller
than what it summarised (leaving an empty band inside the panel), a second set
of section headings indented 68px behind the first, 660px service cards holding
200px of content, and three wordings of "Details".

Corrections: interface cards lost the 9px role text and the two 3px squares drawn
on the shared glyph — the role is now a badge beside the name, and the card
carries name, state, address and rates only. The interface grid wraps instead of
scrolling locally, so a fourth NIC is not hidden at the edge. Way out and
Services are panels of their own, so every block on the page is a Card with the
same title contract, and the DHCP/DNS rows are links with their state on the
right.

It then went further than a shared strip. A status strip was tried and removed:
its one switch was a one-click way to take every network behind the router off
the internet and close its forwarded ports, and without the switch the strip
repeated the routing details page. Routing state, drift, the module switch and
the IPv4/IPv6 forwarding switches all live at interfaces#forwarding, beside the
sentences that say what turning them off closes; Overview reports a gateway
fault. The landing page is the objects only, and the Interfaces heading is its
single settings entry.

Validated against fixture data at 1440×1000, 1024×1000, 768×1200 and 390×1200
in both themes, plus the empty, loading and error states of each panel and the
interface page. No page-wide overflow or horizontal scroll at 375px. Hash
targets `#interfaces`, `#dhcp` and `#dns` still scroll. Mocked browser checks
validate presentation, not live networking.

## Interface cards review — 2026-10-21

The landing card opened a dialog holding what the card could not: the network
it carries, the negotiated link speed, MAC, every address and both totals. A
dialog is the wrong shape for that — it is a page's worth of readings behind a
click, with its own "Interface settings" link at the bottom — so the card now
links straight to the interface page and the dialog is gone.

The interface page took over the readings and gained the two that were only ever
on the tile: what it is moving now and what it has moved in total, both read from
the same NIC counters as the card. It also states the DHCP range its network
serves and how much of it is in use, so "this interface's LAN is running out of
addresses" is visible where the interface is inspected rather than one link
away. Everything the dialog held is still reachable, and the keyboard path is
shorter: one tab stop to the page instead of button → dialog → tab into it.

## Gateway stacks review — 2026-10-21

The page answered "what is this box plugged into" and then left the operator to
work out the rest. IPv6 in particular was one line in Way out: the delegated
prefix, the LAN's /64, whether anyone advertised it, whether the kernel
forwarded it, and whether an exit dropped it were each in a different module,
and finding them required knowing which module owned which half.

Way out and Services are replaced by two stacks, IPv4 and IPv6, each cut across
the modules in packet order: Upstream, LAN, DHCP, Forwarding, Exit, DNS. Each
card states that stage's answer for that family and links to the one page that
changes it, so the question "is IPv6 working, and where do I fix it" is answered
without a module map. Missing stages are named in the card — no /64, nobody
advertising, forwarding off — instead of being absent, because an absent row
reads as "nothing to do" rather than "not configured". The clients stage is
titled DHCP in both stacks because that is the module — and the word an operator
arrives with; the IPv6 card says SLAAC or DHCPv6 in its detail line and opens the
address ranges that carry each network's advertisement mode.

The stacks are a read model. Every card is a link: no draft, no plan, no apply
of its own, so the same module appearing once per family is one object seen
twice and cannot disagree with itself. That is also why the page can afford two
full stacks without becoming a second settings page, and why design.md §4.3's
"no page that sets IPv6 across modules" still holds — it is amended there to
name this view explicitly.

Hash targets survive with the new structure: `#interfaces` is the interface
card, and `#dhcp`/`#dns` (the redirects from the settings trees) point at the
IPv4 DHCP and DNS cards. The interface card is unchanged, and every previous
Way-out fact — both routes, the resolvers, the unmanaged-rule count — has a home
in the stacks except the unmanaged rules, which remain on interfaces#forwarding.

Validated as a read-only page: lint, type-check, unit tests and a production
build pass. Live router state was not available, so the cards were checked
against types and fixtures, not against a running box.
