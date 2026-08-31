import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api } from '@/lib/api'
import type { DeviceList, DevicesApplyResult, Plan } from '@/lib/api-types'
import type { DevicesConfig } from '@/lib/config-types'

// Same polling story as features/dhcp/queries.ts: the UI asks again rather than
// subscribing, because EventSource cannot carry an Authorization header. The
// device list is observed state that the daemon never caches (design.md §4.5),
// so asking again is the only way to stay current.
const OBSERVED_REFETCH_MS = 5000

export const deviceKeys = {
  config: ['devices', 'config'] as const,
  list: ['devices', 'list'] as const,
}

/** Stored identity only — what a human has said. */
export function useDevicesConfig() {
  return useQuery({
    queryKey: deviceKeys.config,
    queryFn: () => api.get<DevicesConfig>('/api/devices/config'),
  })
}

/** The join: identity, presence and fixed addresses in one answer. */
export function useDeviceList() {
  return useQuery({
    queryKey: deviceKeys.list,
    queryFn: () => api.get<DeviceList>('/api/devices/list'),
    refetchInterval: OBSERVED_REFETCH_MS,
  })
}

export function useDevicesPlanPreview() {
  return useMutation({
    mutationFn: (config: DevicesConfig) => api.post<Plan>('/api/devices/plan', config),
  })
}

/**
 * Stores identity.
 *
 * No plan preview beforehand, unlike dhcp. This module has no backend, so its
 * impact is always `none` and there is nothing a rename could disconnect —
 * §5.1's instant apply with no confirmation is exactly right here. The one
 * action on this screen that *can* drop a client is setting a fixed address,
 * and that goes through dhcp's own apply and its impact gate.
 */
export function useApplyDevicesConfig() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (config: DevicesConfig) =>
      api.put<DevicesApplyResult>('/api/devices/config', config),
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ['devices'] })
    },
  })
}
