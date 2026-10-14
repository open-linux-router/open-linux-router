# UI content and presentation standards

The visual direction is a precise, calm network console. Content must support
that direction: users should identify the important information before reading
supporting text. This guide complements [the visual guide](ui-style-guide.md).

## Start with the user's decision

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

- Keep the Overview's four metric cards, CPU, memory and uptime visible.
- Overview Status shows one public-network summary using IPv4's country flag,
  city and ISP. Its disclosure shows IPv4, IPv6 when available, ASN and ASN
  Organization from the IPv4 lookup, in that order.
- Keep download and upload limits beside their respective measurements; separate
  controls serve separate decisions and are not automatically redundant.
- Overview cumulative traffic uses Download then Upload labels, each with
  its byte unit. Preserve unavailable states rather than
  displaying zero when counting is disabled or a read fails.
- Group Overview live traffic and utilization separately from cumulative traffic,
  site probes and uptime. Align section starts across desktop cards while keeping
  each group's internal spacing suited to its content; Status remains one list.
- Keep common configuration fields, addresses, counters and useful evidence
  visible. Do not move them into disclosures solely to shorten a page.
- Reuse existing headings, Card, ListRow, StatusStrip, fields and Dialog. Do not
  invent a compact card, new density tier or special layout for each feature.
- Disclose lengthy technical explanations and infrequently used advanced options.
  Keep prerequisites and consequences beside the action they affect.
- A page can pass review without a visible redesign. Preserve a coherent existing
  presentation when its content already supports the user's task.

## State and data contracts

Distinguish loading, read failure, unknown, empty, disabled and healthy states.
No response is not an empty rule set; saved configuration is not proof of a
running service or end-to-end connectivity. Use neutral pending/unknown styling,
an explicit error and a retry where useful. Name configuration counts as such.

Firewall monitoring retains semantic permission categories and adds incoming
source as a separate dimension. In one vertical diagram, place external sources
above shared decisions and trusted/local sources below them; both flow through
semantic categories toward those decisions. Hide zero-traffic categories and
sources. Sankey widths use one shared scale of measured
packet counts, with byte totals in tooltips and matching conditions. State that
counts reflect the first matching rule and reset when rules are rewritten.
Keep unavailable counters distinct from a verified zero-traffic state.

Show QoS as saved configuration until enforcement exists.
Device details group stable identity and saved settings on the left, live
uplink, priority and live traffic on the right. Reuse Overview rate meters with
Download and Upload labels beside narrower bars, without arrows or cumulative
total rows. Omit Last seen. Group is a tag that opens its edit dialog.
Compact Uplink and Priority segmented selectors lead the right column, in that
order. Uplink selections retain network-change confirmation and apply feedback.
The title's activity matrix shows one day per column, newest
on the right, and six four-hour periods from top to bottom. The displayed date
range adapts to the available width, with fixed square cells; dates without
retained history remain explicitly missing data. Leave future periods
blank and show download/upload totals immediately on hover or focus. Format the
time as `10-14 4:00-8:00`, followed by traffic only; omit sampled/progress wording.
Keep presence available to assistive technology without a visible title badge.
Speed bars open download/upload limit dialogs; keep the
configuration-only caveat in those dialogs and save feedback, without a status row.
Omit Notes and the QoS heading.
Device activity sits at the right of the title bar, without visible
explanatory text. Immediate four-hour hover/focus hints show sampled download and upload
or explicitly report missing/unavailable data. Partial samples are not exact
four-hour totals. History is joined to the device's currently observed addresses.
It begins accumulating after daemon startup. Sampling and UI
refresh run every ten seconds; the first reading establishes a baseline.
History retains at most 14 days and is restored from
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

Gateway groups physical interface cards inside an Interfaces panel, with observed
default gateways and host DNS on the right at desktop widths. Keep IPv4 and IPv6
forwarding controls and the routing details link in that summary; stack it below
interfaces on smaller screens. IPv4 also controls Gateway routing and leaves
system forwarding unchanged when off; IPv6 intent is unapplied when routing is
off. Usage has no separate page; traffic counting lives in routing details.

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

## Naming and writing

Use familiar tool names and concise task labels. Keep distinctions in the name
when they help selection: Internet speed test and LAN link test identify different
measurement boundaries. Preserve established names such as Ping, NAT test and
Traceroute. Do not invent broad category labels for a single item.

Avoid repeating the title in a description, stating obvious click behavior or
adding a generic next-step sentence. Use one short sentence when an explanation
is necessary. Keep essential text readable; reducing font size is not a remedy
for excessive content.

Keep the footer to the product copyright, version, GitHub, Credits and an icon-only
theme control. On desktop, bottom-align the visible content with 12px bottom padding.
Describe the current and next theme in its accessible label and tooltip.

## Capability and richness

Show the real capability set together with consistent visual weight. Keep entries
compact enough to scan across the set. Richness comes from recognizable, distinct
capabilities, useful data and dependable interactions. Do not inflate it with
oversized panels, redundant labels, decorative badges or unavailable features.

Use a consistent ordering. Add grouping when the collection is large enough that
groups reduce search effort; add search when scanning the collection becomes
burdensome. Five tools need neither filters nor one category per card.

## Tools directory reference

The Tools page's goals are quick selection, a visible range of OLR capabilities
and a calm presentation. Its current contract is:

- Show all five real tools in a uniform directory.
- Each entry contains a recognizable Lucide outline icon, one tool name and a chevron.
- The whole entry is a keyboard-accessible link.
- No category, paragraph, instruction footer or generic page subtitle.
- Keep full names visible on phones; retain contrast and focus in both themes.
- Keep detailed explanations and requirements on the individual tool pages.

## Review before shipping

- Can the primary information be identified without reading every paragraph?
- Does each supporting line change a decision or explain a necessary constraint?
- Is any information repeated across title, description, badge and action?
- Are instructions shown at the step where they are used?
- Does simplifying preserve warnings, measurement scope and unavailable states?
- Can users scan the capability set on desktop and find entries on a phone?
- Do imagery, whitespace and type reinforce the same information hierarchy?

Update both guides when changing a shared presentation contract.
