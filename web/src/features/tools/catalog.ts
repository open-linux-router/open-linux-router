import { t } from '@/lib/i18n'
// The directory tile wears a short name and a letter mark; the page behind it
// keeps the fuller label. Both name the same operation, and the tile's short
// name is what fits under a 64px mark. A diagnostic that is a named upstream
// tool takes that project's name instead, and may wear its artwork.
export const TOOL_DEFINITIONS = [
  { slug: 'speedtest', tileLabel: 'SpeedTest', letter: 'S', label: 'SpeedTest', get description() { return t('Measure download, upload and latency from the router to the internet.') } },
  { slug: 'librespeed', tileLabel: 'LibreSpeed', letter: 'L', label: 'LibreSpeed', get description() { return t('Measure how fast this device can send and receive data on your local network, in the browser.') } },
  // The mark is the project's own lowercase initial: the tool is iPerf3, and a
  // capital I in the tile would be the only place the name is spelled wrong.
  { slug: 'iperf3', tileLabel: 'iPerf3', letter: 'i', label: 'iPerf3', get description() { return t('Measure sustained throughput between one device and this router; iperf3 must be installed on both ends.') } },
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
