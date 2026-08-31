# Device icons

One picture per category. These are shipped inside the `olrd` binary
(`internal/webui/assets`), so their size is the binary's size.

## Two tiers, one look

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

**Tier 2 — model image.** One per specific product, e.g. `synology/ds224plus`,
overriding its category icon. **Never text-to-image**: a prompt for "Synology
DS224+" yields a convincing two-bay NAS that is not a DS224+, and an inventory
whose pictures are subtly wrong is worse than one that admits it only knows the
category. Tier 2 is made by *restyling a real photograph* — the photo fixes the
silhouette and proportions, the edit normalises view, light and finish. True
product colours are kept.

None ship yet. `Device.model` and the resolution order exist, so they are a
drop-in.

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

1. **Model** — operator-set, or detected and then confirmed
2. **Category** — operator-set, else detected
3. **`unknown`**

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

2. **Optimise** it into this directory, named exactly after the category:

   ```
   node scripts/optimize-icons.mjs <dir-of-masters>
   ```

   Chromium does the resize and encode; no image library is needed. Masters are
   not committed — they are ~1 MB each and this file is how they are reproduced.

3. **Register it** — one line in `IMAGES` in `../../features/devices/icons.ts`.

A category with no image is not broken: it falls back to `unknown.webp` while
still showing its own correct label. That is deliberate, and it is why the set
can be filled in gradually.
