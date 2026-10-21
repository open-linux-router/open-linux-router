# UI content and presentation standards

The visual direction is a precise, calm network console. Content must support
that direction: users should identify the important information before reading
supporting text. This guide complements [the visual guide](ui-style-guide.md).

## Start with the user's decision

Advanced's System group includes Backup. The backup page distinguishes a
credential-bearing configuration export from runtime or whole-machine backups.
Selecting a restore file validates it first; replacement requires a separate
explicit action showing scope, credential changes and the recovery deadline.
Preserve the original JSON bytes in both directions, including uint64 values.

Disruptive network changes require a second decision after application. A global
recovery notice shows the remaining confirmation time, Keep changes and Revert
now. Never confirm automatically on a successful API response or page load.
Keep failed recovery and lost connectivity visible; explain how to reconnect to
the previous address after the countdown.

Device priority and speed limits apply automatically, without an enable switch.
Use “applied” only after observing matching runtime state. Show pending device
placement and apply failures explicitly. Blank means unlimited. Keep the user
workflow focused on per-device priority and speed limits; do not add aggregate
QoS settings to Gateway or imply they are required for device limits.

Before adding content, write down the page's primary task and what the user needs
to decide next. Give each visible element a purpose in that decision. Remove
repeated or unrelated information; move useful later-stage information to the
stage where it is needed.

| Page type | Primary question | Lead with | Supporting content |
| --- | --- | --- | --- |
| Directory / entry | Which capability do I need? | Recognizable names and distinct imagery | A short distinction only if names are ambiguous |
| Monitoring | What is happening, and what needs attention? | Current state, important changes, key measurements | Units, freshness and scope beside the data |
| Configuration | What am I changing? | Current value and its control | Necessary constraints, consequences and apply action |
| Diagnostic operation | What should I run? | Target, essential options and run action | Requirements and measurement scope |
| Diagnostic result | What happened, and what next? | Outcome and decisive measurements | Evidence, limitations and a relevant next action |

Apply these rules to existing pages as well as new features. The page-by-page
[review record](ui-page-review.md) documents the current baseline.

## Simplification preserves data and consistency

Reducing clutter does not mean reducing every data display. First remove repeated
introductions, obvious instructions and duplicated labels. Then clarify grouping,
ordering and state. Do not shrink metric cards, value typography or chart space
to make a page appear quieter. Overview cards may reduce padding and row height
to improve density while preserving values, units and mobile interaction targets.
Section landing pages omit titles duplicated by navigation, retaining a visually
hidden page title. Detail and operation pages keep their identifying titles.

- Overview shows Status, Traffic, Latency and Quality cards. Traffic's
  title-right map icon toggles statistics and map views in place. Its pressed
  state identifies the active map, and its accessible label names the next view.
  The fullscreen map has its own /map address. The
  quality radar shows Speed, Latency, Stability, Wireless and Functionality
  on a 0–100 higher-is-better scale. Wireless defaults to 80 when unmeasured or
  stale; explain this display-only default in details and the accessible chart
  description. Other missing or stale axes show a dash. Draw a polygon when all
  five axes have display scores. Omit the card total and measured-category caption.
  Details show the total out of 100 using normalized weights of current scored
  categories and components. Missing tests never reduce the score; measured
  failures still count. Show the measured scope, effective weights and earned
  contributions in details. Clicking Quality, its chart or
  Test opens the fullscreen assessment view with run/cancel, an optional
  bandwidth test, freshness, category contributions and raw service/NAT
  evidence. The view has its own /quality address. Keep bandwidth cost and
  router-default-route scope beside the run action. Snapshot reads never start
  tests. Clicking the Latency title opens
  latency settings, without a separate plus button. The dialog starts with an
  Internet target selector: a named provider or “Choose fastest once”. The
  selected target stays fixed across failures and restarts. Show the latest
  single HTTPS latency sample, with `<1 ms` for sub-millisecond values; keep
  DNS lookup simple and omit bandwidth tests. Monitored sites follow the target.
  `Up` and elapsed uptime
  lead Status above system health. Click uptime for a dialog with CPU, memory and root disk
  used/total values; omit the separate System card.
  Click system health for a dialog with DHCP, DNS and Gateway states, with links to their
  pages. Distinguish checking, unavailable, disabled, needs attention and OK;
  retain the visible fault alerts below the cards.
  Latency and Map have no information icons. Traffic flips to reveal its map
  on the back, without a Map details row;
  clicking the enlarged grayscale map opens a viewport-filling color map,
  with a close control and Escape support. Country colors distinguish geography,
  while download/upload retain their semantic colors.
  omit the inline direction legend and top-ten summary.
