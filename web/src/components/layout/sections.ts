import { Activity, ArrowLeftRight, Shield, SlidersHorizontal, Waypoints, Wrench } from 'lucide-react'
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
 * Firewall has its own section because its boundary and live openings deserve
 * a direct place in the bar; Access follows it.
 *
 * Advanced is the one section that is not a module, and that is the point of
 * it. The other sections are what every router has, in the order an operator
 * sets them up in. What is under Advanced is what a house network can go years
 * without: letting the internet in to one device, a public name kept pointing
 * here, dialling in, and publishing services by name. Each used to be a
 * section of its own, which put the rarely-visited half of the app level with
 * the half every visit is about. They read as one group anyway: all four are about reaching in from outside.
 *
 * Devices is absent because it is not a section: the device list is the body of
 * the overview. Filing it under DHCP was considered and rejected — the
 * statically-addressed printer has never held a lease, and would have lived on
 * a page named for the protocol that has never seen it.
 *
 * Ingress keeps its name under Advanced, and it is kept rather than argued
 * away: the mechanism name is jargon borrowed from Kubernetes, and most people running
 * a house network have never met it. It is kept anyway, because every other
 * label here is exactly its module's name and `olr ingress` is the command —
 * breaking that one-to-one mapping costs more than the word costs. "Services"
 * was the alternative and is worse: on a Linux box that word already means
 * systemd units, which is the thing this page is not about.
 */
export interface SettingGroup {
  /** The last segment of the URL: /gateway/dns/blocking. */
  slug: string
  label: string
  /** Shown on the sub-page itself, never in the list that links to it. */
  blurb: string
}

export interface Section {
  to: string
  label: string
  icon: LucideIcon
  end: boolean
  /** A destination within a section, not a top-level navigation item. */
  nested?: boolean
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
        label: 'Advanced interfaces and routing',
        blurb: 'All interfaces, forwarding, routing diagnostics and less common routing settings.',
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
        slug: 'exits',
        label: 'Ways out',
        blurb:
          'Somewhere this router can hand traffic to — another box on your network, a VPN or proxy connection, or nowhere at all.',
      },
      {
        slug: 'usage',
        label: 'Usage',
        blurb: 'How much each device has sent and received since this router started.',
      },
      {
        slug: 'unmanaged',
        label: 'Routing this router does not manage',
        // design.md §3.4: pretending we are the only actor is a bug. Somebody
        // else's rules are shown so a hand-rolled setup is legible rather than
        // mysterious.
        blurb: 'Rules another program put in place. olr leaves them alone.',
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
      { slug: 'details', label: 'DHCP status and settings', blurb: 'Service diagnostics, ranges, reservations and detailed DHCP settings.' },
      {
        slug: 'ranges',
        label: 'Address ranges',
        blurb:
          'The addresses this router hands out on each network. Leave a range blank and it is derived from the network\u2019s subnet, keeping the low addresses free for devices you configure by hand.',
      },
      {
        slug: 'reservations',
        label: 'Reserved addresses',
        blurb:
          'Devices that should always get the same address — printers, a NAS, anything you reach by address rather than by name.',
      },
      // Interface adoption stays under Gateway / Interfaces; served networks are beside DHCP.
      {
        slug: 'advanced',
        label: 'Advanced',
        blurb:
          "Settings this router does not model, passed straight through to dnsmasq. Kept here rather than hand-edited into the daemon's file, so they stay part of your configuration.",
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
      { slug: 'details', label: 'DNS activity and advanced settings', blurb: 'Queries, observed service status and detailed resolver settings.' },
      {
        slug: 'blocking',
        label: 'Blocking',
        blurb:
          'What each set of devices is allowed to look up. Anything blocked here never resolves, whatever app asked for it.',
      },
      {
        slug: 'names',
        label: 'Local names',
        blurb:
          'Names this network answers for itself, so you can reach a device by name instead of by an address that DHCP may move.',
      },
      {
        slug: 'resolving',
        label: 'How names get resolved',
        blurb: 'Where this router goes to turn a name into an address.',
      },
      {
        slug: 'listening',
        label: 'Where it answers',
        blurb: 'Which addresses this router serves DNS on.',
      },
      {
        slug: 'enforcement',
        label: 'Devices that ignore this router',
        blurb:
          'Handing out a DNS server is only advice. Browsers and phones routinely go straight to a public resolver instead — and when they do, nothing here applies to them and nothing here can see them.',
      },
      {
        slug: 'advanced',
        label: 'Advanced',
        blurb:
          'How much of the query log to keep, and settings this router does not model, passed straight through to unbound.',
      },
    ],
  },
  {
    to: '/firewall',
    label: 'Firewall',
    icon: Shield,
    end: false,
    groups: [],
  },
  {
    to: '/access',
    label: 'Access',
    icon: ArrowLeftRight,
    end: false,
    groups: [],
  },
  {
    to: '/tools',
    label: 'Tools',
    icon: Wrench,
    end: false,
    groups: [],
  },
  {
    to: '/advanced',
    label: 'Advanced',
    icon: SlidersHorizontal,
    end: false,
    groups: [
      {
        slug: 'forwards',
        label: 'Port forwards',
        blurb:
          'Let something on the internet reach one device here. With the firewall on, a forward is also its own permission — there is nothing to open separately.',
      },
      {
        // Not a row on the landing page: it is reached from the forwards page,
        // and only when there is something on it.
        slug: 'filtering',
        label: 'Filtering this router does not manage',
        blurb:
          'Another program on this box drops traffic passing through the router by default. olr cannot override that, so a forward may be correct and still not reach.',
      },
      {
        slug: 'ddns',
        label: 'Dynamic DNS',
        blurb:
          'Public names this router keeps pointing at itself, so they follow your address when your internet provider changes it.',
      },
      {
        // Everything on this page is either live — who is connected, and when
        // they last were — or a setting set once and never revisited, and there
        // are three of those. They open in dialogs rather than a level below.
        slug: 'remote',
        label: 'Remote access',
        blurb: 'WireGuard, Shadowsocks and SOCKS5 servers for connecting from outside.',
      },
      {
        slug: 'iptv',
        label: 'IPTV multicast',
        blurb: 'Let a set-top box on your LAN subscribe to routed IPv4 IPTV streams from a separate upstream interface. This does not bridge VLANs or relay the provider DHCP.',
      },
      {
        // Both things on this page are live rather than settings — the
        // addresses, and a certificate whose expiry is the only number here
        // that will be different tomorrow. Filing the certificate one level
        // further down would hide the renewal failure that is silent for weeks.
        slug: 'ingress',
        label: 'Ingress',
        blurb: 'Reach what is running on your network by name, over https.',
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
