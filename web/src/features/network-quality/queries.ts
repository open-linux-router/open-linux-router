import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api } from '@/lib/api'
import type { QualityScope, QualitySnapshot } from './types'

const key = ['tools', 'quality'] as const

export function useNetworkQuality() {
  return useQuery({
    queryKey: key,
    queryFn: () => api.get<QualitySnapshot>('/api/tools/quality'),
    refetchInterval: query => query.state.data?.state === 'running' || query.state.data?.running_scope ? 1000 : 15000,
  })
}

export type QualityAction =
  | { cancel: true }
  | { scope: 'all' }
  | { scope: Exclude<QualityScope, 'all' | 'target'> }
  | { scope: 'target'; target: string }

function actionPath(action: Exclude<QualityAction, { cancel: true }>) {
  const params = new URLSearchParams({ scope: action.scope })
  // The page always includes the reference transfer; the API keeps the
  // explicit switch for callers that need a lighter assessment.
  if (action.scope === 'all') params.set('speed', 'true')
  if (action.scope === 'target') params.set('target', action.target)
  return `/api/tools/quality?${params.toString()}`
}

export function useQualityAction() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (action: QualityAction) =>
      'cancel' in action
        ? api.delete<QualitySnapshot>('/api/tools/quality')
        : api.post<QualitySnapshot>(actionPath(action)),
    onSuccess: result => client.setQueryData(key, result),
    onSettled: () => client.invalidateQueries({ queryKey: key }),
  })
}
