import { AlertTriangle, Info, Search } from 'lucide-react'
import { useMemo, useState } from 'react'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { List, ListEmpty, ListRow } from '@/components/ui/list'
import { Skeleton } from '@/components/ui/skeleton'
import { DeviceDetail } from '@/features/devices/device-detail'
import { DeviceIcon } from '@/features/devices/device-icon'
import { categoryLabel } from '@/features/devices/icons'
import {
  useApplyDevicesConfig,
  useDeviceList,
  useDevicesConfig,
} from '@/features/devices/queries'
import { useDhcpConfig } from '@/features/dhcp/queries'
import { ReservationDialog } from '@/features/dhcp/reservation-dialog'
import { useDhcpApply } from '@/features/dhcp/use-apply'
import { ApiError } from '@/lib/api'
import type { DeviceRow } from '@/lib/api-types'
import type { Device, DevicesConfig, Reservation } from '@/lib/config-types'

/**
 * The device list — the object design.md §4.4 says the operator most wants, and
 * which §10 decision 6 blocked until this module existed to own identity.
 *
 * Two write paths meet on this screen and they are deliberately not the same:
 *
 *   Naming and categorising go to `devices`, which has no backend. Impact is
 *   always none, so they apply instantly with no confirmation (§5.1).
 *
 *   Setting a fixed address goes to `dhcp`, which does. It runs through dhcp's
 *   plan and its impact gate, so a change that would disconnect somebody still
 *   asks first (§5.3.3) even though it was started from here.
 */
export function DevicesPage() {
  const list = useDeviceList()
  const identity = useDevicesConfig()
  const saveIdentity = useApplyDevicesConfig()

  const dhcpConfig = useDhcpConfig()
  const dhcpApplier = useDhcpApply()

  const [query, setQuery] = useState('')
  const [editing, setEditing] = useState<DeviceRow | null>(null)
  const [reserving, setReserving] = useState<DeviceRow | null>(null)

  // Read straight off the query result rather than defaulting to a fresh []
  // here: a new array identity on every render would make the memo below
  // recompute every time regardless of whether anything changed.
  const devices = useMemo(() => list.data?.devices ?? [], [list.data?.devices])

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return devices
    // Matched across every field an operator might recall the device by. They
    // remember "the printer", "the .50 one", or a MAC off a label, and which
    // one they reach for is not predictable.
    return devices.filter((d) =>
      [
        d.name,
        d.mac,
        d.hostname ?? '',
        d.vendor ?? '',
        categoryLabel(d.category),
        d.fixed_ip ?? '',
        ...(d.ips ?? []),
      ]
        .join(' ')
        .toLowerCase()
        .includes(q),
    )
  }, [devices, query])

  if (list.isPending) return <PageSkeleton />
  if (list.isError) {
    return (
      <Alert variant="destructive">
        <AlertTriangle />
        <AlertTitle>Could not load the device list</AlertTitle>
        <AlertDescription>{(list.error as Error).message}</AlertDescription>
      </Alert>
    )
  }

  /** Stores one device's identity by rewriting the whole document. */
  async function save(device: Device) {
    const current: DevicesConfig = identity.data ?? {}
    const rest = (current.devices ?? []).filter((d) => d.mac !== device.mac)

    // An entry with nothing but a MAC says nothing, so saving one is a request
    // to forget rather than to store an empty record.
    const empty = !device.name && !device.category && !device.notes && !device.model
    const next: DevicesConfig = {
      ...current,
      devices: empty ? rest : [...rest, device],
    }

    try {
      await saveIdentity.mutateAsync(next)
      toast.success(empty ? 'Device forgotten' : 'Saved')
    } catch (error) {
      if (error instanceof ApiError) {
        toast.error(error.message, {
          description:
            error.problems.map((p) => `${p.path ?? ''} ${p.message}`.trim()).join('\n') ||
            undefined,
        })
      } else {
        toast.error(String(error))
      }
    }
  }

  /** Fixed addresses belong to dhcp, so this goes through dhcp's apply. */
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

  const online = devices.filter((d) => d.online).length

  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-2xl font-semibold tracking-tight">Devices</h1>
        <p className="text-sm text-muted-foreground">
          Everything on your network — what it is, what it is called, and whether
          it is here.
        </p>
      </header>

      {list.data?.problems?.map((p, i) => (
        <Alert key={i}>
          <Info />
          <AlertTitle>Part of the network is not visible</AlertTitle>
          <AlertDescription>{p.message}</AlertDescription>
        </Alert>
      ))}

      <div className="flex items-center gap-3">
        <div className="relative min-w-0 flex-1">
          <Search
            className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground"
            aria-hidden
          />
          <Input
            className="pl-9"
            placeholder="Search by name, address or hardware"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            aria-label="Search devices"
          />
        </div>
        <div className="hidden shrink-0 text-sm text-muted-foreground sm:block">
          {online} of {devices.length} here
        </div>
      </div>

      {filtered.length === 0 ? (
        <ListEmpty>
          {devices.length === 0
            ? 'Nothing has appeared on the network yet. Devices show up here as soon as they ask for an address.'
            : `Nothing matches “${query}”.`}
        </ListEmpty>
      ) : (
        <List>
          {filtered.map((device) => (
            <DeviceListRow key={device.mac} device={device} onSelect={() => setEditing(device)} />
          ))}
        </List>
      )}

      {list.data && (
        <p className="text-[0.8rem] text-muted-foreground">
          Read {new Date(list.data.as_of).toLocaleTimeString()}. Devices that
          never ask for an address appear once you add them.
        </p>
      )}

      {editing && (
        <DeviceDetail
          // Keyed by MAC so opening a different row rebuilds the form rather
          // than showing the previous device's draft in it.
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
    </div>
  )
}