- Overview Status shows one public-network summary using IPv4's country flag,
  city and ISP. Its row opens a dialog showing IPv4, IPv6 when available, ASN and ASN
  Organization from the IPv4 lookup, in that order; label the organization “Org”.
- Keep download and upload limits beside their respective measurements; separate
  controls serve separate decisions and are not automatically redundant.
- Overview cumulative traffic uses Download then Upload labels, each with
  its byte unit. Preserve unavailable states rather than
  displaying zero when counting is disabled or a read fails.
- Group Overview live traffic and utilization separately from cumulative traffic,
  site probes. Align section starts across desktop cards while keeping
  each group's internal spacing suited to its content; Status remains one list.
- Keep common configuration fields, addresses, counters and useful evidence
  visible. Do not move them into disclosures solely to shorten a page.
- Reuse existing headings, Card, ListRow, StatusStrip, fields and Dialog. Do not
  invent a compact card, new density tier or special layout for each feature.
  Choose shared size variants: MetricValue sm for inline measurements, md for
  summaries and lg for diagnostic results; Card's sm/default spacing is separate
  from Heading's sm/md/lg emphasis. Unavailable latency is never a zero result.
- Disclose lengthy technical explanations and infrequently used advanced options.
  Keep prerequisites and consequences beside the action they affect.
- A page can pass review without a visible redesign. Preserve a coherent existing
  presentation when its content already supports the user's task.

## State and data contracts

Quality assessments run on demand from the fullscreen assessment view opened by
the card heading or chart. Measured scores do not expire; results remain until
another assessment and survive daemon and router restarts.

World traffic combines IPs at the same remote coordinates into one map connection.
Line width represents that connection's combined upload and download relative
to the busiest one; per-IP rates stay in details. Arcs are one amber
hue, outside the map's country colours, and each connection has one neutral dot
that travels out and back, illustrating both directions without selecting a
direction based on traffic volume.

Ingress distinguishes saved repairs from applied settings. When other existing
errors prevent application, say the repair was saved, list the remaining service
names and problems, and explain that the running proxy stays unchanged until
the final repair applies automatically. Retain this warning after refresh.

World traffic uses two-second sampling and UI refresh. Smooth map transitions communicate
continuity between samples, not individual packets or exact connection lifetimes;
the details ranking and rates continue to show the latest observed snapshot.
World traffic ranks remote IPs over a rolling thirty-second observed window,
then locates only the top forty using cached ip.sb city lookups. Details show
city (or region) and country; arcs use only valid returned coordinates,
omitting IPs without coordinates. State that IP locations are approximate in the
details dialog, which also carries the sampling caveats and ip.sb attribution. The fullscreen
administrative map adds country, province/state and major-city names progressively
on zoom, stopping at regional detail because IP locations are approximate; it
shows no title, legend, summary, status or credits, only the map and a close button.
Country fill colors distinguish geography and carry no traffic or status meaning.
Traffic has no GeoIP city, region or country labels. Map labels use bundled
geography and local fonts, without fetching an external basemap.
Show their share of observed traffic in the details dialog,
and retain unknown locations in that denominator. Never imply
zero activity is a failed read, or sampled conntrack
bytes equal whole-router totals. Details name short-connection and bypass
limitations, warm-up, counting-disabled and unavailable states.

Distinguish loading, read failure, unknown, empty, disabled and healthy states.
No response is not an empty rule set; saved configuration is not proof of a
running service or end-to-end connectivity. Use neutral pending/unknown styling,
an explicit error and a retry where useful. Name configuration counts as such.

Firewall monitoring retains semantic permission categories and adds incoming
source as a separate dimension. Use one vertical diagram with three visual
rows: external semantic categories at the top, one virtual OLR / Firewall node
in the middle, and LAN/trusted and local categories at the bottom. Both outer
rows show arrivals at OLR, not an external-to-LAN forwarding path. Each category
shows its name and packet count; position conveys source and color conveys the
Allowed/Blocked decision. Keep full source and decision text in tooltips, without
repeating them on each node. Omit explanatory paragraphs and the matching-conditions
disclosure below the diagram. Hide zero-traffic categories and
sources. Path widths use log(1 + packet count) weights normalized separately
for each side, with a 4px minimum so small categories remain visible. Retain exact
packet labels and byte totals and matching conditions in tooltips.
Keep unavailable counters distinct from a verified zero-traffic state.

