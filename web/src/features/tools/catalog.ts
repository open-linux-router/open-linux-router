export const TOOL_DEFINITIONS = [
  { slug: 'speedtest', directoryLabel: 'Internet speed test', label: 'Internet speed test', description: 'Measure download, upload and latency from the router to the internet.' },
  { slug: 'link-test', directoryLabel: 'LAN link test', label: 'Device-to-router link test', description: 'Measure how fast a device can send and receive data on your local network.' },
  { slug: 'nat', directoryLabel: 'NAT test', label: 'NAT test', description: 'Discover the public UDP address and NAT behavior seen by a STUN server.' },
  { slug: 'ping', directoryLabel: 'Ping', label: 'Ping a destination', description: 'Check whether a destination responds, how quickly, and with how much packet loss.' },
  { slug: 'traceroute', directoryLabel: 'Traceroute', label: 'Traceroute', description: 'Follow the network hops between this router and a hostname or IP address.' },
] as const

export type ToolSlug = typeof TOOL_DEFINITIONS[number]['slug']
