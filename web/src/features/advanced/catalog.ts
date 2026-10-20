import { TOOL_ARTWORK, TOOL_DEFINITIONS } from '@/features/tools/catalog'

/**
 * The Advanced directory.
 *
 * Three groups, each with a charter rather than a topic list, so a feature has
 * somewhere to go before it exists: System is this machine and what may reach
 * it, Services are the background jobs it runs for the network, and Diagnostics
 * are the measurements an operator runs on demand.
 *
 * A tile is a link and one short name. OLR features use approved app artwork
 * where available, otherwise a letter in the group's hue. A tile that names an
 * upstream project may wear its mark instead: a self-contained app icon that
 * fills the tile (NextTrace), or a plain glyph on white (Caddy), like Access's
 * project marks.
 */
export interface AdvancedTile {
  slug: string
  name: string
  letter: string
  /** An app icon with its own backing, for an OLR feature or upstream project. */
  artwork?: string
  /** A project's plain glyph, drawn on the launcher's white tile. */
  logo?: string
  /** One sentence for the tooltip; the name and letter are the tile. */
  description: string
  to: string
  /** Which live answer the tile shows under its name. Absent when it has none. */
  state?: 'firewall' | 'forwards' | 'ddns' | 'iptv' | 'admin' | 'ingress'
}

export interface AdvancedGroup {
  id: string
  title: string
  /** An oklch hue; the launcher draws `oklch(0.52 0.12 <hue>)`. */
  hue: number
  tiles: AdvancedTile[]
}

export const ADVANCED_GROUPS: AdvancedGroup[] = [
  {
    id: 'system',
    title: 'System',
    hue: 250,
    tiles: [
      {
        slug: 'firewall',
        name: 'Firewall',
        letter: 'F',
        description: 'Decide what may reach this router and the devices behind it.',
        to: '/firewall',
        state: 'firewall',
      },
      {
        slug: 'access',
        // The tile is one line, so it takes the short name; the page behind it
        // is still Administrator access.
        name: 'Admin access',
        letter: 'A',
        description: 'Password, sessions and sign-out for this web interface.',
        to: '/advanced/access',
        state: 'admin',
      },
      {
        slug: 'forwards',
        name: 'Port forwarding',
        letter: 'P',
        artwork: '/feature-icons/port-forwarding.webp',
        description: 'Reach a service inside this network through an external port.',
        to: '/advanced/forwards',
        state: 'forwards',
      },
    ],
  },
  {
    id: 'services',
    title: 'Services',
    hue: 190,
    tiles: [
      {
        slug: 'caddy',
        name: 'Caddy',
        letter: 'C',
        logo: '/service-logos/caddy.svg',
        description: 'Publish a service on your own domain over HTTPS.',
        to: '/advanced/caddy',
        state: 'ingress',
      },
      {
        slug: 'ddns',
        name: 'Dynamic DNS',
        letter: 'D',
        description: 'Keep a public name pointing at this changing address.',
        to: '/advanced/ddns',
        state: 'ddns',
      },
      {
        slug: 'iptv',
        name: 'IPTV multicast',
        letter: 'I',
        description: 'Receive the provider’s multicast TV streams on LAN devices.',
        to: '/advanced/iptv',
        state: 'iptv',
      },
    ],
  },
  {
    id: 'diagnostics',
    title: 'Diagnostics',
    hue: 300,
    tiles: TOOL_DEFINITIONS.map(tool => ({
      slug: `tool:${tool.slug}`,
      name: tool.tileLabel,
      letter: tool.letter,
      artwork: TOOL_ARTWORK[tool.slug],
      description: tool.description,
      to: `/tools/${tool.slug}`,
    })),
  },
]
