import {
  ArrowDown,
  ArrowUp,
  ChevronDown,
  ChevronUp,
  FolderInput,
  FolderPlus,
  Layers,
  MoreHorizontal,
  Pencil,
  Plus,
  Router,
  Tag,
  Trash2,
  X,
} from 'lucide-react'
import { useEffect, useState } from 'react'

import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { DeviceIcon } from '@/features/devices/device-icon'
import type { GroupDeletion } from '@/features/devices/group-actions'
import { groupOptions, MAX_GROUP_DEPTH, parentChoices } from '@/features/devices/group-tree'

import type { Density, GroupVariant, Hidden } from '@/features/topology/layout'
import type { MapGroup } from '@/features/topology/model'
import type { Outside } from '@/features/topology/outside'
import { magnitude, type Flow, type TrafficView } from '@/features/topology/traffic'
import type { DeviceRow } from '@/lib/api-types'
import type { DevicesGroup, Service } from '@/lib/config-types'
import { getToken } from '@/lib/api'
import { cn, formatAgo, formatBytes, formatRate, formatRateCompact } from '@/lib/utils'

/**
 * What the map can ask its page to do. All optional: without them the map is
 * a picture, which is what it should be anywhere that cannot change groups.
 */
export interface MapActions {
  onSelect?: (device: DeviceRow) => void
  onCreateGroup?: (parent?: string) => void
  onRenameGroup?: (name: string) => void
  onDeleteGroup?: (deletion: GroupDeletion) => void
  onMoveGroup?: (name: string, parent: string) => void
}

/* -------------------------------------------------------------------------- */
/* The router                                                                 */
/* -------------------------------------------------------------------------- */

export interface NetworkInfo {
  name: string
  exit: string
  inherited: boolean
  down: boolean
  unchecked: boolean
}

/**
 * The router: its name and, when counting, its total rate — and a network
 * only when its way out is down. Inheritance is a dashed outline on that chip
 * and the red says it is not answering.
 */
