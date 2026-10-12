import { SubPage } from '@/components/layout/sub-page'
import { DhcpDetailsContent } from '@/routes/dhcp/index'
import { ServedNetworksContent } from '@/routes/gateway/networks'

export function DhcpDetailsPage() {
  return <SubPage section="/gateway/dhcp" slug="details"><DhcpDetailsContent /><ServedNetworksContent /></SubPage>
}
