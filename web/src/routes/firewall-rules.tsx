import { t, useLocale } from '@/lib/i18n'
import { PageHeading } from '@/components/layout/page-heading'
import { NativeRules } from '@/features/firewall/native-rules'

export function FirewallRulesPage() {
  useLocale()
  return (
    <div className="space-y-8">
      <PageHeading title={t('Firewall rules')} />
      <NativeRules />
    </div>
  )
}
