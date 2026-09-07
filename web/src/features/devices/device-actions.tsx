import { useState } from 'react'
import { toast } from 'sonner'

import { DeviceDetail } from '@/features/devices/device-detail'
import { useApplyDevicesConfig, useDevicesConfig } from '@/features/devices/queries'
import { useDhcpConfig } from '@/features/dhcp/queries'
import { ReservationDialog } from '@/features/dhcp/reservation-dialog'
import { useDhcpApply } from '@/features/dhcp/use-apply'
import { ApiError } from '@/lib/api'
import type { DeviceRow } from '@/lib/api-types'
import type { Device, DevicesConfig, Reservation } from '@/lib/config-types'

/**
 * Acting on one device, wherever it was clicked.
 *
 * Split out from the list it used to live in so the map can open the same two
 * dialogs. One owner of the write paths, rather than a second copy that could
 * drift — and the two paths stay deliberately different:
 *
 *   Naming and categorising go to `devices`, which has no backend. Impact is
 *   always none, so they apply instantly with no confirmation (§5.1).
 *
 *   Setting a fixed address goes to `dhcp`, which does. It runs through dhcp's
 *   plan and its impact gate, so a change that would disconnect somebody still
 *   asks first (§5.3.3) even though it was started from a device.
 */
export function useDeviceActions() {
  const identity = useDevicesConfig()
  const saveIdentity = useApplyDevicesConfig()
  const dhcpConfig = useDhcpConfig()
  const dhcpApplier = useDhcpApply()

  const [editing, setEditing] = useState<DeviceRow | null>(null)
  const [reserving, setReserving] = useState<DeviceRow | null>(null)

  /** Stores one device's identity by rewriting the whole document. */
  async function save(device: Device) {
    const current: DevicesConfig = identity.data ?? {}
    const rest = (current.devices ?? []).filter((d) => d.mac !== device.mac)

    // An entry with nothing but a MAC says nothing, so saving one is a request
    // to forget rather than to store an empty record.
    const empty = !device.name && !device.category && !device.notes && !device.model
    const next: DevicesConfig = { ...current, devices: empty ? rest : [...rest, device] }

    try {
      await saveIdentity.mutateAsync(next)
      toast.success(empty ? 'Device forgotten' : 'Saved')
    } catch (error) {
      if (error instanceof ApiError) {
        toast.error(error.message, {
          description:
            error.problems.map((p) => `${p.path ?? ''} ${p.message}`.trim()).join('\n') || undefined,
        })
      } else {
        toast.error(String(error))
      }
    }
  }

  function saveReservation(reservation: Reservation) {
    const current = dhcpConfig.data
    if (!current) return
    const rest = (current.reservations ?? []).filter(
      (r) => r.mac.toLowerCase() !== reservation.mac.toLowerCase(),
    )
    dhcpApplier.submit({ ...current, reservations: [...rest, reservation] })
  }

  function removeReservation(mac: string) {
    const current = dhcpConfig.data
    if (!current) return
    dhcpApplier.submit({
      ...current,
      reservations: (current.reservations ?? []).filter(
        (r) => r.mac.toLowerCase() !== mac.toLowerCase(),
      ),
    })
  }

  const dialogs = (
    <>
      {editing && (
        <DeviceDetail
          // Keyed by MAC so opening a different device rebuilds the form rather
          // than showing the previous one's draft in it.
          key={editing.mac}
          device={editing}
          open
          onOpenChange={(open) => !open && setEditing(null)}
          busy={saveIdentity.isPending}
          onSave={save}
          onForget={editing.stored ? () => save({ mac: editing.mac }) : undefined}
          onEditFixedAddress={() => {
            setReserving(editing)
            setEditing(null)
          }}
        />
      )}

      {reserving && (
        <ReservationDialog
          key={reserving.mac}
          open
          onOpenChange={(open) => !open && setReserving(null)}
          initial={
            reserving.fixed_ip
              ? { mac: reserving.mac, ip: reserving.fixed_ip }
              : // Pre-filled with the device's MAC: this is the whole point of
                // §11.1 — the operator never goes and finds a hardware address.
                { mac: reserving.mac, ip: '' }
          }
          onSubmit={saveReservation}
          onRemove={reserving.fixed_ip ? () => removeReservation(reserving.mac) : undefined}
        />
      )}
    </>
  )

  return { select: setEditing, dialogs }
}
