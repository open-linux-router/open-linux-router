# Control language audit — state, switch, button, more, edit

**Date:** 2026-10-21 · **Scope:** every route in `web/src/routes` and the shared
components in `web/src/components`. Line numbers refer to the tree this audit
was taken on (`9526a91` plus uncommitted Overview work) and drift as the
migration lands; the files and symbols named beside them do not. **Question:** do the five everyday control atoms — a state, a
switch, a button, a "more"/drill-in affordance and an "edit" affordance — speak
one language across the product?

**Answer:** no. Each atom is individually reasonable and individually
re-invented. The page-level prose contracts in
[the visual guide](ui-style-guide.md) are detailed about *layout* and mostly
silent about these five atoms, so every feature area settled them locally. The
proposal that follows this record is in
[the control language contract](ui-control-language.md).

## Summary

| # | Finding | Evidence |
| --- | --- | --- |
| 1 | Five independent status vocabularies, each owning its own tone map | `routes/overview.tsx:473`, `features/gateway/status-tone.ts:9`, `features/link/interface-visual.tsx:8`, `components/layout/status-strip.tsx:36`, `features/gateway/summary.tsx:194` |
| 2 | The same fact wears different clothes, and the same word wears different tones | "Running" is success in one card and neutral in the next; "Unavailable" is red on Overview and amber on Gateway |
| 3 | Three dot sizes, five neutral opacities, 27 tone class strings | `size-1.5` / `size-2` / `size-2.5`; `bg-muted-foreground/40 · /45 · /50`; `bg-success/70` |
| 4 | `Badge` is the status pill, the tag, the measurement and the drill-in label | `peer-badge.tsx:20`, `forward-list.tsx:56`, `group.tsx:92`, `interfaces-card.tsx:285` |
| 5 | Five switch placements, no visible label on the two most important ones, a dead `size` prop, and "applying" rendered as "disabled" | `switch.tsx:9`, `remote.tsx:267`, `socks-card.tsx:66`, `dns/index.tsx:55` |
| 6 | Button hierarchy is per-page: `Add` is primary on five pages and outline on five others; `Cancel` is ghost or outline by file; "Apply anyway" is painted destructive | `dhcp/ranges.tsx:44` vs `gateway/exits.tsx:23`; `nat/editor.tsx:105` |
| 7 | One chevron means "go to a page" and "open an editor"; only one list in the product has a real "more" menu | `list.tsx:39` (`to`) vs `list.tsx:40` (`onSelect`), `topology/nodes.tsx:412` |

## 1. Status is a local decision in every area

There is no shared status component. `docs/ui-semantic-components.md:53` still
records it as *"Proposed StatusIndicator"*, and the five areas that needed one
built their own:

| Owner | Shape it invented | Tone vocabulary |
| --- | --- | --- |
| `components/layout/status-strip.tsx` | Card, `size-2.5` dot, sentence headline, one switch, drift row | caller passes a background class string: `dot: string` |
| `features/gateway/status-tone.ts` | 6px dot + tinted word inside cards | `good · caution · quiet`, exported `dot`/`tone` maps |
| `routes/overview.tsx` | 8px verdict dot, then 6px module dots + muted words | inline ternaries: `bg-success/70`, `bg-destructive`, `bg-warning`, `bg-muted-foreground/50` |
| `routes/dhcp/index.tsx`, `routes/dns/index.tsx` | a hand-copied strip (`rounded-xl border bg-card p-5`) with the same `size-2.5` dot | local `describeStatus()` returning `{headline, detail, dot}` |
| `routes/advanced/{ddns,caddy,forwards,iptv}.tsx` | seven-branch `dot={…}` expression evaluated at the call site | raw classes |

The strip's own doc comment says it "was written three times before this
(dhcp, dns and gateway each had their own)" — it is now written five times, and
the two copies in `dhcp/index.tsx:43-59` and `dns/index.tsx:41-57` are the same
markup the strip renders, differing in padding (`p-5` vs `p-4 sm:p-6`), dot size
and the fact that they add rows the strip has no slot for.

Two details in the strip's contract have not survived:

- `dot: string` is a leaky prop. Every caller writes a Tailwind class, so the
  same tone is reachable four ways, and `bg-success/70` (Overview) and
  `bg-success` (Gateway) are both "healthy".
- `size-2.5` (10px) in the strip, `size-2` (8px) on interface cards, stage
  cards at `size-1.5` (6px). Three glyph sizes for one meaning.

## 2. The same fact, dressed differently

This is the part that reads as "a different habit per page". Each row is a real
pair of screens in the product today.

