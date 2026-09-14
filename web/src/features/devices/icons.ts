import {
  Bell,
  BookOpen,
  Bot,
  CircuitBoard,
  Gamepad2,
  HardDrive,
  Hexagon,
  HelpCircle,
  Laptop as LaptopGlyph,
  Lightbulb,
  Monitor,
  Network,
  Plug,
  Printer as PrinterGlyph,
  RadioReceiver,
  Router as RouterGlyph,
  Server,
  Smartphone,
  Speaker,
  Tablet as TabletGlyph,
  Thermometer,
  Tv as TvGlyph,
  Video,
  Watch,
  Wifi,
  type LucideIcon,
} from 'lucide-react'

import type { VendorKey } from '@/lib/api-types'
import type { DeviceCategory } from '@/lib/config-types'

import laptop from '@/assets/device-icons/laptop.webp'
import nas from '@/assets/device-icons/nas.webp'
import phone from '@/assets/device-icons/phone.webp'
import printer from '@/assets/device-icons/printer.webp'
import router from '@/assets/device-icons/router.webp'
import tablet from '@/assets/device-icons/tablet.webp'
import tv from '@/assets/device-icons/tv.webp'
import unknown from '@/assets/device-icons/unknown.webp'

// The picture, word and glyph maps.
//
// LABELS and GLYPHS are keyed by DeviceCategory, which is *generated* from the
// Go enum (config-types.ts, via the schema in internal/devices/schema.go). The
// maps are typed differently on purpose, and the difference is the whole
// design:
//
//   LABELS is Record<DeviceCategory, string>   — total, checked at compile time
//   IMAGES is Partial<Record<IconKey, …>>      — sparse, filled in over time
//
// So adding a category in Go and regenerating breaks `npm run build` until it
// has a *word*, but never until it has a *picture*. That is what lets the icon
// set grow gradually while the list stays correct: an unillustrated category
// shows the fallback image beside its own proper label, rather than showing the
// right picture for the wrong thing or the string "accesspoint".
//
// Adding an image is therefore one file and one line here. Nothing else moves.
//
// IMAGES is keyed more finely than the other two, by IconKey rather than by
// category, because a vendor narrows a picture where a category cannot: a
// laptop is a laptop, but an Apple laptop is a specific-looking object. Only
// pictures gain from that. A *label* for `apple/laptop` would be worse than
// "Laptop" beside a vendor name the row is already showing, and a vendor has no
// line glyph at all. See deviceIcon for the order they resolve in.
//
// Imported through vite rather than referenced from public/, so the assets are
// content-hashed into assets/ and get the immutable cache header that
// internal/webui/webui.go already sets for that prefix. It also means a missing
// file is a build error rather than a 404 nobody notices.

/**
 * A key into IMAGES: a category on its own, a vendor's take on one, or a vendor
 * with nothing else known about the device.
 *
 * The template literal type is doing real work. `apple/laptop` is checked
 * against both vocabularies at compile time, so a key cannot name a vendor that
 * does not exist or a category that has since been renamed. For strings that
 * are also filenames, that is the difference between a build error and a
 * picture which silently never loads.
 */
type IconKey = DeviceCategory | VendorKey | `${VendorKey}/${DeviceCategory}`

const IMAGES: Partial<Record<IconKey, string>> = {
  laptop,
  nas,
  phone,
  printer,
  router,
  tablet,
  tv,
  unknown,
}

/**
 * The operator-facing word for a category.
 *
 * Total by construction. If this stops compiling, a category was added to the
 * Go enum and needs a word here — which is the intended prompt, not an
 * obstacle.
 */
export const LABELS: Record<DeviceCategory, string> = {
  '': 'Not set',
  unknown: 'Unknown',

  phone: 'Phone',
  tablet: 'Tablet',
  laptop: 'Laptop',
  desktop: 'Desktop',
  watch: 'Watch',
  ereader: 'E-reader',

  tv: 'TV',
  speaker: 'Speaker',
  console: 'Games console',

  camera: 'Camera',
  doorbell: 'Doorbell',
  thermostat: 'Thermostat',
  sensor: 'Sensor',
  plug: 'Smart plug',
  light: 'Light',
  vacuum: 'Vacuum',

  printer: 'Printer',
  nas: 'Network storage',
  server: 'Server',
  sbc: 'Single-board computer',

  router: 'Router',
  accesspoint: 'Access point',
  switch: 'Network switch',
  hub: 'Smart home hub',
}

