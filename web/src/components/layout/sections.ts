import { Activity, ArrowLeftRight, Shield, SlidersHorizontal, Waypoints, Wifi, Wrench } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'

/**
 * Every section of the app, and everything inside one.
 *
 * This table describes top-level sections and nested Gateway destinations. It renders in five
 * places now — the top bar, the tab bar, the page title, the settings list on a
 * section's landing page, and the header of every sub-page — because writing a
 * heading four times is four ways for it to drift.
 *
 * ## Why there is a second level at all
 *
 * Every section used to be one page holding everything it could do: DNS stacked
 * eight cards, each opening with two to four sentences, so an operator asking
 * "is DNS working" read a paragraph about DNS-over-TLS on the way past. The
 * writing was not the problem — a setting that can take down a house's internet
 * deserves a sentence saying so. Putting all of it on one screen was.
 *
 * So a section's landing page now answers two questions and no others: is this
 * working, and what is it doing. Everything that is a *setting* moves one level
 * down, into a page of its own, and takes its explanation with it. The long
 * blurbs below are that explanation — they are not shown in the list, only on
 * the page the row leads to, which is the moment they are worth reading.
 *
 * Networks, DHCP and DNS live under Gateway: the interfaces and subnets it
 * serves, the addresses it hands out, and the names it resolves belong together.
 * Wireless has its own placeholder section; Access follows it.
 *
 * Access owns the directory of ways in and out. Configuration stays with its
 * module; networks and devices retain their routing choices. Advanced keeps
 * Firewall, Tools, Dynamic DNS and IPTV. Firewall and Tools retain their URLs.
 *
 * Devices is absent because it is not a section: the device list is the body of
 * the overview. Filing it under DHCP was considered and rejected — the
 * statically-addressed printer has never held a lease, and would have lived on
 * a page named for the protocol that has never seen it.
 *
 * Publishing is named by its purpose in the UI. The ingress slug and CLI
 * module name remain stable, so operator documentation still maps to it.
 */
export interface SettingGroup {
  /** The last segment of the URL: /gateway/dns/blocking. */
  slug: string
  label: string
  /** Shown on the sub-page itself, never in the list that links to it. */
  blurb: string
  /** Override for destinations that retain an existing URL. */
  to?: string
}

export interface Section {
  to: string
  label: string
  icon: LucideIcon
  end: boolean
  /** A destination within a section, not a top-level navigation item. */
  nested?: boolean
  /** Top-level navigation owner for a destination with a separate URL. */
  navTo?: string
  groups: SettingGroup[]
}