| Fact | One screen says | Another screen says |
| --- | --- | --- |
| A service is running | WireGuard: **success** pill "Running" (`remote.tsx:312`) | SOCKS5: **neutral** pill "Running" (`proxy-card.tsx:10`) |
| A module cannot be read | Overview: **red** dot + "Unavailable" (`overview.tsx:473`) | Gateway stack: **amber** word "Unavailable" (`stacks.tsx:111`) |
| A section is switched off | DHCP strip: **neutral** dot, headline "Off" (`dhcp/index.tsx:273`) | WireGuard: **neutral** pill "Off" (`remote.tsx:309`) |
| A peer is connected | Device list: **success** pill "Connected" (`peer-badge.tsx:21`) | Map band: 8px dot, word in the accessible name only (`design.md`/style guide) |
| A link has an address | Interface card: 8px dot + "Connected" (`interface-visual.tsx:13`) | Interface row inside the same page: 8px dot + a *badge* for the exceptional states only (`interfaces-card.tsx:285`) |
| "Waiting for a first result" | DDNS: neutral pill "Waiting" (`ddns.tsx:181`) | IPTV: amber dot "needs attention" (`iptv.tsx:96`) |

`features/gateway/summary.tsx:202` even keeps a *separate* state enum
(`'Connected' | 'Disconnected' | 'Down' | 'Not found' | 'Inactive'`) that is
translated at the draw site and never reconciled with the other four enums.

## 3. `Badge` is doing four jobs

| Job | Example | Problem |
| --- | --- | --- |
| Status of a thing | `Running`, `Off`, `Never connected`, `Expired` | competes with dot+word used for the same job elsewhere |
| A category/tag | `LAN`, `Blocking`, `Fixed`, group names (`group.tsx:92`) | correct use, but visually identical to status |
| A measurement | `{formatBytes(row.bytes)} in` as **success** (`forward-list.tsx:56`), `Valid for 30 days` | a value that is not a verdict wears a verdict's colour |
| A verdict word | grade pill hand-rolled as a `<span className="rounded-full …">` (`network-quality.tsx:107`) | same meaning as `success` badge, built outside the component |

`Badge`'s `success`/`warning`/`destructive` variants exist only because status
needed a pill; `secondary` is overloaded to mean both "off" and "unknown" and
"not counted" and "group name".

## 4. Switches: five placements, one of them unlabelled

| Placement | Where | Notes |
| --- | --- | --- |
| Card header, right edge | WireGuard (`remote.tsx:269`), SOCKS5 (`socks-card.tsx:70`) | no visible label; the only word is the status pill |
| Strip right edge | `status-strip.tsx:78` | `sr-only` label from the strip contract |
| `SwitchField` panel | `editable-field.tsx:106`, five DNS/gateway pages | label + hint, own panel surface |
| Ad-hoc `<label>` wrapper | `dns/index.tsx:55` ("Keep a query log") | inline text, 8px gap, different hit area |
| Bare in a list row | `interfaces-card.tsx:317` | `sr-only` label from a sibling `<Label>` |

Other findings:

- `switch.tsx:9` accepts `size?: 'sm' | 'default'` and writes `data-size`, but no
  class or CSS reads it. The variant is inert; nothing passes it.
- `disabled={busy}` is used for "applying" everywhere. The visual guide already
  forbids conflating pending with disabled for reads; it happens for writes too,
  so an applying switch is 50% opacity and unreachable, which is the same picture
  as a switch the router will refuse.
- Off is drawn as neutral, amber or red depending on the page (`dhcp` neutral,
  `forwards` neutral, WireGuard white "Not running" red, `gateway/interfaces`
  warning). The guide's own rule — "Off is not a fault" — is only obeyed in some
  places.

## 5. Buttons: same action, different weight

Counts over `web/src` (225 `<Button>` call sites):

| variant | count | | size | count |
| --- | --- | --- | --- | --- |
| outline | 99 | | default | 141 |
| (primary) | 62 | | sm | 66 |
| ghost | 43 | | icon / icon-sm | 7 / 7 |
| destructive | 19 | | icon-lg / lg | 2 / 2 |
| secondary | 2 | | | |

- **`sm` is not smaller.** `button.tsx:26` gives `sm` `h-9` — the same 36px as
  `default`. It only shrinks text to 0.8rem and padding to `px-2.5`, so 66 call
  sites asked for a smaller control and got a shorter label.
- **`icon` and `icon-sm` are the same size** (`size-9 min-w-11 sm:min-w-0`); they
  differ only by a radius clamp, so the choice between them is arbitrary.
- **`Add` has no fixed rank.** Primary on DHCP ranges/reservations, DNS
  names/blocking (`dhcp/ranges.tsx:44`) and Gateway networks; outline on exits,
  DDNS, forwards, Caddy and WireGuard devices. The same verb, two hierarchies,
  decided per page.
