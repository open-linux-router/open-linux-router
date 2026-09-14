# Device icons

One picture per category, and optionally one per vendor's take on a category.
These are shipped inside the `olrd` binary (`internal/webui/assets`), so their
size is the binary's size.

## Three tiers, one look

**Tier 1 — category icon.** One per `DeviceCategory`. Always present, and the
common case: a DHCP fingerprint or an OUI narrows a client to a *class* far more
often than to a model. Generated from text, because a generic laptop has no
ground truth to be wrong about.

"Brandless" here means no logo, no model, no brand-signature silhouette or
colourway — **not colourless**. A category icon looks like a real device in
realistic materials. Tier 1 has to look finished on its own, because tier 2 is
optional and licence-gated: a deployment with nothing but category icons must
look intentional rather than degraded. The tiers are distinguished in the UI by
the *label* — "Laptop" where only the class is known, "MacBook Air" where the
model is — and by nothing else.

**Tier 1.5 — vendor icon.** One per `<vendor>/<category>`, e.g. `apple/laptop`,
overriding the plain category icon. Named by the closed `VendorKey` vocabulary
in `internal/devices/vendor.go`, and registered under exactly that path in
`IMAGES`, where a template literal type checks both halves at compile time.

This is the one tier that **relaxes a shared rule**: brand-signature silhouette
and colourway are allowed here, because they are the entire point. Everything
else still holds, and two bans are absolute — **no logos and no text**. An Apple
laptop is a thin aluminium wedge; it is not a laptop with an apple on the lid.
Generated from text like tier 1, and legitimately so: "an Apple laptop" is a
class with a recognisable house style, not a product with a serial number.

**Vendor-only icons do not exist, and that is a decision.** A picture for "an
Apple something" has to choose between a phone, a watch, a laptop and a TV box,
which is inventing exactly the information `detect.go` refuses to invent. A
device with a vendor and no category gets its vendor's **initials** in the quiet
tile instead — see `vendorInitials` in `features/devices/icons.ts`. That also
covers all thirty thousand vendors in the IEEE registry rather than the few
dozen anyone will ever draw, which no set of assets can.

**Tier 2 — model image.** One per specific product, e.g. `synology/ds224plus`,
overriding its vendor and category icons. **Never text-to-image**: a prompt for
"Synology DS224+" yields a convincing two-bay NAS that is not a DS224+, and an
inventory whose pictures are subtly wrong is worse than one that admits it only
knows the category. Tier 2 is made by *restyling a real photograph* — the photo
fixes the silhouette and proportions, the edit normalises view, light and
finish. True product colours are kept.

The line between 1.5 and 2 is where the prompt stops being able to be right. A
house style is a real thing a text-to-image model knows; a part number is not.

None of tier 2 ships yet, and it is not wired in either: `Device.model` exists,
but `deviceIcon()` has no rung for it. Adding one is one lookup at the top.

### Licensing gates tier 2

Restyling a product photo creates a derivative of that photo. For an asset
embedded in `olrd` the source must be a manufacturer press-kit image used within
its terms, a permissively licensed image, or one we took. **Image-search results
are not a safe source.** Trademark use to *identify* a product in an inventory is
generally defensible; the photograph's copyright is the real exposure. Tier 1
has no such problem, which is another reason the product stays fully usable on
tier 1 alone.

## Resolution order

First match wins — `internal/devices/list.go`, and `deviceIcon()` in `icons.ts`:

1. **Model** — operator-set, or detected and then confirmed. *Not wired in yet.*
2. **Vendor + category** — `apple/laptop`
3. **Vendor initials** — only when the category is unknown, so there is nothing
   better to say. Not an asset; drawn from the vendor's name in the UI.
4. **Category** — operator-set, else detected
5. **`unknown`**

Two axes, each falling back on its own: a device whose vendor has no artwork
still gets its category's picture, and a category with no artwork still gets a
line glyph rather than a stand-in photograph.

Step 3 sits above step 4 only because there is no category to use. A picture of
the right *kind of thing* beats a mark of the right *brand* — someone scanning
the list is looking for their printer, not for Brother.

An operator override always beats detection. The icon is *identity*, not
presence (design.md §4.4), so it is stored config: a picture someone corrected
must not be silently changed back by the next fingerprint update.

## Shared rules

| | |
|---|---|
| **View** | Front three-quarter, rotated 25° to the left, camera 12° above |
| **Light** | Soft diffuse key from upper left, broad fill, no specular hotspots |
| **Finish** | Matte. No reflections, no gloss, no environment bounce |
| **Shadow** | None baked in — the UI supplies `drop-shadow`, so assets stay reusable on any background |
| **Framing** | Centred and upright, filling ~80% of frame, consistent optical weight across categories |
| **Master** | 1024×1024, transparent background, PNG |
| **Shipped** | 256×256 WebP, quality 0.86 |
| **Forbidden** | Logos, text, stickers, brand marks; backgrounds; ground planes; glowing status lights |

Tier 1 palette: realistic neutral device materials — matte plastic and brushed
aluminium in graphite, slate, silver or off-white; screens dark and blank.

Tier 1.5 is the one exception, and only to the *silhouette* rule: a vendor icon
may carry that vendor's proportions, materials and colourway, because
recognising them is what it is for. The row above still binds otherwise, and
"no logos, no text" binds hardest of all — the shape may say Apple, the lid may
not.

## Adding one

1. **Generate a master** at 1024×1024, transparent, with this preamble and only
   the subject line changed. Keeping it verbatim is what holds the set coherent:

   > Product render of **\<subject\>**. Single object, centred and upright,
   > filling about 80% of the frame. Front three-quarter view, rotated 25 degrees
   > to the left, camera 12 degrees above. Soft diffuse key light from the upper
   > left with broad fill, no specular hotspots. Matte finish throughout — no
   > reflections, no gloss, no environment bounce. No shadow, no ground plane, no
   > background: fully transparent background. Realistic neutral device
   > materials: matte plastic and brushed aluminium in graphite and slate grey.
   > Generic industrial design — absolutely no logos, no text, no stickers, no
   > brand marks, no brand-signature silhouette or colourway.

   For a **tier 1.5** vendor icon, swap the last sentence — and only that
   sentence — for the vendor's house style, keeping every other line verbatim so
   the two tiers sit together in a list:

   > \<Vendor\>'s industrial design language: \<proportions, materials,
   > colourway\>. No logos, no text, no stickers, no brand marks of any kind.

2. **Optimise** it into this directory, named exactly after the category, or
   `<vendor>-<category>` for a vendor icon:

   ```
   node scripts/optimize-icons.mjs <dir-of-masters>
   ```

   Chromium does the resize and encode; no image library is needed. Masters are
   not committed — they are ~1 MB each and this file is how they are reproduced.

3. **Register it** — one line in `IMAGES` in `../../features/devices/icons.ts`.
   A vendor icon is registered under its slash form, `'apple/laptop': appleLaptop`,
   which is what the `IconKey` type checks; the file itself cannot contain a
   slash, hence the hyphen on disk.

Nothing here is required. A category with no image falls back to its line glyph
while still showing its own correct label; a vendor with no image falls back to
the plain category icon; a device with neither falls back to its vendor's
initials. Every gap has a deliberate answer, which is why the set can be filled
in one file at a time and has been.
