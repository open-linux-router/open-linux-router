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

import type { DeviceCategory } from '@/lib/config-types'

import laptop from '@/assets/device-icons/laptop.webp'
import nas from '@/assets/device-icons/nas.webp'
import phone from '@/assets/device-icons/phone.webp'
import printer from '@/assets/device-icons/printer.webp'
import router from '@/assets/device-icons/router.webp'
import tablet from '@/assets/device-icons/tablet.webp'
import tv from '@/assets/device-icons/tv.webp'
import unknown from '@/assets/device-icons/unknown.webp'

// The category → picture and category → word maps.
//
// Both are keyed by DeviceCategory, which is *generated* from the Go enum
// (config-types.ts, via the schema in internal/devices/schema.go). The two maps
// are typed differently on purpose, and the difference is the whole design:
//
//   LABELS is Record<DeviceCategory, string>       — total, checked at compile time
//   IMAGES is Partial<Record<DeviceCategory, …>>   — sparse, filled in over time
//
// So adding a category in Go and regenerating breaks `npm run build` until it
// has a *word*, but never until it has a *picture*. That is what lets the icon
// set grow gradually while the list stays correct: an unillustrated category
// shows the fallback image beside its own proper label, rather than showing the
// right picture for the wrong thing or the string "accesspoint".
//
// Adding an image is therefore one file and one line here. Nothing else moves.
//
// Imported through vite rather than referenced from public/, so the assets are
// content-hashed into assets/ and get the immutable cache header that
// internal/webui/webui.go already sets for that prefix. It also means a missing
// file is a build error rather than a 404 nobody notices.

const IMAGES: Partial<Record<DeviceCategory, string>> = {
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
 * The photograph for a category, or undefined when there is not one yet.
 *
 * Undefined rather than a fallback image, so the caller has to decide what to
 * show — which is the decision that keeps an unillustrated category looking
 * intentional. See DeviceIcon.
 */
export function deviceIcon(category: DeviceCategory): string | undefined {
  return IMAGES[category]
}

/** Whether a category has a photograph of its own. */
export function hasOwnIcon(category: DeviceCategory): boolean {
  return IMAGES[category] !== undefined
}

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
