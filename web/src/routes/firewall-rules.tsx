import { PageHeading } from '@/components/layout/page-heading'
import { NativeRules } from '@/features/firewall/native-rules'

export function FirewallRulesPage() {
  return (
    <div className="space-y-8">
      <PageHeading title="Firewall rules" />
      <NativeRules />
    </div>
  )
}
