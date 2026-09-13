import { SubPage } from '@/components/layout/sub-page'
import { useDhcpConfig } from '@/features/dhcp/queries'
import { InterfacesCard } from '@/features/link/interfaces-card'

/**
 * Adoption, on a page of its own.
 *
 * It is filed under DHCP and not given a section, for the reason it was put on
 * the DHCP page to begin with: dns and gateway read the adopted set too, but
 * this is where it is needed *first*, and a nav entry visited once and never
 * again would be a section in name only. Both the DHCP and DNS landing pages
 * link here when nothing is adopted yet, so the path to it does not depend on
 * guessing which section hides it.
 */
export function DhcpInterfacesPage() {
  const config = useDhcpConfig()

  return (
    <SubPage section="/dhcp" slug="interfaces">
      {/* The card carries its own error and empty states, and its own writes:
          adoption is the link module's config, not this one's. */}
      <InterfacesCard dhcp={config.data} />
    </SubPage>
  )
}