function DeviceListRow({ device, onSelect }: { device: DeviceRow; onSelect: () => void }) {
  const address = device.fixed_ip ?? device.ips?.[0]

  return (
    <ListRow
      leading={<DeviceIcon category={device.category} online={device.online} />}
      title={
        <span className="flex items-center gap-2">
          <span className="truncate">{device.name || device.mac}</span>
          {device.fixed_ip && (
            <Badge variant="outline" className="shrink-0">
              Fixed
            </Badge>
          )}
        </span>
      }
      subtitle={
        <span className="flex items-center gap-1.5">
          <StatusDot device={device} />
          <span className="truncate">
            {categoryLabel(device.category)}
            {address && <span className="font-mono"> · {address}</span>}
          </span>
        </span>
      }
      trailing={device.online ? 'Here' : device.seen ? 'Away' : 'Never seen'}
      onSelect={onSelect}
    />
  )
}

/**
 * Presence as a dot rather than only as the trailing word.
 *
 * The word is hidden below `sm` by ListRow, so on a phone the dot is the only
 * indicator left — and it is paired with text in the same row rather than
 * carrying the meaning by colour alone.
 */
function StatusDot({ device }: { device: DeviceRow }) {
  return (
    <span
      className={
        device.online
          ? 'size-1.5 shrink-0 rounded-full bg-success'
          : 'size-1.5 shrink-0 rounded-full bg-muted-foreground/35'
      }
      aria-hidden
    />
  )
}

function PageSkeleton() {
  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <Skeleton className="h-8 w-32" />
        <Skeleton className="h-4 w-80 max-w-full" />
      </div>
      <Skeleton className="h-9 w-full rounded-md" />
      <div className="divide-y divide-border overflow-hidden rounded-xl border">
        {Array.from({ length: 5 }).map((_, i) => (
          <div key={i} className="flex min-h-14 items-center gap-3 bg-card px-4 py-2.5">
            <Skeleton className="size-11 shrink-0 rounded-md" />
            <div className="flex-1 space-y-1.5">
              <Skeleton className="h-4 w-36" />
              <Skeleton className="h-3 w-48 max-w-full" />
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
