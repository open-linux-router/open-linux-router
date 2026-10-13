# UI visual style guide

This is the shared reference for future UI changes. The direction is **a precise,
calm network console with lightly tangible surfaces**. Clarity of network state
comes first; polish comes from alignment, typography, restrained depth and
consistent interaction. Keep the existing Geist typeface, router identity and
device imagery.

For information hierarchy, wording and progressive disclosure, follow the
[content and presentation standards](ui-content-guide.md).

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

## Layout and hierarchy

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
Only download/upload rates and CPU/memory utilization use inset meter surfaces
in Overview cards. Status, public addresses, latency, totals and uptime use plain
rows; monitored sites retain their icons without capsule backgrounds.
Cumulative traffic rows use Download and Upload labels with plain byte values.
On desktop, Overview cards share title, primary metric section and supplementary
section boundaries, not individual metric rows. Totals, site probes and uptime
start together without divider lines, then flow independently. Status spans the
content sections naturally. Below desktop, cards use their own content height.
Site probes use two columns on phones and three from 640px.
The public-network summary uses a locally bundled flag-icons country flag and
one chevron. Expanded addresses use monospace and wrap within the Status card.

## Typography and imagery

Use Geist Variable with system sans-serif fallback. Page titles are 24px/32px,
weight 600. Panel titles are generally 16px, weight 600. Body and controls are
14px; descriptions and technical metadata are 13–14px with relaxed line height.
Use 12px only for compact auxiliary metadata. Mobile text inputs are 16px to
avoid browser zoom. Use tabular numerals for live rates and counters, and
monospace for addresses, commands and configuration.

Device imagery identifies device categories. Tool directory entries use Lucide
outline icons with a 1.75px stroke in a themed 56px surface (48px on phones),
an 18px name and one directional chevron. Avoid raster illustrations with baked-in
backgrounds. Overview metric headings have the same 24px line box; auxiliary
icons extend their hit area without increasing the heading height. Descriptions and instructions
belong on the individual tool page. No decorative glows or additional gradients are needed.
Use Lucide consistently for functional icons, normally 16–20px. The two-color
brand mark retains its own colors in both themes.

## Controls, lists and dialogs

Use the shared Button, Input, Select, Textarea, Switch, Card and Dialog components.
Regular controls are 36px high on desktop; mobile button/input/select targets
are at least 44px high. Switch visuals are 44 × 24px with an expanded mobile hit
area. Focus uses the primary ring and must remain visible. Hover and active
changes use color; avoid positional movement.

Inline editable fields keep the input, explanation and save action within the
same 720px field area. Show saving, error and completion where the action occurs.
Device titles are edited by clicking the name, with the displayed value prefilled.
Blur or Enter saves; Escape discards. No pencil or save/cancel controls accompany
the title. Its status chip uses the title font size. A divider separates the
title from two columns of 48px minimum-height label/value rows: stable identity
and settings on the left, observed state and traffic on the right. Priority and
limits use the same row rhythm; long values and open editors may expand rows.
Preserve existing confirmation and apply-preview behavior for network changes.
Device detail rows use two zones: left-aligned labels and right-aligned values
or controls, with 48px minimum row height. Keep disclosure chevrons beside their
values rather than in a separate action column. Fixed IP address has its own
switch row. Traffic rates sit below totals in the same value zone. Separate the
desktop identity/settings and traffic halves with a 64px gutter; stack them with
32px spacing below desktop.
Never style a pending or failed read as a disabled service.

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

Page navigation resets document scroll to the top before paint. Hash links retain
their section-target behavior; query-only changes preserve the current scroll.

Desktop section navigation begins at 1024px. Below that, show all six sections
in a six-column bottom navigation; do not require horizontal scrolling.
Account for the device safe area and bottom navigation when laying out content
and the footer. At 375px, keep labels, statuses, addresses and action buttons
usable. Monitoring statistics use two columns on phones.

The desktop footer bottom-aligns its text and theme icon with 12px bottom padding,
keeping the visible content close to the viewport edge while retaining 44px link
and control hit targets. The footer theme control is a single icon. Clicking cycles
auto, light and dark; its accessible label and tooltip name the current and next theme.

Use 150–200ms color/opacity transitions. Motion should explain a state change;
no ambient animation. Honor prefers-reduced-motion in the global stylesheet.

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