Show QoS as saved configuration until enforcement exists.
Device details group stable identity and saved settings on the left, live
uplink, priority and live traffic on the right. Reuse Overview rate meters with
Download and Upload labels beside narrower bars, without arrows.
Total below Upload shows download then upload with the Overview arrows, added up
over the same retained history the title matrix draws and captioned with that
window (`last 90 days`), so a reader can check it against the cells above it;
unavailable history shows dashes. Services below Group lists matching configured
services as icons that open their webpages, with an equally sized add action
that opens Ingress and prefills the current device. Empty lists show only the add action.
Devices without a reserved name/address prefill their observed host instead.
A device that dialed in draws the same row, matched by its tunnel address, and
its add action prefills that address rather than a device reference.
Group is a tag that opens its edit dialog.
Compact Uplink and Priority segmented selectors lead the right column, in that
order. Uplink selections retain network-change confirmation and apply feedback.
On desktop, pair Vendor with Uplink, MAC with Priority, IP with Download,
Group with Upload and Services with Total in shared rows. Place read errors and
traffic availability messages after those rows so they do not shift the
corresponding values.
The title's activity matrix shows one day per column, newest
on the right, and six four-hour periods from top to bottom. The displayed date
range adapts to the available width, with fixed square cells; dates without
retained history remain explicitly missing data. Leave future periods
blank and show download/upload totals immediately on hover or focus. Format the
time as `10-14 4:00-8:00`, followed by traffic only; omit sampled/progress wording.
A device page reports identity and settings, and leaves presence to the lists and
the map. It draws no Last heard and no Way in row, and the hardware address is a
fact row rather than a chip beside the title. Lists, the network map and the
device dialog still report when a device was last heard, never whether it is
online: a DHCP lease outlives presence by hours and the kernel keeps a stale
neighbour entry looking complete, so Online would be a claim this box cannot
support; Last heard is the fact it can. The one exception is a device that dialed
in: every configuration this router renders carries a keepalive, so three minutes
without a handshake really is a broken tunnel, and that page alone says Connected.
Its Last heard (the handshake) and Way in (the tunnel interface and subnet) stay,
because the tunnel is the only thing that can answer them, and its configured
name is a fact row in the same place the device page puts its MAC.
Speed bars open download/upload limit dialogs; keep the
configuration-only caveat in those dialogs and save feedback, without a status row.
Omit Notes and the QoS heading.
Device activity sits at the right of the title bar, without visible
explanatory text. Only the current period breathes with live combined download/upload
speed: faster traffic means faster breathing; idle or unavailable rates stay steady.
Rate updates smoothly change playback speed without resetting the breath.
On becoming idle, complete the current breath before settling at full brightness.
Historical colors continue to represent accumulated traffic.
The lowest nonzero tier (below 10 MiB combined upload/download per four-hour
period) uses a stronger neutral gray than zero/missing data; green starts at
10 MiB. Four green levels use the device's retained 90-day four-hour totals at or
above 10 MiB, split at 25%, 50% and 75% of the greater of the maximum total
and 1 GiB, independently of
screen width. These divide the byte range, not the number of cells.
Values equal to a boundary use the lighter tier; identical totals share a color,
and a flat or single-sample history below 1 GiB follows the same minimum reference. Different devices'
greens represent their own relative traffic, not comparable byte quantities.
This distinguishes low traffic without implying whether a person is
actively using the device.
Immediate four-hour hover/focus hints show sampled download and upload
or explicitly report missing/unavailable data. Partial samples are not exact
four-hour totals. History is joined to the device's currently observed addresses.
It begins accumulating after daemon startup. Sampling runs every ten seconds and
the matrix refreshes every minute; the first reading establishes a baseline.
History retains at most 90 days and is restored from
SQLite's `activity` table in `/var/lib/open-linux-router/runtime.db` after
restarts/upgrades. Transactional
checkpoints run every minute and on normal shutdown; an abrupt power loss may
lose the latest minute. It is not reconstructed from cumulative counters. Changing
device addresses can hide samples associated with its previous addresses.

