import { useQuery } from '@tanstack/react-query'

import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { api } from '@/lib/api'
import { formatBytes } from '@/lib/utils'

interface Bucket { at: string; bytes: { up: number; down: number } }

const fills = ['bg-muted', 'bg-success/20', 'bg-success/40', 'bg-success/70', 'bg-success']

export function DeviceActivity({ addresses }: { addresses: string[] }) {
  const query = new URLSearchParams()
  addresses.slice(0, 32).forEach(address => query.append('address', address))
  const history = useQuery({
    queryKey: ['gateway', 'activity', query.toString()],
    queryFn: () => api.get<Bucket[]>(`/api/gateway/activity?${query}`),
    refetchInterval: 60000,
  })
  const buckets = new Map(history.data?.map(bucket => [Date.parse(bucket.at), bucket.bytes]))
  const now = new Date()
  const cells = Array.from({ length: 14 * 6 }, (_, i) => {
    const day = now.getDate() - 13 + Math.floor(i / 6)
    const hour = (i % 6) * 4
    const at = new Date(now.getFullYear(), now.getMonth(), day, hour)
    const end = new Date(now.getFullYear(), now.getMonth(), day, hour + 4)
    if (at > now) return <span key={i} aria-hidden="true" />
    let bytes: Bucket['bytes'] | undefined
    if (!history.isError) {
      for (const [timestamp, sample] of buckets) {
        if (timestamp >= at.getTime() && timestamp < end.getTime() && timestamp <= now.getTime()) {
          bytes ??= { up: 0, down: 0 }
          bytes.up += sample.up
          bytes.down += sample.down
        }
      }
    }
    const total = bytes ? bytes.up + bytes.down : 0
    const level = total === 0 ? 0 : total < 50 * 1024 ** 2 ? 1 : total < 250 * 1024 ** 2 ? 2 : total < 1024 ** 3 ? 3 : 4
    const time = `${at.toLocaleDateString()} ${at.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}–${end.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}`
    const detail = history.isPending ? 'Loading activity' : history.isError ? 'Activity unavailable' : !bytes ? 'No samples for these addresses' : `↓ ${formatBytes(bytes.down)} · ↑ ${formatBytes(bytes.up)} · Sampled traffic${end > now ? ' · In progress' : ''}`
    return <Tooltip key={i}>
      <TooltipTrigger delay={0} aria-label={`${time}: ${detail}`} className={`size-2 min-h-0 shrink-0 rounded-full p-0 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring ${fills[level]}`} />
      <TooltipContent className="flex-col items-start animate-none!"><span>{time}</span><span>{detail}</span></TooltipContent>
    </Tooltip>
  })
  return <div className="absolute right-0 top-0 w-max [mask-image:linear-gradient(to_right,transparent,black_40%)]" role="group" aria-label="Traffic activity, last 14 days, oldest to newest from left to right, six four-hour periods per day from top to bottom">
    <div className="grid grid-flow-col grid-cols-[repeat(14,8px)] grid-rows-[repeat(6,8px)] gap-1 py-0.5">{cells}</div>
  </div>
}