/**
 * A line glyph per category, for the ones with no photograph yet.
 *
 * This is what makes a partly-filled icon set look deliberate instead of
 * broken. The alternative — falling back to the unknown *photograph* — puts the
 * same grey box on a doorbell, a speaker and a smart plug, so a list of eight
 * devices reads as a rendering failure and the picker becomes unusable: a dozen
 * identical tiles distinguished only by their caption.
 *
 * A glyph is honestly a different kind of thing. It says "no picture yet" while
 * still telling the operator what the row is, and it never impersonates a
 * device we do not have artwork for.
 *
 * Total by construction, like LABELS: a new category needs a glyph, and gets a
 * usable row the moment it exists rather than when someone gets round to
 * rendering it.
 */
export const GLYPHS: Record<DeviceCategory, LucideIcon> = {
  '': HelpCircle,
  unknown: HelpCircle,

  phone: Smartphone,
  tablet: TabletGlyph,
  laptop: LaptopGlyph,
  desktop: Monitor,
  watch: Watch,
  ereader: BookOpen,

  tv: TvGlyph,
  speaker: Speaker,
  console: Gamepad2,

  camera: Video,
  doorbell: Bell,
  thermostat: Thermometer,
  sensor: RadioReceiver,
  plug: Plug,
  light: Lightbulb,
  vacuum: Bot,

  printer: PrinterGlyph,
  nas: HardDrive,
  server: Server,
  sbc: CircuitBoard,

  router: RouterGlyph,
  accesspoint: Wifi,
  switch: Network,
  hub: Hexagon,
}

/**
 * The photograph for a device, or undefined when nothing has been drawn for it
 * yet.
 *
 * Two axes, each falling back independently — most specific first:
 *
 *   1. `apple/laptop`  both known, and the picture can be both
 *   2. `apple`         the vendor, when nothing says what kind of thing it is
 *   3. `laptop`        the category, when the vendor has no artwork
 *   4. undefined       neither, and the caller decides
 *
 * Rung 2 sits above rung 3 only when there is no category to use, which is why
 * it is reached through `category === 'unknown'` rather than by ordering alone.
 * A picture of the right *kind of thing* beats one of the right *brand*: an
 * operator scanning a list is looking for their printer, not for Brother.
 *
 * Rung 2 is also the one that earns the whole mechanism. A network with DHCP
 * off announces no hostnames, so nothing is categorised and every row collapses
 * onto rung 4 — the identical grey box, six times over. A vendor is the only
 * thing known about those devices, and it is enough to tell them apart.
 *
 * Undefined rather than a fallback image, so the caller has to decide what to
 * show; that decision is what keeps an unillustrated device looking intentional
 * rather than broken. See DeviceIcon.
 *
 * Per-model artwork (ICONS.md tier 2) would sit above all of these. It has no
 * assets and no key vocabulary yet, so it is not wired in — adding it means one
 * more rung at the top and nothing else.
 */
export function deviceIcon(category: DeviceCategory, vendor?: VendorKey): string | undefined {
  if (vendor) {
    const both = IMAGES[`${vendor}/${category}`]
    if (both) return both

    // The vendor alone, but only when the category would contribute nothing.
    // 'unknown' and '' are both "nobody has said" — see DeviceCategory in
    // internal/devices/category.go, where the two differ in provenance but not
    // in how much they tell a picture.
    if (category === 'unknown' || category === '') {
      const own = IMAGES[vendor]
      if (own) return own
    }
  }
  return IMAGES[category]
}

// hasOwnIcon(category) used to live here and had no callers. Rather than grow
// it a vendor argument nobody would pass, it is gone: `deviceIcon(...) !==
// undefined` is what it was, and is shorter than importing it.

/** The line glyph for a category, used wherever there is no photograph. */
export function deviceGlyph(category: DeviceCategory): LucideIcon {
  return GLYPHS[category] ?? HelpCircle
}

/** The word for a category, for anywhere a label is needed. */
export function categoryLabel(category: DeviceCategory): string {
  return LABELS[category] ?? LABELS.unknown
}

/**
 * The vocabulary in the order the picker shows it, grouped the way an operator
 * thinks about a network rather than alphabetically. Mirrors the order in
 * internal/devices/category.go.
 *
 * "Not set" is omitted: clearing a category is a distinct action ("Use the
 * detected category"), not a value to pick from a list.
 */
export const CATEGORY_GROUPS: { label: string; categories: DeviceCategory[] }[] = [
  { label: 'Personal', categories: ['phone', 'tablet', 'laptop', 'desktop', 'watch', 'ereader'] },
  { label: 'Media', categories: ['tv', 'speaker', 'console'] },
  {
    label: 'Home',
    categories: ['camera', 'doorbell', 'thermostat', 'sensor', 'plug', 'light', 'vacuum'],
  },
  { label: 'Computing', categories: ['printer', 'nas', 'server', 'sbc'] },
  { label: 'Network', categories: ['router', 'accesspoint', 'switch', 'hub'] },
  { label: 'Other', categories: ['unknown'] },
]
