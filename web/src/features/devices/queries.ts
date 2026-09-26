import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api, ApiError } from '@/lib/api'
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

/*
 * Groups, one item at a time.
 *
 * These go to the per-item routes rather than through useApplyDevicesConfig,
 * for the reason internal/devices/http.go gives: a whole-document PUT is two
 * requests with the lock covering only the second, and a rename has to carry
 * every member along — the server does that under its lock, and a client
 * splicing the document itself would be a second copy of the cascade.
 *
 * Every one invalidates all of ['devices'], not just the config: the list rows
 * carry `group` too, and the map is drawn from the list.
 */

const groupPath = (name: string) => `/api/devices/groups/${encodeURIComponent(name)}`

function useDevicesMutation<T>(fn: (vars: T) => Promise<DevicesApplyResult>) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: fn,
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ['devices'] })
    },
  })
}

/**
 * Creates a group, inside another when a parent is given. Creating one that
 * already exists is a no-op, not an error.
 */
export function useCreateDeviceGroup() {
  return useDevicesMutation(({ name, parent }: { name: string; parent?: string }) =>
    api.put<DevicesApplyResult>(groupPath(name), parent ? { parent } : {}),
  )
}

/** Renames a group; its members follow on the server. */
export function useRenameDeviceGroup() {
  return useDevicesMutation(({ from, to }: { from: string; to: string }) =>
    api.put<DevicesApplyResult>(groupPath(from), { name: to }),
  )
}

/**
 * Moves a group inside another, or to the top with an empty parent. Its
 * members and subgroups travel with it; the server refuses a move that would
 * put a group inside itself or nest deeper than four levels.
 */
export function useMoveDeviceGroup() {
  return useDevicesMutation(({ name, parent }: { name: string; parent: string }) =>
    api.put<DevicesApplyResult>(groupPath(name), { parent }),
  )
}

/**
 * Deletes a group. What it held moves up one level — its devices and
 * subgroups go to its parent, or to the top — rather than the delete being
 * refused.
 */
export function useDeleteDeviceGroup() {
  return useDevicesMutation((name: string) => api.delete<DevicesApplyResult>(groupPath(name)))
}

/** Puts one device in a group, or takes it out of its group with an empty name. */
export function useSetDeviceGroup() {
  return useDevicesMutation(({ mac, group }: { mac: string; group: string }) =>
    api.put<DevicesApplyResult>(`/api/devices/devices/${encodeURIComponent(mac)}/group`, {
      group,
    }),
  )
}

/**
 * The server's words, most specific first. A 422 carries the reason in its
 * problems and only "invalid devices configuration" in its message, which on
 * its own would tell the operator nothing about what to change.
 */
export function describeError(error: unknown): string {
  if (error instanceof ApiError) {
    return error.problems.length ? error.problems.map((p) => p.message).join('; ') : error.message
  }
  return String(error)
}
