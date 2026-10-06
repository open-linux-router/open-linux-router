import { useQuery } from '@tanstack/react-query'

import { api } from '@/lib/api'

export interface HostMetrics {
  uptime_seconds: number
  cpu_used_cores: number | null
  cpu_cores: number
  memory_used_bytes: number
  memory_total_bytes: number
}

export function useHostMetrics() {
  return useQuery({
    queryKey: ['system', 'metrics'],
    queryFn: () => api.get<HostMetrics>('/api/system/metrics'),
    refetchInterval: 5000,
    retry: false,
  })
}

export interface PublicAddresses {
  ipv4: string
  ipv6: string
}

export function usePublicAddresses() {
  return useQuery({
    queryKey: ['system', 'public-addresses'],
    queryFn: () => api.get<PublicAddresses>('/api/system/public-addresses'),
    refetchInterval: 5 * 60 * 1000,
    retry: false,
  })
}
