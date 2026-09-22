import {
  Activity,
  Globe,
  KeyRound,
  Link2,
  Network,
  Router,
  Waypoints,
} from 'lucide-react'
import type { LucideIcon } from 'lucide-react'

/**
 * Every section of the app, and everything inside one.
 *
 * This table is the only description of a section anywhere. It renders in five
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
 * ## Why the section names did not change
 *
 * The old rule was to use the word the audience already knows, which is why
 * dhcp's section was once called "Addresses" — nobody outside networking says
 * DHCP. That rule was right while this list *was* the front door. It is not any
 * more: the overview answers the everyday questions in plain language, so the
 * rest are free to name mechanisms, and the person who goes looking for a
 * section called DHCP is exactly the person who wants DHCP. Mechanism names in
 * a row also read as one system, where "Addresses / DNS / Internet" read as
 * three different registers.
 *
 * There is no Firewall section, and its absence is deliberate rather than
 * pending. The section used to hold port forwarding and no filtering at all;
 * both halves are under Gateway now, which is the module that owns the boundary
 * in either direction (docs/port-forwarding.md §0). A label promising a
 * firewall over a page that has none was the thing worth losing.
 *
 * Devices is absent because it is not a section: the device list is the body of
 * the overview. Filing it under DHCP was considered and rejected — the
 * statically-addressed printer has never held a lease, and would have lived on
 * a page named for the protocol that has never seen it.
 *
 * Ingress is the exception, and it is kept rather than argued away:
 * the mechanism name is jargon borrowed from Kubernetes, and most people running
 * a house network have never met it. It is kept anyway, because every other
 * label here is exactly its module's name and `olr ingress` is the command —
 * breaking that one-to-one mapping costs more than the word costs. "Services"
 * was the alternative and is worse: on a Linux box that word already means
 * systemd units, which is the thing this page is not about.
 */
export interface SettingGroup {
  /** The last segment of the URL: /dns/blocking. */
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
  /**
   * One line, under the section's title. Deliberately short: a landing page
   * opens with a status strip that says what is actually happening, and a
   * paragraph above it would be read first and mean less.
   */
  blurb: string
  groups: SettingGroup[]
}

export const SECTIONS: Section[] = [
  {
    to: '/',
    label: 'Overview',
    icon: Activity,
    end: true,
    blurb: 'Your network, and anything that needs you.',
    groups: [],
  },
  {
    // First after the overview, because it is what everything below it keys
    // off: a DHCP range is served on a network, a gateway exit is chosen by
    // network. Putting it after them would mean every one of those pages sends
    // the operator back here before they can do anything.
    to: '/networks',
    label: 'Networks',
    icon: Router,
    end: false,
    // Adoption lives on this page too, and did not always. It was a sub-page of
    // DHCP, filed there because DHCP was the first module that needed it. That
    // reason expired when this section landed: a network cannot be created on
    // an interface nobody handed over, so adoption became the prerequisite of
    // the page the nav already calls first — reachable only by opening a
    // section listed *after* it and going one level down. An operator with two
    // NICs to set up could not find it, which is the whole bug in one sentence.
    blurb:
      'The interfaces this router has been given, the subnets they carry, and how the router itself gets online.',
    groups: [],
  },
  {
    to: '/gateway',
    label: 'Gateway',
    icon: Waypoints,
    end: false,
    blurb: 'The boundary with the internet: where traffic goes out, and what comes in.',
    groups: [
      {
        slug: 'exits',
        label: 'Ways out',
        blurb:
          'Somewhere this router can hand traffic to — another box on your network, a VPN or proxy connection, or nowhere at all.',
      },
      {
        slug: 'forwards',
        label: 'Port forwards',
        blurb:
          'Let something on the internet reach one device here. This is a translation, not a firewall permission — olr has no filtering policy for it to be an exception to.',
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
      {
        slug: 'filtering',
        label: 'Filtering this router does not manage',
        blurb:
          'Another program on this box drops traffic passing through the router by default. olr cannot override that, so a forward may be correct and still not reach.',
      },
    ],
  },
  {
    to: '/dhcp',
    label: 'DHCP',
    icon: Network,
    end: false,
    blurb: 'Devices that join your network get an address from this router.',
    groups: [
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
      // Interfaces was here, and it was the wrong place. See /networks.
      {
        slug: 'advanced',
        label: 'Advanced',
        blurb:
          "Settings this router does not model, passed straight through to dnsmasq. Kept here rather than hand-edited into the daemon's file, so they stay part of your configuration.",
      },
    ],
  },
  {
    to: '/dns',
    label: 'DNS',
    icon: Globe,
    end: false,
    blurb: 'Every device on your network looks up names through this router.',
    groups: [
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
        blurb: 'Which addresses this router serves DNS on, and who is allowed to ask.',
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
    // Next to the gateway, because the two of them are the same question asked
    // twice: what may come in. A port forward lets the internet reach one
    // thing; this lets you reach all of it.
    to: '/remote',
    label: 'Remote access',
    icon: KeyRound,
    end: false,
    blurb: 'Reach your whole network from outside it, from your own devices.',
    // No sub-pages. Everything here is either live — who is connected, and when
    // they last were — or a setting that is set once and never revisited, and
    // there are three of those. A settings page holding three fields would be a
    // level of navigation charging rent.
    groups: [],
  },
  {
    to: '/ingress',
    label: 'Ingress',
    icon: Link2,
    end: false,
    blurb: 'Reach what is running on your network by name, over https.',
    // No sub-pages. Both things on this screen are live rather than settings —
    // the addresses, and a certificate whose expiry is the only number on the
    // page that will be different tomorrow. Filing the certificate one level
    // down would hide the renewal failure that is silent for weeks.
    groups: [],
  },
]

/** The icon for the app itself, used in the bar beside the name. */
export const BRAND_ICON = Router

/** The section a path belongs to, section landing pages and sub-pages alike. */
export function sectionOf(pathname: string): Section | undefined {
  return SECTIONS.find(({ to, end }) => (end ? pathname === to : pathname.startsWith(to)))
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
