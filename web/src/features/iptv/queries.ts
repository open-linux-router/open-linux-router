import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api } from '@/lib/api'

export interface IptvConfig {
  enabled: boolean
  upstream?: string
  networks?: string[]
  sources?: string[]
}

export interface IptvPlan {
  changes: string[] | null
  impact: string
  empty: boolean
}

export interface IptvStatus {
  enabled: boolean
  service: { active: boolean; enabled: boolean; installed: boolean; state: string }
  plan?: IptvPlan
  problem?: string
}

export interface IptvApplyResult {
  plan: IptvPlan
  steps?: { description: string; done: boolean; error?: string }[]
  error?: { message: string }
}

export function useIptvConfig() {
  return useQuery({ queryKey: ['iptv', 'config'], queryFn: () => api.get<IptvConfig>('/api/iptv/config') })
}

export function useIptvStatus() {
  return useQuery({
    queryKey: ['iptv', 'status'],
    queryFn: () => api.get<IptvStatus>('/api/iptv/status'),
    refetchInterval: 10_000,
  })
}

export function useIptvPlan() {
  return useMutation({
    mutationFn: (config: IptvConfig) => api.post<IptvPlan>('/api/iptv/plan', config),
  })
}

export function useIptvApply() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (config: IptvConfig) => api.put<IptvApplyResult>('/api/iptv/config', config),
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ['iptv'] })
      queryClient.invalidateQueries({ queryKey: ['firewall'] })
    },
  })
}
