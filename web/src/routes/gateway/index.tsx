import { GatewaySummary } from '@/features/gateway/summary'

/**
 * The gateway, on the page you land on.
 *
 * The page itself is in features/gateway/summary.tsx: it is the same status
 * strip, object list and settings entry every other section lands on, and it
 * reads the kernel for all three.
 */
export function GatewayPage() {
  return <GatewaySummary />
}
