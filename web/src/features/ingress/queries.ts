import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api } from '@/lib/api'
import type { IngressApplyResult, IngressProviders, IngressStatus } from '@/lib/api-types'
import type { IngressConfig, Service } from '@/lib/config-types'

// The same polling story as the other modules: EventSource cannot send an
// Authorization header, so streaming from the browser needs a cookie session or
// a fetch-based reader, and neither is worth inventing before there is live data
// to carry.
//
// This screen polls more slowly than the firewall's, and for the opposite
// reason. Nothing here changes second to second — a certificate moves once every
// sixty days — but two things do move on their own: a certificate appears a
// minute or two after the module is first enabled, and a device's address can
// change under a published service. Thirty seconds is fast enough that an
// operator who has just switched this on sees the certificate arrive without
// reloading, and slow enough not to spend a request a second on a number
// measured in days.
const OBSERVED_REFETCH_MS = 30_000

export const ingressKeys = {
  config: ['ingress', 'config'] as const,
  status: ['ingress', 'status'] as const,
  providers: ['ingress', 'providers'] as const,
}

export function useIngressConfig() {
  return useQuery({
    queryKey: ingressKeys.config,
    queryFn: () => api.get<IngressConfig>('/api/ingress/config'),
  })
}

export function useIngressStatus() {
  return useQuery({
    queryKey: ingressKeys.status,
    queryFn: () => api.get<IngressStatus>('/api/ingress/status'),
    // Observed state, never cached by the daemon (design.md §4.5), so the only
    // way to stay current is to ask again.
    refetchInterval: OBSERVED_REFETCH_MS,
  })
}

/**
 * Which DNS providers the installed proxy was built with.
 *
 * Fetched rather than bundled, and that is the whole design of this field. olr
 * ships no proxy, so the legal set is a property of the operator's binary — the
 * config schema publishes no enum for exactly this reason, and a dropdown built
 * from a compiled-in list would offer names their Caddy does not have.
 *
 * `retry: false` because the expected failure is a 503 meaning "there is no
 * proxy here yet", which is a normal state on a fresh box and not worth three
 * attempts. The message that comes back is the one telling them how to get one.
 */
export function useIngressProviders() {
  return useQuery({
    queryKey: ingressKeys.providers,
    queryFn: () => api.get<IngressProviders>('/api/ingress/providers'),
    retry: false,
    staleTime: Infinity,
  })
}

/**
 * One change, described the way the API names it.
 *
 * A value rather than a call, because a change has to survive being held: when
 * the daemon answers 409 because the change would take a published name away,
 * the page shows the plan and then sends *the same change* again with
 * `confirm=true`. Replaying is only simple if the change is data.
 */
export interface IngressChange {
  method: 'PUT' | 'DELETE' | 'PATCH'
  path: string
  body?: unknown
  /** What this change is, for the toast. */
  label: string
}

const base = '/api/ingress'
const seg = (s: string) => encodeURIComponent(s)

/**
 * The changes this screen can make, each naming the thing it changes.
 *
 * The split is not arbitrary. RFC 7386 merges an object key by key but replaces
 * an array wholesale, so the certificate settings go through PATCH and the
 * services get a route per item. It is also what keeps the rules on the daemon's
 * side: `saveService` posts to the name's own path, and reducing `grafana` and
 * `grafana.home.example.com` to one entry is the daemon's job, not this file's.
 */
export const ingressChange = {
  saveService: (name: string, service: Service): IngressChange => ({
    method: 'PUT',
    path: `${base}/services/${seg(name)}`,
    body: service,
    label: `${name} published`,
  }),
  removeService: (name: string): IngressChange => ({
    method: 'DELETE',
    path: `${base}/services/${seg(name)}`,
    label: `${name} is no longer published`,
  }),
  /**
   * The certificate half, patched key by key.
   *
   * Note what this cannot accidentally do: a patch that does not mention
   * `provider_token` leaves the stored credential alone, and one that sends it
   * back as the mask the API handed out means the same thing. So a form can
   * round-trip every field on this card without a way to destroy the credential
   * by saving a screen it only read.
   */
  settings: (fields: Partial<IngressConfig>): IngressChange => ({
    method: 'PATCH',
    path: `${base}/config`,
    body: fields,
    label: 'Saved',
  }),
}

/**
 * Sends one change, optionally already confirmed.
 *
 * There is no separate preview call. The daemon plans before it writes and
 * refuses on its own when the plan is disruptive, so the round trip that would
 * be spent asking is only spent when the answer is "ask the operator". Every
 * other edit lands first time.
 */
export function useApplyIngressChange() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: ({ change, confirm }: { change: IngressChange; confirm?: boolean }) =>
      api.send<IngressApplyResult>(
        change.method,
        confirm ? `${change.path}?confirm=true` : change.path,
        change.body,
      ),
    onSettled: () => {
      // Invalidated on failure too: a partial apply already rewrote files and
      // may have signalled the proxy, so every observed answer is stale whether
      // or not the call succeeded.
      queryClient.invalidateQueries({ queryKey: ['ingress'] })
    },
  })
}

/**
 * Re-render and reload from stored intent, changing nothing.
 *
 * The repair path design.md §5.3.2 asks for in place of rollback, and on this
 * screen it has one concrete job: a Caddyfile somebody edited by hand shows as
 * drift, and this is the button that puts it back. It needs no confirmation —
 * the daemon treats it as already confirmed, because the intent being re-applied
 * is intent the operator stored earlier.
 */
export function useReapplyIngress() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: () => api.send<IngressApplyResult>('POST', `${base}/apply`, undefined),
    onSettled: () => queryClient.invalidateQueries({ queryKey: ['ingress'] }),
  })
}
