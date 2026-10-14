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
  const cells = Array.from({ length: 14 * 48 }, (_, i) => {
    const at = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 13 + Math.floor(i / 48), 0, (i % 48) * 30)
    const bytes = history.isError ? undefined : buckets.get(Math.floor(at.getTime() / 1800000) * 1800000)
    const total = bytes ? bytes.up + bytes.down : 0
    const level = total === 0 ? 0 : total < 50 * 1024 ** 2 ? 1 : total < 250 * 1024 ** 2 ? 2 : total < 1024 ** 3 ? 3 : 4
    const end = new Date(at.getTime() + 1800000)
    const time = `${at.toLocaleDateString()} ${at.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}–${end.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}`
    const detail = at > now ? 'Not yet observed' : history.isPending ? 'Loading activity' : history.isError ? 'Activity unavailable' : !bytes ? 'No samples for these addresses' : `↓ ${formatBytes(bytes.down)} · ↑ ${formatBytes(bytes.up)} · Sampled traffic${end > now ? ' · In progress' : ''}`
    return <Tooltip key={i}>
      <TooltipTrigger aria-label={`${time}: ${detail}`} className={`size-3 min-h-0 shrink-0 rounded-[3px] p-0 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring ${fills[level]} ${!bytes ? 'opacity-40' : ''}`} />
      <TooltipContent className="flex-col items-start"><span>{time}</span><span>{detail}</span></TooltipContent>
    </Tooltip>
  })
  return <div className="mt-6 overflow-x-auto border-t pt-6" role="group" aria-label="Traffic activity, last 14 days, one row per day, one cell per half hour">
    <div className="mx-auto grid w-max grid-cols-[repeat(48,12px)] gap-1 p-1">{cells}</div>
  </div>
}
