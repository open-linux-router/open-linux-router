import { t } from '@/lib/i18n'
// One name per tool, usually the same on the tile and on the page behind it.
// `tileLabel` is what the tile shows where the two differ — NextTrace is the
// project, Traceroute is the operation — and a diagnostic that is a named
// upstream service keeps that service's own name in both places, because the
// service is what distinguishes it from its neighbours.
export const TOOL_DEFINITIONS = [
  // The internet speed tests are one tile per service, not one tile and a
  // picker: each measures a different path, and nobody arrives here wanting
  // "a speed test" without already knowing which one. The daemon runs Ookla
  // itself and shells out to the other two, which is how the page knows one is
  // missing while the others still work.
  { slug: 'speedtest', tileLabel: 'Speedtest by Ookla', letter: 'S', label: 'Speedtest by Ookla', get description() { return t('Measure download, upload and latency against an Ookla server near this router.') } },
  { slug: 'cloudflare', tileLabel: 'Cloudflare Speed Test', letter: 'C', label: 'Cloudflare Speed Test', get description() { return t('Measure download, upload and latency against the nearest Cloudflare edge.') } },
  { slug: 'fastcom', tileLabel: 'Fast.com', letter: 'F', label: 'Fast.com', get description() { return t('Measure download and upload on the Netflix Open Connect path streaming uses.') } },
  { slug: 'librespeed', tileLabel: 'LibreSpeed', letter: 'L', label: 'LibreSpeed', get description() { return t('Measure how fast this device can send and receive data on your local network, in the browser.') } },
  // The mark is the project's own lowercase initial: the tool is iPerf3, and a
  // capital I in the tile would be the only place the name is spelled wrong.
  { slug: 'iperf3', tileLabel: 'iPerf3', letter: 'i', label: 'iPerf3', get description() { return t('Measure sustained throughput between one device and this router; iperf3 must be installed on both ends.') } },
  { slug: 'nat', tileLabel: 'STUN', letter: 'S', label: 'STUN', get description() { return t('See the public UDP address and how this router\'s NAT maps and filters traffic.') } },
  { slug: 'ping', tileLabel: 'Ping', letter: 'P', label: 'Ping a destination', get description() { return t('Check whether a destination responds, how quickly, and with how much packet loss.') } },
  { slug: 'traceroute', tileLabel: 'NextTrace', letter: 'T', get label() { return t('Traceroute') }, get description() { return t('Follow the network hops between this router and a hostname or IP address.') } },
] as const

export type ToolSlug = typeof TOOL_DEFINITIONS[number]['slug']

// A tool that is a named upstream project may wear that project's mark; every
// other tool keeps its letter, because a mark says which project answers the
// measurement rather than how it is carried out. Marks are bundled under
// `public/tool-logos`, where their provenance is recorded.
//
// `TOOL_ARTWORK` is artwork that carries its own backing and fills the tile;
// `TOOL_LOGOS` is a plain glyph drawn on the launcher's white tile.
export const TOOL_ARTWORK: Partial<Record<ToolSlug, string>> = {
  traceroute: '/tool-logos/nexttrace.webp',
  librespeed: '/tool-logos/librespeed.svg',
}

export const TOOL_LOGOS: Partial<Record<ToolSlug, string>> = {
  speedtest: '/tool-logos/speedtest.svg',
  cloudflare: '/tool-logos/cloudflare.svg',
  fastcom: '/tool-logos/fastcom.png',
}
