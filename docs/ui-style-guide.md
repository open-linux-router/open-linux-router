# UI visual style guide

This is the shared reference for future UI changes. The direction is **a precise,
calm network console with lightly tangible surfaces**. Clarity of network state
comes first; polish comes from alignment, typography, restrained depth and
consistent interaction. Keep the existing Geist typeface, router identity and
device imagery.

For information hierarchy, wording and progressive disclosure, follow the
[content and presentation standards](ui-content-guide.md).

For the cross-page component inventory, typography roles, surface interactions
and migration targets, follow the [semantic component contracts](ui-semantic-components.md).
That document defines shared size variants and records the implementation audit;
route-specific behavior below remains applicable.

## Findings and decisions

The audit found inconsistent content widths and headings, oversized tool
illustrations, repeated borders, scattered save buttons, values hidden on
phones, a cramped six-section navigation and inconsistent color meanings.
The shared shell, heading, list and control primitives now define the baseline.
Use them before creating page-specific styling.

Content cleanup must preserve existing data space and component consistency.
Remove redundant wording before changing density. Keep established metric cards,
charts and common fields; avoid introducing extra presentation variants for
secondary information. See the [page review record](ui-page-review.md).

## Color and surfaces

World traffic uses one neutral arc and one moving dot per remote location.
The same dot travels to the remote location and back along the arc, using
transmit color outbound and receive color on return, four seconds each way.

Use semantic Tailwind tokens from `web/src/index.css`, never page-local gray or
blue approximations. Light and dark themes are designed as separate contrast
pairs.

| Role | Light | Dark |
| --- | --- | --- |
| Canvas / background | #f6f7f9 | #111318 |
| Card / popover | #ffffff | #1a1d24 |
| Main text | #171b22 | #f1f3f7 |
| Secondary text | #5b6472 | #abb4c3 |
| Subtle surface | #f0f2f5 | #232731 |
| Divider | #e2e6ec | #343b48 |
| Input border | #a4adbb | #67758b |
| Primary text / focus | #1d4ed8 | #8ab4ff |
| Solid primary action | #2563eb | #2563eb |
| Text on solid primary | #ffffff | #ffffff |
| Receive / download | #1d4ed8 | #8ab4ff |
| Transmit / upload | #0f766e | #6bd4c6 |
| Success text | #167447 | #70d8a5 |
| Warning text | #935a10 | #f1c071 |
| Destructive text | #b42335 | #ff9daa |

Use `primary-fill` with `primary-foreground` for filled actions. Use `primary`
for links and selected navigation. Use `success-foreground` and
`warning-foreground` for readable status text. Color accompanies a label or
icon; it never replaces one. A normally blocked DNS request is neutral activity,
not an incident. Reserve destructive emphasis for errors and destructive actions.

Cards use a one-pixel border and shadow-xs. Shadows clarify elevation rather
than decorate every nested row. A list inside CardContent does not need a second
border. Use muted fills for small metrics, not another elevated card.
Use `panel-surface` for custom panels and `interactive-surface` for actual
link/button surfaces. Hover changes color, never shadow depth. Radius tokens
are 8px (`rounded-lg`), 12px (`rounded-xl`) and 16px (`rounded-2xl`). Dialogs
use 16px and shadow-md. Card spacing and title size are independent variants.

## Layout and hierarchy

Desktop and mobile navigation use Overview, Gateway, Wireless, Access and
Advanced, in that order. Wireless currently shows an under-construction placeholder.
Firewall and Tools are reached through Advanced, retain their existing URLs,
and highlight Advanced in navigation, including on their detail pages.

Firewall's arrival diagram uses a centered firewall boundary spanning 40% of
the chart width inside its 24px gutters. Pack incoming bands contiguously along
each edge, with logarithmic widths normalized separately for each side and a
4px minimum. Keep each curved path region about 88px tall and packet values
with their units on one line. Reserve separate label space so small traffic
categories remain readable.

Access has three app-launcher grids headed “Networking”, “Add a way out” and
“Add a way in”, in that order. Networking contains WireGuard, Tailscale, NetBird,
ZeroTier, Headscale and Nebula, shown once rather than repeated
in the directional groups. Keep their existing configuration actions available.
Do not show System default or purpose filters. Each tile shows a locally bundled
project logo above a short name, without an enclosing card, border, shadow or
corner arrow. Preserve brand colors on a white 64px rounded icon backing in
both themes. Protocols without a recognized logo use existing Lucide SVG icons;
never generate or draw replacement artwork. Use three columns on phones, five
from 640px, seven from 1024px and eight from 1280px, with 12px horizontal and
24px vertical gaps. Keep names below icons with an 8px gap and visible keyboard
focus around the whole button. Group headings use 24px/32px semibold text.
Hover/focus explains function and state; click opens
details and configuration actions for touch users. External setup stays explicit.

