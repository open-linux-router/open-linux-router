import { t, useLocale } from '@/lib/i18n'
import { Badge } from '@/components/ui/badge'
import type { SocksStatus } from '@/lib/api-types'

export function ProxyBadge({ status }: { status?: Pick<SocksStatus, 'enabled' | 'binary_error' | 'service' | 'service_error'> }) {
  useLocale()
  if (!status) return null
  if (status.binary_error) return <Badge variant="destructive">{t('Not installed')}</Badge>
  if (!status.enabled || status.service_error) return null
  return status.service?.active ? <Badge variant="secondary">{t('Running')}</Badge> : <Badge variant="destructive">{t('Stopped')}</Badge>
}