Device Uplink offers Default (inherit interface/system routing) and named
exits only. Existing explicit router-connection overrides remain accurately
displayed until the user selects Default or a named exit.
Use label/value pairs within each device-detail half. Keep units with their inputs and chevrons beside selected values. Lead identity rows with Vendor. Edit addresses and their fixed toggle
in a dialog opened by clicking the IP. Show a Fixed tag beside reserved addresses;
keep the hover hint limited to DHCP lease information and omit the hostname field.
Preserve diagnostic scope: router-originated tests do not measure a downstream device's path.
The Inspection switch starts and stops the session directly, without confirmation.
Explain credentials, certificate trust and cleanup in the setup guide.
Device Connections keeps Inspection and its switch in the title bar, with a
setup gear icon while active; keep the QR code and guide link inside the setup
dialog. Show the mitmproxy viewer directly, without duplicate request lists, diagnostics or a
disclosure. Certificate installation, trust and cleanup belong
in the public five-step setup guide, with the certificate download at step two.
Device DNS defaults to hidden records with an Inspection switch in its title bar.
Enabling it polls and displays Time, Record, Type and right-aligned Answer values
without a visible header row. An accessible search icon reveals a compact filter
beside the switch in the title row; closing clears the filter. Both inspection
panels keep their title bars fixed and expand content downward.
DNS and Connections title bars show dividers only while their inspection is active.
Do not promise encrypted DNS blocking beyond the supported protocol and port.

Use download then upload consistently. Device names may supplement an address
only when the address uniquely identifies a device. Keep long domains, IPv6
addresses and provider errors readable rather than silently truncating them.

## Disclosure and hierarchy

Access groups technology tiles under “Networking”, “Add a way out” and “Add a way
in”. Keep WireGuard, Tailscale, NetBird, ZeroTier, Headscale, SSH, SOCKS5,
Gateway and HE Tunnel Broker in the directory. Shadowsocks, Trojan, VLESS, VMess,
Hysteria, TUIC, OpenVPN and IPsec are outside the product scope. Do not label the
directory as proof of legal compliance or describe removed technologies as a
statutory list of prohibited products.
A technology can appear in both directions; its tile goes straight to the
configuration owner for that direction, and a technology OLR does not configure
opens its project documentation instead. Remove System default and purpose
filters from this page. Use app-style icons above names without enclosing cards.
Use project logos for branded technologies, including frp's official icon.
SOCKS5 and Port forwarding use reviewed original app artwork with simple flat
functional shapes and no lettering; the name remains below each icon. Other
unbranded entries retain a capital initial on a tinted tile. The approved HTTP
and HTTPS artwork does not introduce directory entries or replace Caddy's mark.
Port forwarding is a forwarding rule rather than a remote-access technology,
so it is not a tile
here; it lives under Advanced with the firewall whose ruleset it shares.
Functions and state appear on hover/focus and in the tile's accessible name.
External technologies remain explicitly “Not integrated”; broader ecosystem uses
are not promises of OLR configuration support. Existing service states remain
authoritative.

Tunnel Broker keeps provider details together in one saved assignment, with
activation and LAN assignment as separate decisions. Saving defaults to off.
Show five sections on one page with sticky step navigation; omit the repeated
page introduction and back-link heading. Keep endpoint requirements and change
consequences visible; disclose server comparisons and technical explanations.

Overview group headers open the group's detail page directly on desktop and
mobile. The unnamed device bucket retains its in-place disclosure.
Keep service icons beside the compact activity column, leaving the remaining
row width for the device name.
Group details lead with parent, device counts and subgroup links beside bulk
uplink and priority controls in the device summary layout. Keep current-member
scope, replacement behavior and the priority enforcement caveat beside the
explicit apply actions; selected values are batch choices, not observed shared
settings. List member devices below the summary.

Gateway is objects only: the interfaces this router is plugged into, its way
out, and the services it runs. It has no status strip and no switches — routing's
on/off state, drift and every routing and forwarding switch belong to the
routing settings at interfaces#forwarding, beside the sentences that say what
turning them off closes. Overview reports a gateway fault. Physical interface
cards carry name, role, link state, address and current rates without history
charts, and open that interface's own page rather than a dialog: the page owns
link speed, MAC, every address, live rates, totals and the address range the
interface serves. Way out states the
observed IPv4/IPv6 default routes and the resolvers this router uses, read-only.
Services states DHCP and DNS with their state and their counters (active leases,
queries since restart). IPv4 also controls Gateway routing and leaves system
forwarding unchanged when off; IPv6 intent is unapplied when routing is off.
Usage has no separate page; traffic counting lives in routing details.