Tunnel Broker uses one scrollable page with five numbered sections and a sticky
step navigation immediately below the 64px shell header. Keep section anchors
clear of both bars and scroll the navigation locally on narrow screens. The
page title is accessible but visually hidden; avoid a repeated introductory
heading above the step navigation.

Gateway's Interfaces panel keeps physical interface cards in one horizontal row,
with a right-side routing summary from 1024px. Scroll the interface row locally
when needed; below desktop, stack the summary beneath it. Interface cards show
current rates without traffic charts, with wrapping addresses and 16px padding.

The shell has a 1280px maximum width, a 64px header and horizontal gutters of
16px on phones, 24px from 640px, and 32px from 1280px. Content starts with
24px vertical space on phones and 32px on larger screens.

All page containers use the available shell width, centered in the viewport.
Header, page headings, cards and footer share the same outer boundaries. Do not
apply route-dependent page widths. Individual fields and explanatory paragraphs
may constrain their own reading width without narrowing the entire page.
Section landing pages omit the visible heading already named in navigation;
retain an `sr-only` h1 for assistive technology, with no heading gap. Detail and
operation pages use `PageHeading` and `SubPage`: a visible
title, an explanation only when needed, optional back link or breadcrumb, and optional
primary action. Nested DNS and DHCP breadcrumbs lead through Gateway. Preserve
existing redirects and deep links when moving settings.

The spacing vocabulary is 4, 8, 12, 16, 24 and 32px. Use 32px between major
sections, 16–24px inside cards and 8px between label and control. Basic corner
radii are 8px for controls/small inset surfaces, 12px for panels and 16px for
larger enclosing surfaces. Avoid arbitrary one-off radii. Overview metric cards
use 12px padding on phones and 16px on larger screens, an 8px heading gap,
36px metric rows and 4px between rows. Interactive rows retain 44px targets on
phones; site latency items use unfilled 32px rows with 6px vertical gaps.
Only download/upload rates use inset meter surfaces
in Overview cards. Status, public addresses, latency, totals and uptime use plain
rows; monitored sites retain their icons without capsule backgrounds.
Cumulative traffic rows use Download and Upload labels with plain byte values.
On desktop, Overview cards share title and primary metric boundaries. Totals
and site probes follow their primary metrics without divider lines. Status
flows naturally. Below desktop, cards use their own content height.
Site probes use two columns on phones and three from 640px.
Latency settings place a full-width labeled Internet target Select above the
monitored-site list, sharing the dialog's Save and Cancel actions. Keep the
selected provider and automatic selection in this control, without extra
badges or explanatory content on the card.
The public-network summary uses a locally bundled flag-icons country flag.
Clicking any Status row opens its details in a shared dialog, without chevrons.
Public addresses use monospace and wrap within the dialog.
Status rows fill the card width. Give flags and health icons the same 16px
leading slot so text starts align.
System health uses the same dialog interaction as uptime, with module names on
the left and status values aligned right. Keep module details as plain rows.

## Typography and imagery

Use Geist Variable with system sans-serif fallback. Page titles are 24px/32px,
weight 600. Panel titles are generally 16px, weight 600. Body and controls are
14px; descriptions and technical metadata are 13–14px with relaxed line height.
Use 12px only for compact auxiliary metadata. Mobile text inputs are 16px to
avoid browser zoom. Use tabular numerals for live rates and counters, and
monospace for addresses, commands and configuration.

Device imagery identifies device categories. Device icon selection also offers
theSVG search above the built-in categories,
using the existing image tiles and selected state. Render chosen logos in the
same muted tile as operating-system marks throughout device views.

Tool directory entries use Lucide
outline icons with a 1.75px stroke in a themed 56px surface (48px on phones),
an 18px name and one directional chevron. Avoid raster illustrations with baked-in
backgrounds. Overview metric headings have the same 24px line box; auxiliary
icons extend their hit area without increasing the heading height. Descriptions and instructions
belong on the individual tool page. No decorative glows or additional gradients are needed.
Use Lucide consistently for functional icons, normally 16–20px. The two-color
brand mark retains its own colors in both themes.

