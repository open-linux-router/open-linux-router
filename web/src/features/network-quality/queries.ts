import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api } from '@/lib/api'
import type { QualitySnapshot } from './types'

const key = ['tools', 'quality'] as const

export function useNetworkQuality() {
  return useQuery({
    queryKey: key,
    queryFn: () => api.get<QualitySnapshot>('/api/tools/quality'),
    refetchInterval: query => query.state.data?.state === 'running' ? 1000 : 15000,
  })
}

export function useQualityAction() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (action: { cancel: true } | { speed: boolean }) =>
      'cancel' in action
        ? api.delete<QualitySnapshot>('/api/tools/quality')
        : api.post<QualitySnapshot>(`/api/tools/quality?speed=${action.speed}`),
    onSuccess: result => client.setQueryData(key, result),
    onSettled: () => client.invalidateQueries({ queryKey: key }),
  })
}
