import {
  AirVent,
  Bell,
  BookOpen,
  Bot,
  CircuitBoard,
  Droplets,
  Fan,
  Gamepad2,
  GlassWater,
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
  Refrigerator,
  Router as RouterGlyph,
  Server,
  Shirt,
  Smartphone,
  Speaker,
  Tablet as TabletGlyph,
  Thermometer,
  Tv as TvGlyph,
  Video,
  WashingMachine,
  Watch,
  Wifi,
  type LucideIcon,
} from 'lucide-react'

import type { VendorKey } from '@/lib/api-types'
import type { DeviceCategory } from '@/lib/config-types'

import accessPoint from '@/assets/device-icons/accesspoint.webp'
import aircon from '@/assets/device-icons/aircon.webp'
import airer from '@/assets/device-icons/airer.webp'
import airPurifier from '@/assets/device-icons/airpurifier.webp'
import camera from '@/assets/device-icons/camera.webp'
import gameConsole from '@/assets/device-icons/console.webp'
import desktop from '@/assets/device-icons/desktop.webp'
import doorbell from '@/assets/device-icons/doorbell.webp'
import fridge from '@/assets/device-icons/fridge.webp'
import hub from '@/assets/device-icons/hub.webp'
import humidifier from '@/assets/device-icons/humidifier.webp'
import laptop from '@/assets/device-icons/laptop.webp'
import light from '@/assets/device-icons/light.webp'
import nas from '@/assets/device-icons/nas.webp'
import phone from '@/assets/device-icons/phone.webp'
import plug from '@/assets/device-icons/plug.webp'
import printer from '@/assets/device-icons/printer.webp'
import router from '@/assets/device-icons/router.webp'
import sensor from '@/assets/device-icons/sensor.webp'
import server from '@/assets/device-icons/server.webp'
import speaker from '@/assets/device-icons/speaker.webp'
import tablet from '@/assets/device-icons/tablet.webp'
import thermostat from '@/assets/device-icons/thermostat.webp'
import tv from '@/assets/device-icons/tv.webp'
import unknown from '@/assets/device-icons/unknown.webp'
import vacuum from '@/assets/device-icons/vacuum.webp'
import washer from '@/assets/device-icons/washer.webp'
import waterPurifier from '@/assets/device-icons/waterpurifier.webp'
import watch from '@/assets/device-icons/watch.webp'
import appleDesktop from '@/assets/device-icons/apple-desktop.webp'
import appleLaptop from '@/assets/device-icons/apple-laptop.webp'
import applePhone from '@/assets/device-icons/apple-phone.webp'
import appleSpeaker from '@/assets/device-icons/apple-speaker.webp'
import appleTablet from '@/assets/device-icons/apple-tablet.webp'
import appleTv from '@/assets/device-icons/apple-tv.webp'
import appleWatch from '@/assets/device-icons/apple-watch.webp'
import huaweiLaptop from '@/assets/device-icons/huawei-laptop.webp'
import lenovoLaptop from '@/assets/device-icons/lenovo-laptop.webp'

