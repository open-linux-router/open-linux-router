import { t, useLocale } from '@/lib/i18n'
import { StatusPill } from '@/components/ui/status'
import { state } from '@/lib/state'
import type { SocksStatus } from '@/lib/api-types'

export function ProxyBadge({ status }: { status?: Pick<SocksStatus, 'enabled' | 'binary_error' | 'service' | 'service_error'> }) {
  useLocale()
  if (!status) return null
  if (status.binary_error) return <StatusPill tone="fault">{t('Not installed')}</StatusPill>
  if (!status.enabled || status.service_error) return null
  const health = state(status.service?.active ? 'Running' : 'Not running')
  return <StatusPill tone={health.tone}>{health.label}</StatusPill>
}
