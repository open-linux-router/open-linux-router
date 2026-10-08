import { SubPage } from '@/components/layout/sub-page'
import { DnsDetailsContent } from '@/routes/dns/index'

export function DnsDetailsPage() {
  return <SubPage section="/gateway/dns" slug="details"><DnsDetailsContent /></SubPage>
}