export const SECTIONS: Section[] = [
  {
    to: '/',
    label: 'Overview',
    icon: Activity,
    end: true,
    groups: [],
  },
  {
    to: '/gateway',
    label: 'Gateway',
    icon: Waypoints,
    end: false,
    groups: [
      {
        slug: 'interfaces',
        label: 'Interfaces & routing',
        blurb: '',
      },
      {
        slug: 'dhcp',
        label: 'DHCP',
        blurb: 'Define served networks, hand out addresses and reserve them for devices that need a fixed one.',
      },
      {
        slug: 'dns',
        label: 'DNS',
        blurb: 'Resolve names, manage local names and control what devices can look up.',
      },
      {
        slug: 'unmanaged',
        label: 'External routing rules',
        // design.md §3.4: pretending we are the only actor is a bug. Somebody
        // else's rules are shown so a hand-rolled setup is legible rather than
        // mysterious.
        blurb: 'Read-only rules outside OLR management.',
      },
    ],
  },
  {
    to: '/gateway/dhcp',
    label: 'DHCP',
    icon: Waypoints,
    nested: true,
    end: false,
    groups: [
      { slug: 'details', label: 'DHCP', blurb: '' },
      {
        slug: 'ranges',
        label: 'Address ranges',
        blurb:
          'Addresses handed out on each network.',
      },
      {
        slug: 'reservations',
        label: 'Reserved addresses',
        blurb:
          'Fixed DHCP addresses for selected devices.',
      },
      // Interface adoption stays under Gateway / Interfaces; served networks are beside DHCP.
      {
        slug: 'advanced',
        label: 'Advanced',
        blurb:
          'Custom dnsmasq settings. OLR-managed directives cannot be overridden.',
      },
    ],
  },
  {
    to: '/gateway/dns',
    label: 'DNS',
    icon: Waypoints,
    nested: true,
    end: false,
    groups: [
      { slug: 'details', label: 'DNS', blurb: '' },
      {
        slug: 'blocking',
        label: 'Blocking',
        blurb:
          'DNS blocking rules for selected devices and networks.',
      },
      {
        slug: 'names',
        label: 'Local names',
        blurb:
          'Names resolved locally by this router.',
      },
      {
        slug: 'resolving',
        label: 'Upstream DNS',
        blurb: '',
      },
      {
        slug: 'listening',
        label: 'DNS listening',
        blurb: '',
      },
      {
        slug: 'enforcement',
        label: 'DNS enforcement',
        blurb:
          'Redirect external DNS requests on selected networks.',
      },
      {
        slug: 'advanced',
        label: 'Advanced',
        blurb:
          '',
      },
    ],
  },
  {
    to: '/wireless',
    label: 'Wireless',
    icon: Wifi,
    end: false,
    groups: [],
  },
  {
    to: '/firewall',
    label: 'Firewall',
    icon: Shield,
    nested: true,
    navTo: '/advanced',
    end: false,
    groups: [],
  },
  {
    to: '/access',
    label: 'Access',
    icon: ArrowLeftRight,
    end: false,
    groups: [
      { slug: 'gateway', label: 'Gateway', blurb: 'Use another gateway or an existing VPN interface.' },
      { slug: 'tunnelbroker', label: 'Tunnel Broker', blurb: '' },
      { slug: 'wireguard', label: 'WireGuard', blurb: '' },
      { slug: 'socks-server', label: 'SOCKS5 server', blurb: '' },
      {
        slug: 'socks5',
        label: 'External SOCKS5 proxy',
        blurb: '',
      },
      {
        slug: 'remote',
        label: 'Remote access',
        blurb: '',
      },
      {
        slug: 'ingress',
        label: 'Publish web services',
        blurb: 'Published services still need their own access controls.',
      },
      {
        slug: 'forwards',
        label: 'Port forwards',
        blurb: 'Forwarding a port also grants its firewall permission.',
      },
      {
        // Reached from port forwards when foreign filtering exists.
        slug: 'filtering',
        label: 'External filtering',
        blurb: 'Read-only forwarding policies outside OLR management.',
      },
    ],
  },
  {
    to: '/tools',
    label: 'Tools',
    icon: Wrench,
    nested: true,
    navTo: '/advanced',
    end: false,
    groups: [],
  },
  {
    to: '/advanced',
    label: 'Advanced',
    icon: SlidersHorizontal,
    end: false,
    groups: [
      { slug: 'firewall', label: 'Firewall', blurb: '', to: '/firewall' },
      { slug: 'tools', label: 'Tools', blurb: '', to: '/tools' },
      {
        slug: 'ddns',
        label: 'Dynamic DNS',
        blurb:
          '',
      },
      {
        slug: 'iptv',
        label: 'IPTV multicast',
        blurb: '',
      },
    ],
  },
]

/** The section a path belongs to, section landing pages and sub-pages alike. */
export function sectionOf(pathname: string): Section | undefined {
  return [...SECTIONS].reverse().find(({ to, end }) =>
    end ? pathname === to : pathname === to || pathname.startsWith(`${to}/`),
  )
}

/** One group, by the section path and slug the caller already knows. */
export function groupOf(sectionTo: string, slug: string): { section: Section; group: SettingGroup } {
  const section = SECTIONS.find((s) => s.to === sectionTo)
  const group = section?.groups.find((g) => g.slug === slug)
  if (!section || !group) {
    // A slug that is not in the table is a routing bug, not a user error: the
    // routes and this list are written together. Failing loudly in development
    // beats a page with an empty heading.
    throw new Error(`no settings group ${sectionTo}/${slug}`)
  }
  return { section, group }
}
