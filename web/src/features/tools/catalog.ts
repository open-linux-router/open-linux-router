export const TOOL_DEFINITIONS = [
  { slug: 'speedtest', label: 'Internet speed test', description: 'Measure download, upload and latency from the router to the internet.', category: 'Bandwidth', detail: 'Choose a route and test server' },
  { slug: 'link-test', label: 'Device-to-router link test', description: 'Measure how fast a device can send and receive data on your local network.', category: 'Local network', detail: 'Test your connection with iperf3' },
  { slug: 'nat', label: 'NAT test', description: 'Discover the public UDP address and NAT behavior seen by a STUN server.', category: 'Connectivity', detail: 'Check mapping and filtering' },
  { slug: 'ping', label: 'Ping a destination', description: 'Check whether a destination responds, how quickly, and with how much packet loss.', category: 'Reachability', detail: 'Send four probes from the router' },
  { slug: 'traceroute', label: 'Traceroute', description: 'Follow the network hops between this router and a hostname or IP address.', category: 'Routing', detail: 'Explore the path to a destination' },
] as const

export type ToolSlug = typeof TOOL_DEFINITIONS[number]['slug']
