# UI content and presentation standards

The visual direction is a precise, calm network console. Content must support
that direction: users should identify the important information before reading
supporting text. This guide complements [the visual guide](ui-style-guide.md).

## Start with the user's decision

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
to make a page appear quieter.

- Keep the Overview's four metric cards, CPU, memory and uptime visible.
- Keep download and upload limits beside their respective measurements; separate
  controls serve separate decisions and are not automatically redundant.
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

Show QoS as saved configuration until enforcement exists. Preserve diagnostic
scope: router-originated tests do not measure a downstream device's path.
Inspection requires explicit consent about credentials and certificate trust.
Do not promise encrypted DNS blocking beyond the supported protocol and port.

Use download then upload consistently. Device names may supplement an address
only when the address uniquely identifies a device. Keep long domains, IPv6
addresses and provider errors readable rather than silently truncating them.

## Disclosure and hierarchy

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