- **`Cancel` has two styles**: `ghost` in DHCP/DNS/QoS dialogs, `outline` in
  ingress, NAT, dial, gateway and link dialogs.
- **`Remove` has two styles**: `variant="destructive"` (`pool-dialog.tsx:334`)
  and `variant="ghost" className="text-destructive"`
  (`ingress/service-dialog.tsx:319`).
- **"Apply anyway" is painted as a destructive action** in five editors
  (`nat/editor.tsx:105`, `dial/uplink-card.tsx:415`, `gateway/editor.tsx:132`,
  `dhcp/editor.tsx:117`, `dns/editor.tsx:131`) and once as
  "Take it back anyway" (`interfaces-card.tsx:433`). It deletes nothing; it
  accepts a plan warning. Red here teaches the operator that red means "continue".
- **Icon sizes inside buttons** are set four ways: inherited 16px, explicit
  `size-4`, `size-5`, and `size-3.5`.
- **Row actions** are `size="icon"` + `text-muted-foreground` on the WireGuard
  list, `size="sm" variant="ghost"` for Caddy's external link, `size="icon-sm"`
  for the topology menu, `size="icon-sm"` in Overview's site editor, and a plain
  `<button>` for the Overview map toggle — five hit-area conventions for one
  kind of control.

## 6. "More" is two different verbs behind one glyph

`ListRow` renders the *same* `ChevronRight` (`list.tsx:36`) whether the row is a
link (`to`) or a button that opens an editor (`onSelect`):

| Row | Behaviour | Glyph |
| --- | --- | --- |
| WireGuard device (`remote.tsx:337`) | navigates to the device page | chevron |
| Port forward (`forward-list.tsx:29`) | opens an edit dialog | chevron |
| DHCP range, reservation, DNS name, blocking policy, network, exit, DDNS record | opens an edit dialog | chevron |

Glyph geometry and opacity are also local: `size-4`/`size-3.5`, full /
`text-muted-foreground/50` (`summary.tsx:163`) / `/60` (`stacks.tsx:414`),
rotating (`disclosure.tsx:29`) or static, inside the link or a sibling 36px
button (`interfaces-card.tsx:314`).

A true **more** menu exists exactly once — groups in the topology tree
(`nodes.tsx:412`, `DropdownMenu` with View / Rename / New group inside / Delete).
Everywhere else, secondary actions are either hidden behind the row-dialog or
piled up as separate icon buttons (`Overview.tsx:651-660` has three adjacent
icon buttons per site row with `↑ ↓` text glyphs).

## 7. Edit has four grammars

| Grammar | Example |
| --- | --- |
| Click the name, type inline (no pencil) | device title (`device-inline-editor.tsx`) — documented exception |
| Pencil in the row, outside the link | WireGuard device (`remote.tsx:346`) |
| The whole row opens the editor, chevron included | forwards, exits, DNS names, DHCP ranges |
| The value is a control (segmented / select / tag) | device uplink and priority, group tag, fixed-IP dialog |

The consequence is that "where do I change this" cannot be learned once. A
device's name is edited by clicking it, a WireGuard device's name by a pencil, a
port forward's by clicking anywhere on the row, and a group's by clicking a tag
that is a button — four answers, one product.

## What is already right, and must survive

- `design.md` §5.4's split — *drift* versus *backend liveness* — is the correct
  semantic base; any shared state model must keep both facts, not average them.
- Three states, not two: "we could not tell" is a first-class answer
  (`status-strip.tsx:19-21`).
- Never a dot without a word (`status-strip.tsx` comment, and the guide's
  accessibility rule).
- Domain words are not interchangeable: DHCP's "disruptive" and DNS's
  "disruptive" are different events (`features/plan/plan-preview.tsx:12-20`).
  The **tone** can be shared; the **word** must not be.
- `ImpactBadge` (`features/plan/plan-preview.tsx:52`) already demonstrates the
  right shape for a shared tone map: a typed `Record<…, {label, hint, variant}>`.
- `SwitchField` (`editable-field.tsx:106`) is the right container; it just is not
  used consistently.

## Counts used above

| Measure | Value |
| --- | --- |
| Tone-bearing class strings outside the primitives | 27 distinct |
| Dot sizes for a status glyph | 3 |
| Neutral opacities for the "no state" dot | 5 (`/40 /45 /50 /80`, `stroke` variants) |
| Status renderers that own a tone map | 5 |
| `Switch` call sites / files | 16 / 15 |
| `<Button>` call sites | 225 |
| `Badge` call sites | 51 |
| `ListRow` rows: navigate / open a dialog / static | 3 / 9 / 1 |
| "More" menus in the product | 1 |
| Edit grammars | 4 |
