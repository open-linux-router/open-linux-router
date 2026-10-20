// The directory tile wears a short name and an initial; the page behind it
// keeps the fuller label. Both name the same operation, and the tile's short
// name is what fits under a 64px mark.
export const TOOL_DEFINITIONS = [
  { slug: 'speedtest', tileLabel: 'Speed test', letter: 'S', label: 'Internet speed test', description: 'Measure download, upload and latency from the router to the internet.' },
  { slug: 'link-test', tileLabel: 'LAN link test', letter: 'L', label: 'Device-to-router link test', description: 'Measure how fast a device can send and receive data on your local network.' },
  { slug: 'nat', tileLabel: 'NAT test', letter: 'N', label: 'NAT test', description: 'Discover the public UDP address and NAT behavior seen by a STUN server.' },
  { slug: 'ping', tileLabel: 'Ping', letter: 'P', label: 'Ping a destination', description: 'Check whether a destination responds, how quickly, and with how much packet loss.' },
  { slug: 'traceroute', tileLabel: 'Traceroute', letter: 'T', label: 'Traceroute', description: 'Follow the network hops between this router and a hostname or IP address.' },
] as const

export type ToolSlug = typeof TOOL_DEFINITIONS[number]['slug']