import osAndroid from '@/assets/os-marks/android.svg'
import osAppleDark from '@/assets/os-marks/apple-dark.svg'
import osAppleLight from '@/assets/os-marks/apple-light.svg'
import osArch from '@/assets/os-marks/archlinux.svg'
import osDebian from '@/assets/os-marks/debian.svg'
import osFedora from '@/assets/os-marks/fedora.svg'
import osFreeBSD from '@/assets/os-marks/freebsd.svg'
import osHarmony from '@/assets/os-marks/harmonyos.svg'
import osLinux from '@/assets/os-marks/linux.svg'
import osOpenWrt from '@/assets/os-marks/openwrt.svg'
import osProxmox from '@/assets/os-marks/proxmox.svg'
import osRaspberryPi from '@/assets/os-marks/raspberrypi.svg'
import osUbuntu from '@/assets/os-marks/ubuntu.svg'
import osWindows from '@/assets/os-marks/windows.svg'

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
  accesspoint: accessPoint,
  aircon,
  airer,
  airpurifier: airPurifier,
  camera,
  console: gameConsole,
  desktop,
  doorbell,
  fridge,
  hub,
  humidifier,
  laptop,
  light,
  nas,
  phone,
  plug,
  printer,
  router,
  sensor,
  server,
  speaker,
  tablet,
  thermostat,
  tv,
  unknown,
  vacuum,
  washer,
  waterpurifier: waterPurifier,
  watch,

  'apple/desktop': appleDesktop,
  'apple/laptop': appleLaptop,
  'apple/phone': applePhone,
  'apple/speaker': appleSpeaker,
  'apple/tablet': appleTablet,
  'apple/tv': appleTv,
  'apple/watch': appleWatch,

  'huawei/laptop': huaweiLaptop,
  'lenovo/laptop': lenovoLaptop,
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

  airpurifier: 'Air purifier',
  waterpurifier: 'Water purifier',
  humidifier: 'Humidifier',
  aircon: 'Air conditioner',
  airer: 'Clothes airer',
  fridge: 'Fridge',
  washer: 'Washing machine',

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

  airpurifier: Fan,
  waterpurifier: GlassWater,
  humidifier: Droplets,
  aircon: AirVent,
  airer: Shirt,
  fridge: Refrigerator,
  washer: WashingMachine,

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
 * Undefined rather than a fallback image, so the caller has to decide what to
 * show; that decision is what keeps an unillustrated device looking intentional
 * rather than broken. See DeviceIcon.
 *
 * `vendorMarked` says the caller has a vendor mark it would rather show than the
 * anonymous `unknown` photograph. Without it this function has a hole that is
 * easy to miss and was: `unknown` *is* registered in IMAGES, so an uncategorised
 * device returns the grey box at rung 3 and never reaches the caller's fallback
 * at all. Six devices from six different makers came out as six identical
 * boxes, which is the exact screen this whole mechanism exists to fix.
 *
 * Per-model artwork (ICONS.md tier 2) would sit above all of these. It has no
 * assets and no key vocabulary yet, so it is not wired in — adding it means one
 * more rung at the top and nothing else.
 */
export function deviceIcon(
  category: DeviceCategory,
  vendor?: VendorKey,
  vendorMarked = false,
): string | undefined {
  // 'unknown' and '' are both "nobody has said" — see Category in
  // internal/devices/category.go, where the two differ in provenance but not in
  // how much either tells a picture.
  const uncategorised = category === 'unknown' || category === ''

  if (vendor) {
    const both = IMAGES[`${vendor}/${category}`]
    if (both) return both

    if (uncategorised) {
      const own = IMAGES[vendor]
      if (own) return own
    }
  }

  // The grey box is the answer for a device nothing is known about. A device
  // whose maker is known is not that device, even when nobody has drawn it.
  if (uncategorised && vendorMarked) return undefined

  return IMAGES[category]
}

// hasOwnIcon(category) used to live here and had no callers. Rather than grow
// it a vendor argument nobody would pass, it is gone: `deviceIcon(...) !==
// undefined` is what it was, and is shorter than importing it.

/**
 * A vendor's name reduced to a mark that survives at 32 pixels.
 *
 * This is what a device with a vendor and no category gets instead of a
 * picture, and it is not a consolation prize — it is the only answer available.
 * Artwork would mean drawing "an Apple something", which means choosing between
 * a phone, a watch and a laptop, which is inventing information; ICONS.md and
 * detect.go both refuse to do that. Artwork also only ever covers the few dozen
 * vendors somebody has drawn, where the registry knows thirty thousand. Letters
 * cover all of them.
 *
 * The rules, in order, each earning its place on a name that really occurs:
 *
 *   1. A short all-capitals first word *is* the mark already: "WNC", "ZTE",
 *      "HP", "LG". Because the split happens on the hyphen too, "TP-Link"
 *      gives "TP" rather than the "TL" nobody would recognise.
 *   2. Two or more words give their first two initials: "Raspberry Pi" → RP,
 *      "Philips Hue" → PH, "Texas Instruments" → TI.
 *   3. One word gives its first two letters: "Huawei" → HU, "eero" → EE.
 *
 * Upper-cased at the end rather than preserved, because a column of these is
 * scanned for difference rather than read for spelling, and "eE" beside "HU"
 * reads as a bug.
 */
export function vendorInitials(vendor?: string): string {
  if (!vendor) return ''

  // Split on anything that is not a letter or digit, so hyphens, dots and
  // ampersands all separate words: "TP-Link", "D-Link", "Routerboard.com".
  const words = vendor.split(/[^A-Za-z0-9]+/).filter(Boolean)
  if (words.length === 0) return ''

  const first = words[0]
  if (first.length <= 4 && first === first.toUpperCase()) return first
  if (words.length >= 2) return (first[0] + words[1][0]).toUpperCase()
  return first.slice(0, 2).toUpperCase()
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
  {
    label: 'Appliances',
    categories: ['airpurifier', 'waterpurifier', 'humidifier', 'aircon', 'airer', 'fridge', 'washer'],
  },
  { label: 'Computing', categories: ['printer', 'nas', 'server', 'sbc'] },
  { label: 'Network', categories: ['router', 'accesspoint', 'switch', 'hub'] },
  { label: 'Other', categories: ['unknown'] },
]

