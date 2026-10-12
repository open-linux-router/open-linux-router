# Page content review baseline

The objective is fast identification of useful information, a visible range of
OLR capabilities, and a calm, consistent interface. This record accompanies the
[content guide](ui-content-guide.md) and [visual guide](ui-style-guide.md).

| Pages | Review decision |
| --- | --- |
| Overview | Preserve four metric cards, CPU/memory/uptime, charts and separate direction limits; remove generic introductory copy through the shared shell. |
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
| Firewall | Preserve native rules, counters, remarks and controls; shared heading cleanup only; no new rule view. |
| Access | Remove duplicate hero introduction; keep the established directional groups and capability links. |
| SOCKS5 outbound | Keep status, authentication and address fields; concise support scope and routing next step. |
| Remote access | Keep separate status/settings/peer cards; remove duplicate WireGuard labels and onboarding prose; retain exposure and password information. |
| HTTPS ingress | Preserve certificate details and all service domains/targets; show configuration counts accurately; keep service-authentication warning. |
| Port forwards | Keep shared routing switch and counters; distinguish unknown rules from inactive rules; configuration counts do not promise public reachability. |
| External filtering | Distinguish loading/error/unknown/empty; retain policy and counters without claiming end-to-end connectivity. |
| Tools directory | Retain the approved five-entry directory with illustration, name and chevron. |
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
