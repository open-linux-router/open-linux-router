# Page content review baseline

The objective is fast identification of useful information, a visible range of
OLR capabilities, and a calm, consistent interface. This record accompanies the
[content guide](ui-content-guide.md) and [visual guide](ui-style-guide.md).

| Pages | Review decision |
| --- | --- |
| Overview | Show Status, Traffic, Latency and Map; uptime leads Status. Omit System, information icons and the map legend/top-ten hint; retain charts and separate direction limits. |
| Device | Separate observed facts from editable settings within the existing card; keep all fields; place DNS activity before inspection and QoS. |
| Group | Preserve member lists and batch controls; remove repeated MAC/direct-membership prose; keep overwrite and non-inheritance consequences. |
| Device/group QoS | State that configuration is saved but not enforced; keep forms and shared group-cap semantics. |
| Inspection | Keep the existing panel and results; confirm privacy and CA consequences before starting; disclose lengthy preparation/scope and viewer details; retain session stop time and diagnostics. |
| Gateway | Preserve cards, charts and section structure; shared headings remove generic introductions. |
| Interfaces/routing and interface detail | Keep controls and all observed fields; shorten management and routing explanations without moving controls. |
| Exits | Preserve list/edit forms; distinguish blocking, unknown and failed checks; remove repeated failure prose. |
| Usage | Preserve measurement controls and counters; show a uniquely matched device name with its address; download precedes upload; disclose accounting limitations. |
| External routing rules | Distinguish loading, failure, unknown and empty; preserve readable raw evidence and provide retry. |
| DHCP details, ranges, reservations, advanced | Put status/details first; preserve lists, range forms and address fields; concise headings and managed-configuration constraints. |
| DNS details, blocking, names, upstream, listening, enforcement, advanced | Preserve filters, values and forms; group activity accounting metadata; shorten repeated explanations; retain binding, TLS verification and protocol limitations. |
| Firewall | One vertical Sankey with three visual rows: external semantic categories above one virtual OLR / Firewall node, trusted/LAN and router-local categories below. Both sides arrive at OLR; outer nodes state source and Allowed/Blocked decision. Use one actual packet scale and only nonzero categories. Matching conditions include source, destination, packets and bytes. Wrap labels and adapt chart width; overflowing charts scroll from the leading edge with a visible hint. Preserve switch and warnings; native, disabled, unverified and unavailable counters do not claim automatic traffic measurements. Editing lives at `/firewall/rules`. |
| Access | Remove duplicate hero introduction; keep the established directional groups and capability links. |
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
| Credits | Preserve every project and attribution; remove repeated section introductions. |
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