/**
 * An operating system with a mark — internal/devices operatingSystems, in the
 * same order. Hand-mirrored for the reason VendorKey is: the icon field is
 * validated in Go but published as a pattern, not an enum, so nothing
 * generates this list.
 */
export type OsKey =
  | 'windows'
  | 'apple'
  | 'android'
  | 'harmonyos'
  | 'linux'
  | 'ubuntu'
  | 'debian'
  | 'fedora'
  | 'archlinux'
  | 'freebsd'
  | 'proxmox'
  | 'openwrt'
  | 'raspberrypi'

/**
 * The OS marks, in full colour, from thesvg.org (MIT). Logos, which ICONS.md
 * otherwise forbids — see "Operating-system marks" there for why these are
 * the exception.
 *
 * `dark` is the variant for a dark theme, for the one mark that is a single
 * colour and would vanish on its own background: Apple's is black on light
 * and white on dark. HarmonyOS wears Huawei's flower, because its own mark is
 * the wordmark "HMOS", which is unreadable at 32 pixels and black besides.
 */
export const OS_MARKS: Record<OsKey, { label: string; src: string; dark?: string }> = {
  windows: { label: 'Windows', src: osWindows },
  apple: { label: 'macOS / iOS', src: osAppleLight, dark: osAppleDark },
  android: { label: 'Android', src: osAndroid },
  harmonyos: { label: 'HarmonyOS', src: osHarmony },
  linux: { label: 'Linux', src: osLinux },
  ubuntu: { label: 'Ubuntu', src: osUbuntu },
  debian: { label: 'Debian', src: osDebian },
  fedora: { label: 'Fedora', src: osFedora },
  archlinux: { label: 'Arch Linux', src: osArch },
  freebsd: { label: 'FreeBSD', src: osFreeBSD },
  proxmox: { label: 'Proxmox', src: osProxmox },
  openwrt: { label: 'OpenWrt', src: osOpenWrt },
  raspberrypi: { label: 'Raspberry Pi OS', src: osRaspberryPi },
}

export const OS_KEYS = Object.keys(OS_MARKS) as OsKey[]

/**
 * What a stored `icon` draws, or undefined when it names nothing drawable —
 * an Apple TV nobody has painted yet, say, which then falls back to the
 * category's own picture exactly as if no icon were set.
 */
export type ChosenIcon =
  | { kind: 'photo'; src: string }
  | { kind: 'os'; src: string; dark?: string; label: string }

export function chosenIcon(icon?: string): ChosenIcon | undefined {
  if (!icon) return undefined
  if (icon.startsWith('os/')) {
    const mark = OS_MARKS[icon.slice(3) as OsKey]
    return mark && { kind: 'os', ...mark }
  }
  const src = IMAGES[icon as IconKey]
  return src ? { kind: 'photo', src } : undefined
}

/**
 * The words for a vendor's own pictures, where the vendor has a name for the
 * thing that says more than "<Vendor> <category>". Only pictures that exist
 * are offered, so this is keyed like IMAGES and just as sparse.
 */
const BRAND_LABELS: Partial<Record<IconKey, string>> = {
  'apple/phone': 'iPhone',
  'apple/tablet': 'iPad',
  'apple/laptop': 'MacBook',
  'apple/desktop': 'iMac',
  'apple/watch': 'Apple Watch',
  'apple/tv': 'Apple TV',
  'apple/speaker': 'HomePod',
}

/**
 * Every vendor picture that has been drawn, for the picker. Derived from
 * IMAGES rather than listed, so a new `huawei/phone` shows up in the picker
 * the moment its line is added there.
 */
export const BRAND_CHOICES: { icon: string; category: DeviceCategory; label: string }[] = (
  Object.keys(IMAGES) as IconKey[]
)
  .filter((key) => key.includes('/'))
  .map((key) => {
    const [vendor, category] = key.split('/') as [string, DeviceCategory]
    const name = vendor.charAt(0).toUpperCase() + vendor.slice(1)
    return { icon: key, category, label: BRAND_LABELS[key] ?? `${name} ${LABELS[category].toLowerCase()}` }
  })