## Controls, lists and dialogs

Ingress pending repairs reuse Alert above the settings and a warning status dot.
List remaining errors with service names and wrapping text; keep the existing
single-service edit dialog and save action.

Use the shared Button, Input, Select, Textarea, Switch, Card and Dialog components.
Regular controls are 36px high on desktop; mobile button/input/select targets
are at least 44px high. Switch visuals are 44 × 24px with an expanded mobile hit
area. Focus uses the primary ring and must remain visible. Hover and active
changes use color; avoid positional movement.

Inline editable fields keep the input, explanation and save action within the
same 720px field area. Show saving, error and completion where the action occurs.
Device titles are edited by clicking the name, with the displayed value prefilled.
Blur or Enter saves; Escape discards. No pencil or save/cancel controls accompany
the title. The name input is 320px wide, capped by available space.
Device traffic activity sits behind the title, aligned right and fading to
transparent toward the overlapping name and icon. Omit the visible presence chip;
retain its accessible status. Each day is one column, newest on the right, with
six four-hour dots from midnight downward. Future periods stay blank, and dot
tooltips appear immediately. A divider separates the
title from two columns of 36px desktop / 44px mobile minimum-height label/value rows: stable identity
and settings on the left, routing, priority and traffic on the right. Group uses
a clickable tag with a muted surface that opens its edit dialog.
Device traffic reuses Overview meters with visible direction labels.
At desktop widths both halves share five grid rows: Vendor/Uplink, MAC/Priority,
IP/Download, Group/Upload and Services/Total. Services shows matching configured
services as 28px icon tiles linking directly to their published webpages. A matching
28px outlined add button opens Ingress with the device prefilled. An empty list
shows only the add button. Total uses the Overview Traffic meter's neutral text
arrows (↓/↑), with the same size and weight, and byte values.
Match row padding and let wrapped content grow both sides together; feedback follows the aligned rows.
Device speed bars use half their available value width from 640px, aligned right;
on phones they retain the full width so rates and limit actions remain readable.
The device title contains a right-aligned traffic matrix occupying two thirds
of the title width: one column per day and six four-hour periods per column.
Cells stay 14px square with 3px corners. Derive the row gap from the 96px
matrix height, six cells and 1px vertical padding; use that same gap horizontally.
Fit as many complete date columns as the available width allows; wider layouts
show more dates rather than stretching cells. Keep today at the right edge,
the current-period hint and 1px inner padding.
Fade only the leftmost 20% toward overlapping identity content; keep the rest
fully visible, including zero/unknown dots. Use the shared opaque activity
palette: zero/unknown uses the empty gray, low nonzero traffic uses a stronger
neutral gray (#cfd4db light / #48515e dark), and the remaining four levels use
green intensities, using 25%, 50% and 75% of the greater of the retained
14-day four-hour peak and 1 GiB as boundaries. Green starts at 10 MiB;
boundary values keep the lighter tier. Use no decorative borders or inset rings;
cell fills and spacing define the grid. Retain visible keyboard focus. No axes,
legend, heading or helper text; tooltips provide dates, units and sample state.
Uplink and Priority lead the right column with compact segmented selectors.
Clicking each labeled speed bar opens its limit dialog; omit separate limit
fields and the QoS divider/status row.
Device policies apply automatically, without a switch. Keep QoS controls on
devices; Gateway does not expose aggregate link-rate settings. Blank device
limits mean unlimited; pending placement must remain distinct from active state.
Preserve existing confirmation and apply-preview behavior for network changes.
Group details reuse the device summary card: a 64px icon and title above a
divider, then two columns with a 64px desktop gutter and 32px stacked spacing.
Use the shared device label/value rows for parent, member counts and subgroup
tags on the left, with matching Uplink and Priority segmented controls on the
right. Keep batch apply actions and scope beside those controls, followed by
the device list in its own panel. Use the device page's 16px panel spacing.
Device detail rows use two zones: left-aligned labels and right-aligned values
or controls, with 36px desktop / 44px mobile minimum row height. Keep disclosure chevrons beside their
values rather than in a separate action column. Clicking the IP opens a dialog
with an address and fixed toggle, without a pencil. A Fixed badge follows reserved
addresses. Device traffic rates have Download and Upload labels beside narrower,
arrow-free inset meters; cumulative totals sit below Upload. Omit Last seen. Uplink sits
above Priority in the right column. Both use segmented controls with 28px desktop
buttons, 2px container padding and 44px mobile targets. Uplink applies through
the existing network-change confirmation flow. Separate the
desktop identity/settings and traffic halves with a 64px gutter; stack them with
32px spacing below desktop.
Never style a pending or failed read as a disabled service.
Active device inspection displays the embedded mitmproxy viewer directly below
the Connections divider. Keep its setup gear icon beside the title-bar switch;
the QR code and setup guide link belong in the setup dialog, not in the title bar.
DNS and Connections use matching, vertically centered 72px title bars whose
height stays fixed during inspection. Content expands below the title bar,
with a divider only while active; disable scroll anchoring on both panels.
Reserve the document scrollbar gutter to prevent horizontal movement.
DNS inspection defaults off and uses the shared table without a visible header,
with Time, Record, Type and right-aligned Answer columns.
Its search icon reveals a compact filter in the same title row, moving the
inspection controls left with a 200ms width/opacity transition that honors
reduced motion. Keep long records and answers readable.

List rows have a minimum height of 64px. On phones, trailing values reflow below
the title instead of disappearing. Technical values and important descriptions
must wrap or have an accessible path to their complete value. A secondary
action is a sibling via ListRow's `action` prop, outside the row link/button.
For Button rendered as a Link, set `nativeButton={false}`.

Dialogs use a restrained scrim, a 480px default maximum width and 16/24px
padding. Long forms must scroll within the viewport and retain a reachable
close and submit action. Empty states use a normal surface, a short explanation
and a relevant next action where one exists.

## Responsive behavior and motion

Overview has four desktop cards: Status, Traffic, Latency and Quality.
Clicking the Latency heading opens its site configuration dialog; omit an add icon.
Map and Quality chart content use half the normal card side and bottom gutters
(6px on phones, 8px on larger screens), while headings retain their normal inset.
Desktop cards target 4:3 proportions with a 240px minimum height and grow for
disclosures or extra probes. Use one column on phones, two from 640px and
four from 1280px so values and units remain on one line.
Traffic's title-right map icon toggles its statistics and embedded map views.
Keep both mounted as the front and back of one card to preserve height and map
continuity. Rotate the whole card 180 degrees around its vertical axis over
600ms, with 1200px perspective and hidden backfaces. Each face has its own
title and toggle so the map appears on the back of the card. Reduced motion
switches faces immediately. Make the inactive face inert and hidden to assistive
technology. Do not add a Map details row; clicking the map opens fullscreen.
The Quality heading and radar open the assessment dialog; omit a separate Test
button on the card. Retain measured scores without age-based fading or hiding.
Network quality uses a five-axis radar with theme-aware primary fill and border
grid lines. It occupies one card at every breakpoint. Only measured, current
axes have points; missing/stale axes show dashes and prevent a filled polygon,
except Wireless, which defaults to 80 for display. Omit the title-right total
and below-plot scope caption; retain Test aligned right. The heading, plot and Test action open the shared
scrollable details dialog. Category disclosures show score, measured scope,
normalized weight and point contributions, with raw evidence below. Keep long
errors and addresses wrapping inside the dialog.
Accessible chart descriptions identify partial and missing measurements.
Uptime is
the first plain disclosure row in Status, above system health. Both rows use
aligned 16px Lucide CircleCheck icons in success color when healthy. Uptime
opens a dialog with CPU, memory and root disk used/total values. Map has no information icon,
direction legend or top-ten summary. The
grayscale preview fills Traffic's remaining content area. Clicking it opens a
viewport-filling dialog with a pastel country map (muted colors in dark mode),
pan/zoom, direction labels, attribution and the shared close control. The local
fullscreen map reveals province/state boundaries, their names and major cities
on zoom, stopping at regional detail (zoom 6). State names and capital/world-city
names begin at zoom 3; other major cities appear at zooms 4–5. Country fill
indices are stable and checked against neighboring countries, with separate
light/dark palettes. Keep administrative labels neutral and secondary
to traffic; traffic points have no GeoIP text labels.
Place zoom and whole-world reset buttons at bottom right, with 44px phone
targets. Resizing must preserve the current fullscreen view. Escape
closes it and restores focus to the preview. On
phones each card uses the full column. Its local MapLibre map uses receive/transmit
colors and bounded logarithmic path widths. Moving particles indicate observed
traffic direction and stop for reduced motion. Use 1.4px endpoints merged at
shared coordinates. Keep particle phases stable
across ranking updates; ease line widths and fade arriving/departing paths and
points with a 450ms exponential time constant. Fade particles at path ends.
Reduced motion disables both particles and interpolation; hidden tabs pause updates.
IP points use valid returned coordinates without country-center fallbacks;
omit points without coordinates. Draw flows between different locations even
within one country. The details dialog supplies city/country, rates and ip.sb
attribution without crowding the map.

Overview group headers navigate directly to group details at every screen size;
do not expand or focus named groups in place.
Device list rows reserve space for up to three 24px service icons with 4px gaps.
Use only the space needed by the device's services; additional icons scroll horizontally.

Network-map device ordering samples current traffic ranks every five seconds.
Pause reordering while the pointer is over the map, keyboard focus is inside it,
or a touch is active. Resume at the next scheduled update rather than immediately
on leaving. Rates and counters keep their normal live refresh; do not smooth them.
New devices appear after existing rows until the next reorder; removed devices
disappear immediately. Groups retain their name order.

Firewall uses one vertical Sankey with three visual rows: external semantic
categories above, one virtual OLR / Firewall node in the middle, and trusted/LAN
and router-local semantic categories below. Both sides flow toward OLR. These
positions distinguish origins, not successive processing stages. Keep the
semantic category and packet count on each outer node; omit repeated source and
Allowed/Blocked labels. Color conveys the decision, with full text in tooltips. Use one measured
packet scale and semantic receive/transmit and success/warning colors. Reserve
label space for small nonzero paths without inflating their widths. Provide an
accessible tooltip details; narrow screens scroll inside the diagram without
causing page-wide overflow. Adapt to available width and wrap labels. When the
chart must scroll, start at its leading edge; never
automatically center it and hide the first category.

Page navigation resets document scroll to the top before paint. Hash links retain
their section-target behavior; query-only changes preserve the current scroll.

Desktop section navigation begins at 1024px. Below that, show all five sections
in a five-column bottom navigation; do not require horizontal scrolling.
Account for the device safe area and bottom navigation when laying out content
and the footer. At 375px, keep labels, statuses, addresses and action buttons
usable. Monitoring statistics use two columns on phones.

The desktop footer bottom-aligns its text and theme icon with 12px bottom padding,
keeping the visible content close to the viewport edge while retaining 44px link
and control hit targets. The footer theme control is a single icon. Clicking cycles
auto, light and dark; its accessible label and tooltip name the current and next theme.

Use 150–200ms color/opacity transitions. Motion should explain a state change;
no ambient animation except observed world traffic and the current device-activity block's traffic indicator.
Breathe faster as combined live download/upload increases, with a logarithmic
4s–1.4s cycle capped at 100 MiB/s. Use opacity 1 → 0.45 over 55% of the
cycle, rest dim for 10%, then return to full opacity over 35%. Each changing
segment uses cubic-bezier(0.37, 0, 0.63, 1), with zero endpoint slopes for
gentle reversals. Keep the cell size and palette; no glow or scale animation.
Keep a fixed animation timeline and ease playback speed toward each new target
with a 350ms exponential time constant (about one second to settle), preserving
the current phase. When idle or unavailable, finish the current cycle and pause
at full opacity; resume from that phase when traffic returns.
Idle or unavailable rates stay steady;
historical cells retain their traffic fills. Honor prefers-reduced-motion in the global stylesheet.

## Page coverage and review checklist

Review Overview; device and group details; Gateway and interface details; exits,
usage and unmanaged settings; every DNS and DHCP detail/settings page; native
Firewall; Access and SOCKS5, remote, ingress, forwards and filtering; Tools and
all five diagnostic pages; Advanced, DDNS and IPTV; administrator access;
Credits; not-found; token/password sign-in; initial setup and first-run states.
Legacy URLs should still redirect to the appropriate current page.

For every iteration:

- Check 375px, 768px and 1440px layouts, light and dark themes.
- Check long device names, IPv6 addresses, empty collections and populated data.
- Verify keyboard focus, a single clear page title and semantic navigation.
- Verify loading, unavailable and error states cannot be mistaken for healthy data.
- Open affected dialogs, verify scroll/close/submit access and avoid nested buttons.
- Keep current value, units and action visible on mobile; avoid page-wide overflow.
- Run web lint, build and tests; run the repository's Go checks.
- Capture screenshots of affected pages and states for review.

The stylesheet and shared components are the implementation source of truth.
Update this guide alongside deliberate changes to these contracts.
