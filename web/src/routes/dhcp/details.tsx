import { SubPage } from '@/components/layout/sub-page'
import { DhcpDetailsContent } from '@/routes/dhcp/index'

export function DhcpDetailsPage() {
  return <SubPage section="/gateway/dhcp" slug="details"><DhcpDetailsContent /></SubPage>
}
