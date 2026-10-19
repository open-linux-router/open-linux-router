import { Badge } from '@/components/ui/badge'
import type { SocksStatus } from '@/lib/api-types'

export function ProxyBadge({ status }: { status?: Pick<SocksStatus, 'enabled' | 'binary_error' | 'service' | 'service_error'> }) {
  if (!status) return null
  if (status.binary_error) return <Badge variant="destructive">Not installed</Badge>
  if (!status.enabled || status.service_error) return null
  return status.service?.active ? <Badge variant="secondary">Running</Badge> : <Badge variant="destructive">Stopped</Badge>
}
