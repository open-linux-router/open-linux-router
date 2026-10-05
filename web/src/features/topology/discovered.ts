import { useQuery } from '@tanstack/react-query'

import { api } from '@/lib/api'

export interface DiscoveredWeb {
  mac: string
  ip: string
  url: string
  checked_at: string
}

export function useDiscoveredWeb() {
  return useQuery({
    queryKey: ['devices', 'web'],
    queryFn: () => api.get<DiscoveredWeb[]>('/api/devices/web'),
    refetchInterval: 60_000,
  })
}
