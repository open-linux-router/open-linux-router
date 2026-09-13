import { ListEmpty } from '@/components/ui/list'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { gatewayChange, type GatewayChange } from '@/features/gateway/queries'
import type { AssignmentStatus } from '@/lib/api-types'
import type { GatewayConfig } from '@/lib/config-types'

// Sentinel values for the two choices that are not an exit name.
//
// The **leading space is load-bearing** and is not a typo to tidy away: an exit
// name is trimmed by Normalize and required non-empty by Validate, so no real
// name can begin with one, which is what makes these impossible to collide with
// an exit somebody actually called "inherit". The empty string is not available
// — the Select treats it as "nothing chosen" — so a distinguishable non-empty
// value is needed for each.
export const INHERIT = ' inherit'
export const DIRECT = ' direct'

export /**
 * One row per network, each with the sentence docs/gateway.md §1.3 settles on.
 *
 * The effective value carries its source, which is the whole reason
 * inheritance is usable here at all: *Clash · from the box-wide setting* tells
 * an operator both what is happening and where to go to change it, without
 * anyone having to simulate a rule list.
 */
function NetworkList({
  config,
  status,
  busy,
  onChange,
}: {
  config: GatewayConfig
  status?: AssignmentStatus[]
  busy: boolean
  onChange: (change: GatewayChange) => void
}) {
  const assignments = config.interfaces ?? []
  const exits = config.exits ?? []

  if (assignments.length === 0) {
    return (
      <ListEmpty>
        No networks yet. Networks appear here once this router knows about them.
      </ListEmpty>
    )
  }

  function set(iface: string, value: string | null) {
    // An empty exit is a real value, not a missing one: it records "this network
    // explicitly follows the box-wide setting". Dropping the row entirely is a
    // different statement, and there is a DELETE for it.
    const exit = !value || value === INHERIT ? '' : value
    onChange(gatewayChange.assign(iface, exit))
  }

  // Not a List: these rows carry an interactive control, and List's trailing
  // slot is `hidden sm:block` — which would leave the setting unreachable on a
  // phone, on the one screen whose whole purpose is changing it.
  return (
    <ul className="divide-y overflow-hidden rounded-xl border">
      {assignments.map((a) => {
        const row = status?.find((s) => s.interface === a.interface)
        return (
          <li
            key={a.interface}
            className="flex flex-col gap-2 bg-card px-4 py-3 sm:flex-row sm:items-center sm:gap-3"
          >
            <div className="min-w-0 flex-1">
              <div className="truncate text-sm font-medium">{a.interface}</div>
              <div className="truncate text-[0.8rem] text-muted-foreground">
                {describeAssignment(row)}
              </div>
            </div>
            <Select
              value={a.exit ? a.exit : INHERIT}
              disabled={busy}
              onValueChange={(v) => set(a.interface, v)}
            >
              <SelectTrigger
                className="w-full sm:w-56"
                aria-label={`Internet via, for ${a.interface}`}
              >
                <SelectValue>
                  {(v: string) => (v === INHERIT ? 'Follow the setting above' : v)}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={INHERIT}>Follow the setting above</SelectItem>
                {exits.map((e) => (
                  <SelectItem key={e.name} value={e.name}>
                    {e.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </li>
        )
      })}
    </ul>
  )
}

function describeAssignment(row?: AssignmentStatus): string {
  if (!row) return ''
  if (row.reason) return row.reason
  const via = row.exit || 'this router’s own connection'
  return row.source === 'default' ? `${via} — from the setting above` : via
}
