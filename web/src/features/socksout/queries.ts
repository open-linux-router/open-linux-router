import { useQuery } from '@tanstack/react-query'

import { api } from '@/lib/api'

export type SocksOutConfig = { enabled: boolean; proxy?: string }
export type SocksOutStatus = { enabled: boolean; service: { active: boolean }; problem?: string }

export function useSocksOutConfig() {
  return useQuery({
    queryKey: ['socksout', 'config'],
    queryFn: () => api.get<SocksOutConfig>('/api/socksout/config'),
  })
}

export function useSocksOutStatus() {
  return useQuery({
    queryKey: ['socksout', 'status'],
    queryFn: () => api.get<SocksOutStatus>('/api/socksout/status'),
    refetchInterval: 5000,
  })
}