export function RouterNode({ networks, traffic }: { networks: NetworkInfo[]; traffic: TrafficView }) {
  return (
    <div className="min-w-60 rounded-2xl border bg-card px-4 py-3 shadow-sm">
      <div className="flex items-center gap-3">
        {/* The one dark tile on the map: this box is the hardware, everything
            else is a list of what it serves. */}
        <span className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-foreground text-background">
          <Router className="size-5" aria-hidden />
        </span>
        <div className="min-w-0">
          <div className="text-sm font-semibold">This router</div>
          {traffic.rated && <Rates flow={traffic.total} className="text-xs" neutral />}
        </div>
      </div>

      {/* Only a network whose way out has stopped answering. Every network's
          route, on every visit, was small print nobody needed while it was
          fine — the Networks page has it — but one that is down is the most
          urgent fact on the map, and stays. */}
      {networks.some((n) => n.down) && (
        <ul className="mt-2.5 flex flex-wrap gap-1.5 border-t pt-2.5" aria-label="Networks that are down">
          {networks.filter((n) => n.down).map((n) => (
            <li key={n.name}>
              <NetworkChip network={n} />
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

function NetworkChip({ network: n }: { network: NetworkInfo }) {
  return (
    <span
      title={
        (n.down ? `${n.exit} is not responding. ` : '') +
        (n.inherited ? 'Follows the box-wide setting' : `Set on ${n.name}`)
      }
      className={cn(
        'inline-flex items-center gap-1.5 rounded-md border px-1.5 py-0.5 text-xs',
        n.inherited && 'border-dashed',
        n.down ? 'border-destructive/50 text-destructive' : 'text-muted-foreground',
      )}
    >
      <span
        aria-hidden
        className={cn(
          'size-1.5 rounded-full',
          n.down ? 'bg-destructive' : n.unchecked ? 'bg-muted-foreground/40' : 'bg-success',
        )}
      />
      <span className="font-mono text-foreground/85">{n.name}</span>
      <span aria-hidden>→</span>
      <span className="sr-only">goes out</span>
      {n.exit || 'direct'}
    </span>
  )
}

/* -------------------------------------------------------------------------- */
/* Observed default-route next hops                                            */
/* -------------------------------------------------------------------------- */

export function NextHopNode({ hop, onSelect }: { hop: Outside['hops'][number]; onSelect?: (device: DeviceRow) => void }) {
  const device = hop.device
  const open = device && onSelect ? () => onSelect(device) : undefined
  const name = device?.name || device?.mac || (hop.routes[0].via ? 'Gateway' : 'Direct route')
  const detail = hop.routes.map((r) => `IPv${r.family} ${r.via ?? `direct on ${hop.dev}`}${r.down ? ' (not responding)' : ''}`).join(' · ')
  const title = hop.routes.map((r) => `IPv${r.family} default route ${r.via ? `via ${r.via}` : 'without a next-hop address'} on ${hop.dev} (metric ${r.metric})`).join('; ') +
    (device ? `. Next hop matches ${name}.` : '. No device identified.')
  return (
    <Shell
      onClick={open}
      title={title}
      className={cn(
        'flex size-full min-w-0 items-center gap-3 rounded-2xl border bg-card pr-4 pl-2.5 text-left shadow-xs',
        open && 'transition-[border-color,box-shadow] hover:border-foreground/20 hover:shadow-sm',
        hop.down && 'border-destructive/50',
        focusRing,
      )}
    >
      {device ? (
        <DeviceIcon icon={device.icon} category={device.category} vendor={device.vendor}
          vendorKey={device.vendor_key} online={device.online} size="sm" />
      ) : (
        <span className={cn('flex size-8 shrink-0 items-center justify-center rounded-full',
          hop.down ? 'bg-destructive/10 text-destructive' : 'bg-muted text-foreground/75')}>
          <Router className="size-4" aria-hidden />
        </span>
      )}
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="truncate text-sm font-semibold">{name}</span>
        <span className={cn('truncate font-mono text-xs', hop.down ? 'text-destructive' : 'text-muted-foreground')}>{detail}</span>
      </span>
    </Shell>
  )
}

/* -------------------------------------------------------------------------- */

/* -------------------------------------------------------------------------- */
/* Groups                                                                     */
/* -------------------------------------------------------------------------- */

/**
 * A group, in whichever of its four shapes the layout gave it.
 *
 * The container is only a frame and a header: the devices and subgroups
 * inside it are drawn as their own nodes on top, so that each can move on its
 * own when the layout changes — a device fanning out of its container when the
 * group is focused is the same element travelling, not one disappearing and
 * another appearing. The frame's job is to be quiet: a hairline, a faint fill
 * one step darker per level of nesting, and no colour of its own. Groups used
 * to be tinted by position, and the tints said nothing a title did not while
 * competing with the one colour that means something, the red of a way out
 * that is down.
 */
export function GroupNode({
  group,
  variant,
  nested,
  dashed,
  groups,
  actions,
  onFocus,
}: {
  group: MapGroup
  variant: GroupVariant
  /** A container inside another: its list sits on a quieter fill. */
  nested?: boolean
  /** For the bucket beside the operator's groups, which is not one of them. */
  dashed: boolean
  groups: DevicesGroup[]
  actions: MapActions
  /** Clicking the group: focus it, or leave focus when it is the focused one. */
  onFocus: () => void
}) {
  const bucket = group.name === undefined

  if (variant === 'chip') {
    return (
      <button
        type="button"
        onClick={onFocus}
        title={`Focus ${group.title}`}
        className={cn(
          'flex size-full items-center gap-2 rounded-lg border bg-card px-3 text-left shadow-xs transition-colors hover:bg-accent',
          dashed && 'border-dashed',
          focusRing,
        )}
      >
        <span className="min-w-0 flex-1 truncate text-xs font-medium">{group.title}</span>
      </button>
    )
  }

  if (variant === 'card') {
    return (
      <button
        type="button"
        onClick={onFocus}
        title={`Focus ${group.title}`}
        className={cn(
          'flex size-full items-center gap-2.5 rounded-lg border bg-muted/40 px-3 text-left transition-colors hover:bg-accent',
          focusRing,
        )}
      >
        <span className="flex size-7 shrink-0 items-center justify-center rounded-md border bg-card text-muted-foreground">
          <Layers className="size-3.5" aria-hidden />
        </span>
        <span className="min-w-0 flex-1 truncate text-[13px] font-medium">{group.title}</span>
      </button>
    )
  }

  const focused = variant === 'focused'
  return (
    <section
      aria-label={group.title}
      className={cn(
        '@container relative size-full',
        focused && 'rounded-xl border bg-card shadow-sm ring-1 ring-foreground/5',
        focused && dashed && 'border-dashed',
      )}
    >
      {/* The title sits above the list, not inside a frame round it: a
          container is its list, and a box round a box round rows was the
          nesting the eye had to count its way out of. */}
      {!focused && (
        <div
          aria-hidden
          className={cn(
            'absolute inset-x-0 top-10 bottom-0 rounded-xl',
            nested ? 'bg-muted/60 dark:bg-muted/40' : 'bg-card shadow-xs ring-1 ring-foreground/[0.07]',
            dashed && 'border border-dashed bg-transparent shadow-none ring-0',
          )}
        />
      )}
      <div
        className={cn(
          'relative flex h-10 items-center gap-2',
          focused ? 'pl-3.5' : 'pl-1.5',
          focused && !bucket ? 'pr-2' : 'pr-3.5',
        )}
      >
        {/* The whole header is the button, with the menu stacked above it —
            a button inside a button is not allowed, and a title that is the
            only click target is a small one. */}
        <button
          type="button"
          onClick={onFocus}
          aria-label={focused ? `Back to every group` : `Focus ${group.title}`}
          className={cn(
            'absolute inset-0 transition-colors hover:bg-foreground/[0.03]',
            focused ? 'rounded-xl' : 'rounded-lg',
            focusRing,
          )}
        />
        {/* The name and nothing else. Counts, rates and a menu on every card
            made the map a wall of small print; the line into the card says
            how busy it is, and what can be done to a group is offered once
            somebody has chosen it, by focusing. */}
        <div
          className={cn(
            'pointer-events-none relative min-w-0 flex-1 truncate',
            focused || nested ? 'text-[13px] font-medium' : 'text-[15px] font-semibold tracking-tight',
          )}
        >
          {group.title}
        </div>
        {focused && !bucket && <GroupMenu group={group} groups={groups} actions={actions} />}
        {focused && (
          <span className="pointer-events-none relative flex size-7 items-center justify-center text-muted-foreground">
            <X className="size-4" aria-hidden />
          </span>
        )}
      </div>
    </section>
  )
}

/**
 * A group's menu: rename, make a group inside it, move it inside another, and
 * delete it. "Move inside…" lists every group it could go into, in tree order,
 * with the ones it cannot — itself, anything below it, anything too deep to
 * take everything it carries — left out or greyed.
 */
function GroupMenu({ group, groups, actions }: { group: MapGroup; groups: DevicesGroup[]; actions: MapActions }) {
  const name = group.name!
  const { onRenameGroup, onDeleteGroup, onMoveGroup, onCreateGroup } = actions
  if (!onRenameGroup && !onDeleteGroup && !onMoveGroup && !onCreateGroup) return null

  const stored = groups.find((g) => g.name === name)
  const parent = stored?.parent ?? ''
  const choices = parentChoices(groups, name)
  const depth = groupOptions(groups).find((g) => g.name === name)?.depth ?? 0

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="ghost"
            size="icon-sm"
            className="relative text-muted-foreground"
            aria-label={`${group.title} options`}
          />
        }
      >
        <MoreHorizontal />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-48">
        {onRenameGroup && (
          <DropdownMenuItem onClick={() => onRenameGroup(name)}>
            <Pencil /> Rename
          </DropdownMenuItem>
        )}
        {onCreateGroup && depth < MAX_GROUP_DEPTH - 1 && (
          <DropdownMenuItem onClick={() => onCreateGroup(name)}>
            <FolderPlus /> New group inside
          </DropdownMenuItem>
        )}
        {onMoveGroup && (
          <DropdownMenuSub>
            <DropdownMenuSubTrigger>
              <FolderInput /> Move inside…
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent className="min-w-44">
              <DropdownMenuRadioGroup
                value={parent || ' top'}
                onValueChange={(value: string) => {
                  const next = value === ' top' ? '' : value
                  if (next !== parent) onMoveGroup(name, next)
                }}
              >
                <DropdownMenuRadioItem value=" top">Top level</DropdownMenuRadioItem>
                {choices.length > 0 && <DropdownMenuSeparator />}
                {choices.map((c) => (
                  <DropdownMenuRadioItem
                    key={c.name}
                    value={c.name}
                    disabled={!c.allowed}
                    title={c.allowed ? undefined : `Groups nest at most ${MAX_GROUP_DEPTH} levels deep`}
                  >
                    <span style={{ paddingLeft: c.depth * 12 }}>{c.name}</span>
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </DropdownMenuSubContent>
          </DropdownMenuSub>
        )}
        {onDeleteGroup && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              variant="destructive"
              onClick={() =>
                onDeleteGroup({
                  name,
                  devices: group.directCount,
                  groups: group.subgroupCount,
                  parent: parent || undefined,
                })
              }
            >
              <Trash2 /> Delete
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/* -------------------------------------------------------------------------- */
/* Devices                                                                    */
/* -------------------------------------------------------------------------- */

/**
 * One device, at either density.
 *
 * Compact is an icon and a name — enough to see what is where, small enough
 * that sixty of them are still a picture. Detail adds the address, the network
 * it is on, and its live rate with a thin bar underneath; the bar is scaled
 * against the busiest device on the whole map, not in its group, so a bar means
 * the same length everywhere on screen.
 */
export function DeviceNode({
  device,
  services,
  serviceDomain,
  density,
  row,
  traffic,
  busiest,
  actions,
}: {
  device: DeviceRow
  services?: Service[]
  serviceDomain?: string
  density: Density
  /** Drawn as a row of its container's list rather than as a card of its own. */
  row?: { first: boolean }
  traffic: TrafficView
  busiest: number
  actions: MapActions
}) {
  const { onSelect } = actions
  const name = device.name || device.mac
  const flow = traffic.flowOf(device)
  const share = busiest > 0 ? magnitude(flow, traffic.rated) / busiest : 0
  const open = onSelect ? () => onSelect(device) : undefined

  if (row) {
    return (
      <div className={cn(
        'group/node relative size-full px-1 py-0.5',
        onSelect && 'rounded-lg transition-colors hover:bg-foreground/[0.035]',
      )}>
        {/* Hairlines between rows, inset past the icon the way a settings list
            draws them, and none above the first. */}
        {!row.first && <span aria-hidden className="absolute top-0 right-0 left-[60px] h-px bg-border/70" />}
        <Shell
          onClick={open}
          title={name}
          className={cn(
            'flex size-full min-w-0 items-center gap-2 rounded-lg px-3 text-left transition-colors',
            focusRing,
          )}
        >
          <DeviceIcon
            icon={device.icon}
            category={device.category}
            vendor={device.vendor}
            vendorKey={device.vendor_key}
            online={device.online}
            size="sm"
          />
          <span className="flex min-w-0 flex-1 flex-col">
            <span className={cn('truncate text-sm font-medium', !device.online && 'text-muted-foreground')}>
              {name}
            </span>
            {density === 'detail' && <Address device={device} />}
          </span>
          <span aria-hidden className={cn('shrink-0', density === 'compact' ? 'w-20' : 'w-[108px]')} />
          <span className="w-[68px] shrink-0 text-right text-xs text-muted-foreground tabular-nums whitespace-nowrap">
            {/* Away is said beside the dimmed name, so presence is never
                colour alone — and a green dot on every row that is fine was
                the one mark on the list that said nothing. */}
            <DeviceActivity device={device} flow={flow} rated={traffic.rated} counting={traffic.counting} />
          </span>
        </Shell>
        <ServiceLinks services={services} domain={serviceDomain}
          className={cn('absolute inset-y-0 right-20', density === 'compact' ? 'w-20' : 'w-[108px]')} compact />
      </div>
    )
  }

  const shell = cn(
    'flex size-full min-w-0 items-center rounded-lg border bg-card text-left shadow-xs transition-[border-color,box-shadow]',
    onSelect && 'hover:border-foreground/20 hover:shadow-sm',
    focusRing,
    services?.length && 'pb-8',
  )

  const body =
    density === 'compact' ? (
      <Shell onClick={open} title={name} className={cn(shell, 'gap-2 pr-2.5 pl-2')}>
        <DeviceIcon
          icon={device.icon}
          category={device.category}
          vendor={device.vendor}
          vendorKey={device.vendor_key}
          online={device.online}
          size="sm"
          className="size-6"
        />
        <span className={cn('min-w-0 flex-1 truncate text-xs font-medium', !device.online && 'text-muted-foreground')}>
          {name}
        </span>
        <Presence device={device} />
      </Shell>
    ) : (
      <Shell onClick={open} className={cn(shell, 'gap-2.5 px-3')}>
        <DeviceIcon
          icon={device.icon}
          category={device.category}
          vendor={device.vendor}
          vendorKey={device.vendor_key}
          online={device.online}
          size="sm"
        />
        <span className="flex min-w-0 flex-1 flex-col">
          <span className="flex min-w-0 items-center gap-1.5">
            <span className={cn('truncate text-[13px] leading-[18px] font-medium', !device.online && 'text-muted-foreground')}>
              {name}
            </span>
            <Presence device={device} />
          </span>
          <Address device={device} />
          {traffic.counting && device.seen && (
            <span className="mt-1.5 flex items-center gap-2">
              {/* Scaled against the busiest device on the whole map. A floor
                  of 2% so a device that moved *something* is visibly not the
                  same as one that moved nothing. */}
              <span aria-hidden className="h-0.5 min-w-6 flex-1 overflow-hidden rounded-full bg-muted">
                <span
                  className="block h-full rounded-full bg-foreground/35"
                  style={{ width: share > 0 ? `${Math.max(2, share * 100)}%` : 0 }}
                />
              </span>
              <span className="shrink-0 text-[10.5px] leading-3 text-muted-foreground tabular-nums">
                <DeviceTraffic device={device} flow={flow} rated={traffic.rated} counting />
              </span>
            </span>
          )}
        </span>
      </Shell>
    )

  return (
    <div className="group/node relative size-full">
      {body}
      <ServiceLinks services={services} domain={serviceDomain}
        className={cn('absolute bottom-0.5 right-2', density === 'compact' ? 'left-10' : 'left-[54px]')} />
    </div>
  )
}

/** Links sit beside, not inside, the device detail button. */
function ServiceLinks({ services, domain, className, compact = false }: { services?: Service[]; domain?: string; className: string; compact?: boolean }) {
  if (!services?.length) return null
  return (
    <span className={cn('flex min-w-0 items-center gap-1 overflow-x-auto overflow-y-hidden', compact ? 'h-full' : 'h-8', className)}>
      {services.slice(0, 8).map((service) => {
        const label = domain ? `${service.name}.${domain}` : service.name
        const version = serviceIconVersion(service, domain)
        const tile = <ServiceIcon key={`${service.name}:${version}`} name={service.name} version={version} />
        return domain ? (
          <a key={service.name} href={`https://${label}${service.link_path ?? ''}`} target="_blank" rel="noopener noreferrer"
            title={`Open ${label}${service.link_path ?? ''}`} aria-label={`Open ${label}${service.link_path ?? ''}`}
            className={cn('flex shrink-0 items-center justify-center rounded-[6px] bg-muted transition-colors hover:bg-accent focus-visible:outline-2', compact ? 'size-6' : 'size-7')}>
            {tile}
          </a>
        ) : (
          <span key={service.name} title={service.name} className={cn('flex shrink-0 items-center justify-center rounded-[6px] bg-muted', compact ? 'size-6' : 'size-7')}>{tile}</span>
        )
      })}
      {services.length > 8 && (
        <span className="flex h-7 shrink-0 items-center text-[11px] text-muted-foreground" title={services.slice(8).map((s) => s.name).join(', ')}>
          +{services.length - 8}
        </span>
      )}
    </span>
  )
}

// The browser caches successful icons for a day; change the URL when the
// published destination or its icon-discovery path changes.
function serviceIconVersion(service: Service, domain?: string): string {
  const { device, host, port, scheme } = service.upstream
  const identity = JSON.stringify([service.name, domain, service.link_path, device, host, port, scheme])
  let hash = 2166136261
  for (let i = 0; i < identity.length; i++) {
    hash = Math.imul(hash ^ identity.charCodeAt(i), 16777619)
  }
  return (hash >>> 0).toString(36)
}

function ServiceIcon({ name, version }: { name: string; version: string }) {
  const [icon, setIcon] = useState<string>()
  useEffect(() => {
    const controller = new AbortController()
    let objectURL: string | undefined
    const token = getToken()
    fetch(`/api/ingress/services/${encodeURIComponent(name)}/icon?v=${version}`, {
      signal: controller.signal,
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    }).then(async (response) => {
      if (!response.ok) return
      objectURL = URL.createObjectURL(await response.blob())
      if (!controller.signal.aborted) setIcon(objectURL)
      else URL.revokeObjectURL(objectURL)
    }).catch(() => {})
    return () => {
      controller.abort()
      if (objectURL) URL.revokeObjectURL(objectURL)
    }
  }, [name, version])
  return icon ? (
    <img src={icon} alt="" className="size-5 object-contain" />
  ) : (
    <span className="text-[10px] font-semibold uppercase" aria-hidden>{name.slice(0, 2)}</span>
  )
}

/** A button when the node does something, and only then. */
function Shell({
  onClick,
  title,
  className,
  children,
}: {
  onClick?: () => void
  title?: string
  className: string
  children: React.ReactNode
}) {
  return onClick ? (
    <button type="button" onClick={onClick} title={title} className={className}>
      {children}
    </button>
  ) : (
    <div title={title} className={className}>
      {children}
    </div>
  )
}

/** Presence as a dot, paired with the dimmed name — never colour alone. */
function Presence({ device }: { device: DeviceRow }) {
  return (
    <span
      aria-label={device.online ? 'here' : device.seen ? 'away' : 'never seen'}
      className={cn('size-1.5 shrink-0 rounded-full', device.online ? 'bg-success' : 'bg-muted-foreground/30')}
    />
  )
}

/** The second line of a detail node: the address, whole where it fits. */
function Address({ device }: { device: DeviceRow }) {
  const address = device.fixed_ip ?? device.ips?.[0]
  if (!device.seen) return <span className="truncate text-[11px] leading-4 text-muted-foreground italic">never seen</span>
  if (!address) return <span className="text-[11px] leading-4 text-muted-foreground">no address</span>
  return (
    <span
      className="flex min-w-0 items-center gap-1 text-muted-foreground"
      // The whole address on hover: a narrow node truncates it, and an
      // address is the one field nobody can guess the end of.
      title={device.fixed_ip ? `${address} (fixed)` : address}
    >
      {device.fixed_ip && <Tag className="size-3 shrink-0" aria-label="fixed" />}
      <span className="truncate font-mono text-[11px] leading-4">{address}</span>
    </span>
  )
}

/**
 * A node's figure: both rates when something is moving, and until the second
 * sample the running total — one number, because "↓ 0 bps ↑ 0 bps" before it
 * would claim an idleness nobody measured.
 *
 * When nothing is moving, or the device is away, it is when the router last
 * heard from it: "4m ago", "2h ago". That is one answer for what used to be
 * three words — idle, away, and a dash for a device never counted — and it
 * is the one that says something, since a quiet device that is here and one
 * that left at breakfast are both "not moving".
 */
function DeviceTraffic({
  device,
  flow,
  rated,
  counting,
}: {
  device: DeviceRow
  flow?: Flow
  rated: boolean
  counting: boolean
}) {
  const moving = device.online && counting && flow && (!rated || (flow.downRate ?? 0) + (flow.upRate ?? 0) >= 1)
  if (!moving) return <Quiet device={device} />
  if (!rated) return <>{formatBytes(flow.down + flow.up)}</>
  const down = flow.downRate ?? 0
  const up = flow.upRate ?? 0
  // Compact, with the exact figures on hover: the row's width belongs to the
  // device's name, and a rate only has to say "busy" or "quiet" at a glance.
  return (
    <span title={`↓ ${formatRate(down)}  ↑ ${formatRate(up)}`}>
      <span className="text-foreground/75">↓{formatRateCompact(down)}</span>
      <span className="ml-1">↑{formatRateCompact(up)}</span>
    </span>
  )
}

/** Live bit rates without the unit in the narrow list; full units remain on hover. */
function DeviceActivity({ device, flow, rated, counting }: {
  device: DeviceRow
  flow?: Flow
  rated: boolean
  counting: boolean
}) {
  const moving = device.online && counting && flow && (!rated || (flow.downRate ?? 0) + (flow.upRate ?? 0) >= 1)
  if (!moving) return <Quiet device={device} />
  if (!rated) return <span title={`${formatBytes(flow.down + flow.up)} transferred`}>active</span>
  const down = flow.downRate ?? 0
  const up = flow.upRate ?? 0
  const label = `Live speed: download ${formatRate(down)}, upload ${formatRate(up)}`
  return (
    <span className="inline-flex flex-col items-end align-middle" title={label} aria-label={label}>
      {([['↓', down], ['↑', up]] as const).map(([arrow, rate]) => (
        <span key={arrow} aria-hidden className="leading-[12px]">
          {arrow}{formatRateCompact(rate)}
        </span>
      ))}
    </span>
  )
}

/** When a device was last heard, or the word for why there is no time. */
function Quiet({ device }: { device: DeviceRow }) {
  const at = device.last_seen
  return (
    <span
      className="text-muted-foreground/60"
      title={at ? `Last heard ${new Date(at).toLocaleString()}` : undefined}
    >
      {at ? formatAgo(at) : device.online ? 'idle' : device.seen ? 'away' : 'never seen'}
    </span>
  )
}

/* -------------------------------------------------------------------------- */
/* Folds, picks and notes                                                     */
/* -------------------------------------------------------------------------- */

/** A fold counts devices and groups, but not the "Other devices" bucket. */
function describeHidden(h: Hidden, others?: boolean) {
  const devices = h.devices === 1 ? 'device' : 'devices'
  const groups = h.groups === 1 ? 'group' : 'groups'
  if (others) return h.groups > 0 ? `+${h.groups + 1} more` : 'Other devices'
  if (h.devices && h.groups) return `+${h.devices} more ${devices} and ${h.groups} ${groups}`
  if (h.groups) return `+${h.groups} more ${groups}`
  return `+${h.devices} more ${devices}`
}

export function MoreNode({
  variant,
  open,
  hidden,
  others,
  devices,
  onToggle,
}: {
  variant: 'row' | 'card'
  open: boolean
  hidden: Hidden
  others?: boolean
  devices?: number
  onToggle: () => void
}) {
  const label = open ? 'Show fewer' : describeHidden(hidden, others)
  const Icon = open ? ChevronUp : ChevronDown
  if (variant === 'row') {
    return (
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={open}
        className={cn(
          'flex size-full items-center justify-center gap-1.5 rounded-lg border border-dashed text-xs font-medium text-muted-foreground transition-colors hover:border-solid hover:bg-card hover:text-foreground',
          focusRing,
        )}
      >
        {label}
        <Icon className="size-3.5" aria-hidden />
      </button>
    )
  }
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-expanded={open}
      className={cn(
        'flex size-full flex-col items-center justify-center gap-0.5 rounded-xl border border-dashed px-3 text-center transition-colors hover:border-solid hover:bg-accent/60',
        focusRing,
      )}
    >
      <span className="flex items-center gap-1 text-[13px] font-medium">
        {label}
        <Icon className="size-3.5 text-muted-foreground" aria-hidden />
      </span>
      {!open && devices !== undefined && (
        <span className="text-xs text-muted-foreground tabular-nums">
          {devices} {devices === 1 ? 'device' : 'devices'}
        </span>
      )}
    </button>
  )
}

/**
 * The groups that did not fit beside a focused one, behind one chip. A menu
 * rather than a fold: opening them in place would push the focused group off
 * the centre it just moved to.
 */
export function PickNode({ groups, onFocus }: { groups: MapGroup[]; onFocus: (key: string) => void }) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <button
            type="button"
            className={cn(
              'flex size-full items-center justify-center gap-1 rounded-lg border border-dashed px-3 text-xs font-medium text-muted-foreground transition-colors hover:bg-accent hover:text-foreground',
              focusRing,
            )}
          />
        }
      >
        +{groups.length} more
        <ChevronDown className="size-3.5" aria-hidden />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-52">
        {groups.map((g) => (
          <DropdownMenuItem key={g.key} onClick={() => onFocus(g.key)}>
            <span className="min-w-0 flex-1 truncate">{g.title}</span>
            <span className="text-xs text-muted-foreground tabular-nums">{g.total}</span>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

export function NoteNode({
  note,
  filtering,
  onCreateGroup,
}: {
  note: 'empty' | 'hint' | 'nothing'
  filtering: boolean
  onCreateGroup?: (parent?: string) => void
}) {
  if (note === 'hint') {
    return (
      <div className="flex size-full flex-col items-start justify-center gap-2 rounded-lg border border-dashed px-3.5 text-xs text-muted-foreground sm:flex-row sm:items-center sm:gap-4">
        <p className="line-clamp-2 min-w-0 flex-1">
          Sort your devices into groups — Serving, Personal, IoT, whatever fits your house — and this map
          is drawn by them.
        </p>
        {onCreateGroup && (
          <Button size="sm" variant="outline" className="shrink-0" onClick={() => onCreateGroup()}>
            <Plus /> Create a group
          </Button>
        )}
      </div>
    )
  }
  return (
    <p className="flex size-full items-center justify-center text-xs text-muted-foreground">
      {note === 'nothing'
        ? 'No devices match.'
        : filtering
          ? 'Nothing here matches.'
          : 'Nothing in it yet. Use a device’s menu to move it here.'}
    </p>
  )
}

/* -------------------------------------------------------------------------- */
/* Numbers                                                                    */
/* -------------------------------------------------------------------------- */

/**
 * ↓ and ↑ as rates. Down first: it is the number people mean by "busy".
 *
 * `neutral` drops the arrows' colour, for the map, where no colour is spent on
 * anything that is not a fault. The stat tile above the map keeps them.
 */
export function Rates({ flow, className, neutral }: { flow: Flow; className?: string; neutral?: boolean }) {
  return (
    <span className={cn('inline-flex shrink-0 items-center gap-2 tabular-nums', className)}>
      <span className="inline-flex items-center gap-0.5">
        <ArrowDown
          className={cn('size-3', neutral ? 'text-muted-foreground' : 'text-blue-600 dark:text-blue-400')}
          aria-label="down"
        />
        {formatRate(flow.downRate ?? 0)}
      </span>
      <span className="inline-flex items-center gap-0.5 text-muted-foreground">
        <ArrowUp
          className={cn('size-3', neutral ? 'text-muted-foreground' : 'text-teal-600 dark:text-teal-400')}
          aria-label="up"
        />
        {formatRate(flow.upRate ?? 0)}
      </span>
    </span>
  )
}

const focusRing = 'outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring'