Open newly navigated pages at the top so their title and primary information are
visible. Preserve section targeting for hash links and scroll for query-only changes.

Default to two content levels: the main information and necessary context.
A third level needs a specific reason, such as a consequential safety condition.
An explanation is optional, including under a page title. Do not fill every
available subtitle or helper slot just because the component supports it.

Entry pages help selection. Operation pages explain execution. Result pages
explain outcomes. Do not put protocol implementation details, setup instructions
or descriptions of the next screen on every directory card.

Keep information that affects safety or interpretation visible at the decision
point: disruptive network changes, authentication consequences, unavailable
data, stale measurements and test scope must not disappear as part of simplification.
Secondary diagnostics and raw technical evidence can be disclosed separately
with clear labels.

## Languages and shared terminology

The UI supports English and Simplified Chinese. Keep English source messages
and their Chinese translations together through `t()` and
`web/src/lib/locales/zh-CN.ts`; reuse messages rather than inventing synonyms.
Use 网关 for Gateway, 出口 for Uplink / Way out, 接入 for Access, 地址池 for
Address ranges and 地址预留 for Reserved addresses. Translate status and actions
without changing protocol identifiers, addresses, commands, user names or API data.
Keep technical backend errors verbatim so operators can diagnose them.

Language selection is browser-local, persists across reloads and synchronizes
between tabs. On first visit, use Chinese for a Chinese browser preference and
English otherwise; unsupported languages fall back to English. Switching languages
updates the document language and visible controls without resetting forms or
restarting queries. Keep the switch available before login and initial setup.
New messages without a translation fall back to English. Translate complete
sentences with named placeholders when values are embedded; do not assemble new
sentences from translated fragments. Keep state decisions independent of copy.

## Naming and writing

Use familiar tool names and concise task labels. Keep distinctions in the name
when they help selection: Internet speed test and LAN link test identify different
measurement boundaries. Preserve established names such as Ping, NAT test and
Traceroute. Do not invent broad category labels for a single item.

Avoid repeating the title in a description, stating obvious click behavior or
adding a generic next-step sentence. Use one short sentence when an explanation
is necessary. Keep essential text readable; reducing font size is not a remedy
for excessive content.

Keep the footer to the product copyright, version, GitHub, Credits, language switch
and an icon-only theme control. On desktop, bottom-align the visible content with 12px bottom padding.
Describe the current and next theme in its accessible label and tooltip.

## Capability and richness

Device icon selection supports the same theSVG library as site logos. Choosing
a library logo changes only the picture, preserving the device category. Keep
automatic detection available and show a clear empty result for unmatched searches.

Show the real capability set together with consistent visual weight. Keep entries
compact enough to scan across the set. Richness comes from recognizable, distinct
capabilities, useful data and dependable interactions. Do not inflate it with
oversized panels, redundant labels, decorative badges or unavailable features.

Use a consistent ordering. Add grouping when the collection is large enough that
groups reduce search effort; add search when scanning the collection becomes
burdensome. Five tools need neither filters nor one category per card.

## Advanced directory reference

Advanced's goals are quick selection, a current answer and a calm presentation.
Its contract is:

- Three groups with charters: System (the machine and what may reach it),
  Services (background work for the network), Diagnostics (measurements).
  Place a feature by its charter, not by which module happens to own it.
- One tile per destination, with a capital initial on a 64px tinted tile. The
  tiles of a group share one hue; the letter carries identity.
- A tile that names an upstream tool takes that project's name and its app icon
  (NextTrace opens Traceroute); a tile that is a protocol or OLR's own
  measurement keeps its initial.
- One line of name below the tile, never wrapped, and nothing printed under it.
  Keep the tile's name short enough for one line; the page behind it may carry
  the longer name (Admin access opens Administrator access).
- Function and state appear on hover/focus and in the tile's accessible name.
- The whole tile is a keyboard-accessible link; no dialog opens on click.
- There is no intermediate "Tools" page: a diagnostic tile opens the tool.
- Keep explanations, requirements and results on the individual pages.

## Review before shipping

- Can the primary information be identified without reading every paragraph?
- Does each supporting line change a decision or explain a necessary constraint?
- Is any information repeated across title, description, badge and action?
- Are instructions shown at the step where they are used?
- Does simplifying preserve warnings, measurement scope and unavailable states?
- Can users scan the capability set on desktop and find entries on a phone?
- Do imagery, whitespace and type reinforce the same information hierarchy?

Update both guides when changing a shared presentation contract.
