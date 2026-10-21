import { t } from '@/lib/i18n'
// The directory tile wears a short name and a letter mark; the page behind it
// keeps the fuller label. Both name the same operation, and the tile's short
// name is what fits under a 64px mark. A diagnostic that is a named upstream
// tool takes that project's name instead, and may wear its artwork.
export const TOOL_DEFINITIONS = [
  { slug: 'speedtest', tileLabel: 'Speed test', letter: 'S', get label() { return t('Internet speed test') }, get description() { return t('Measure download, upload and latency from the router to the internet.') } },
  { slug: 'link-test', tileLabel: 'LAN link test', letter: 'L', get label() { return t('Device-to-router link test') }, get description() { return t('Measure how fast a device can send and receive data on your local network.') } },
  { slug: 'nat', tileLabel: 'NAT test', letter: 'N', get label() { return t('NAT test') }, get description() { return t('Discover the public UDP address and NAT behavior seen by a STUN server.') } },
  { slug: 'ping', tileLabel: 'Ping', letter: 'P', label: 'Ping a destination', get description() { return t('Check whether a destination responds, how quickly, and with how much packet loss.') } },
  { slug: 'traceroute', tileLabel: 'NextTrace', letter: 'T', get label() { return t('Traceroute') }, get description() { return t('Follow the network hops between this router and a hostname or IP address.') } },
] as const

export type ToolSlug = typeof TOOL_DEFINITIONS[number]['slug']

// A tool that is a named upstream project may wear that project's own app
// icon; every other tool keeps its letter. Artwork is bundled under
// `public/tool-logos`, where its provenance is recorded. Never borrow the mark
// of a protocol, or of a service a test happens to contact.
export const TOOL_ARTWORK: Partial<Record<ToolSlug, string>> = {
  traceroute: '/tool-logos/nexttrace.webp',
}
