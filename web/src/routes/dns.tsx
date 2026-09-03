import { AlertTriangle, Check, Plus, X } from 'lucide-react'
import { useEffect, useState } from 'react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Disclosure } from '@/components/ui/disclosure'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { List, ListEmpty, ListRow } from '@/components/ui/list'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { ActivityCard } from '@/features/dns/activity'
import { PlanDiff, PlanReasons, impactHint } from '@/features/dns/impact'
import { PolicyDialog } from '@/features/dns/policy-dialog'
import { useDnsConfig, useDnsStatus } from '@/features/dns/queries'
import { RELAY_UNIT, RESOLVER_UNIT, UnitLabel, serviceOf } from '@/features/dns/units'
import { useDnsApply } from '@/features/dns/use-apply'
import type { DnsStatus } from '@/lib/api-types'
import type { DnsConfig, Policy, UpstreamMode } from '@/lib/config-types'

/**
 * DNS is a top-level section, beside Addresses rather than under it, so this
 * page has to answer "what is my network's DNS doing?" on its own.
 *
 * Three jobs, and they are not equal. What it is doing (two daemons, drift, how
 * names get resolved) is a header. What it saw — the query log and the
 * domain→address map — is the whole reason olr owns :53 at all
 * (docs/dns.md §4), so it comes second and above every configuration form. What
 * it blocks follows, then the settings that are genuinely settings.
 */
export function DnsPage() {
  const config = useDnsConfig()
  const status = useDnsStatus()
  const applier = useDnsApply()

  if (config.isPending) return <PageSkeleton />
  if (config.isError) {
    return (
      <Alert variant="destructive">
        <AlertTriangle />
        <AlertTitle>Could not load the DNS settings</AlertTitle>
        <AlertDescription>{(config.error as Error).message}</AlertDescription>
      </Alert>
    )
  }

  const current = config.data

  /** Every edit is a whole new config sent through the same path. */
  const change = (next: DnsConfig) => applier.submit(next)

  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-2xl font-semibold tracking-tight">DNS</h1>
        <p className="text-sm text-muted-foreground">
          Every device on your network looks up names through this router. This
          is what they asked for, and what they were allowed to reach.
        </p>
      </header>

      {applier.failure && (
        <Alert variant="destructive" role="alert">
          <AlertTriangle />
          <AlertTitle>Only part of the change went through</AlertTitle>
          <AlertDescription className="space-y-3">
            <p>
              The steps that finished have already taken effect and will not be
              undone. Fixing the cause and applying again is safe — it picks up
              where this left off.
            </p>
            <Disclosure summary="Which steps ran">
              <ul className="space-y-1 pt-1 font-mono text-xs">
                {applier.failure.steps?.map((step) => (
                  <li key={step.description} className="flex gap-2">
                    {step.done ? (
                      <Check className="mt-0.5 size-3.5 shrink-0 text-success" aria-label="done" />
                    ) : (
                      <X className="mt-0.5 size-3.5 shrink-0 text-destructive" aria-label="failed" />
                    )}
                    <span>
                      {step.description}
                      {step.error && <span className="block opacity-80">{step.error}</span>}
                    </span>
                  </li>
                ))}
              </ul>
            </Disclosure>
            <Button size="sm" variant="outline" onClick={applier.dismissFailure}>
              Dismiss
            </Button>
          </AlertDescription>
        </Alert>
      )}

      <StatusCard
        config={current}
        status={status.data}
        error={status.error as Error | null}
        busy={applier.busy}
        onChange={change}
      />

      <ActivityCard config={current} busy={applier.busy} onChange={change} />

      <BlockingCard config={current} onChange={change} busy={applier.busy} />

      <ResolvingCard config={current} onChange={change} busy={applier.busy} />

      <ListeningCard config={current} onChange={change} busy={applier.busy} />

      <RedirectCard config={current} onChange={change} busy={applier.busy} />

      <AdvancedCard config={current} onChange={change} busy={applier.busy} />

      <ConfirmDialog applier={applier} />
    </div>
  )
}

/* -------------------------------------------------------------------------- */

function StatusCard({
  config,
  status,
  error,
  busy,
  onChange,
}: {
  config: DnsConfig
  status?: DnsStatus
  error: Error | null
  busy: boolean
  onChange: (next: DnsConfig) => void
}) {
  const summary = describeStatus(config, status, error)

  // Reported on its own line rather than folded into the headline: a unit that
  // is running now and not enabled costs nothing until the next reboot, and
  // then costs the whole network its name resolution at once.
  const notAtBoot = (status?.services ?? []).filter((s) => s.status && !s.status.enabled)

  return (
    <Card>
      <CardContent className="space-y-4">
        <div className="flex items-center gap-3">
          <span className={`size-2.5 shrink-0 rounded-full ${summary.dot}`} aria-hidden />
          <div className="min-w-0 flex-1">
            <div className="font-medium">{summary.headline}</div>
            <div className="text-sm text-muted-foreground">{summary.detail}</div>
          </div>
          <Label htmlFor="dns-enabled" className="sr-only">
            Answer DNS for this network
          </Label>
          <Switch
            id="dns-enabled"
            checked={config.enabled}
            disabled={busy}
            onCheckedChange={(enabled) => onChange({ ...config, enabled })}
          />
        </div>

        {status?.drifted && !status.drift_error && (
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant="warning">Not yet applied</Badge>
            <span className="text-sm text-muted-foreground">
              What is running is behind these settings.
            </span>
          </div>
        )}

        {/* Tinted rather than given an Alert variant of its own: the shared
            component only ships default and destructive, and this is not
            destructive — nothing is wrong yet, which is exactly the problem. */}
        {notAtBoot.length > 0 && (
          <Alert className="border-warning/40 bg-warning/10 text-warning-foreground">
            <AlertTriangle />
            <AlertTitle>DNS will not come back after a reboot</AlertTitle>
            <AlertDescription className="text-warning-foreground/90">
              {notAtBoot.map((s) => (
                <p key={s.unit}>
                  {s.status?.installed === false
                    ? `${UnitLabel(s.unit)} is not installed on this box — reinstall the olr package.`
                    : `${UnitLabel(s.unit)} is running, but is not set to start at boot.`}
                </p>
              ))}
            </AlertDescription>
          </Alert>
        )}

        <Disclosure summary="Technical details">
          <dl className="space-y-1.5 pt-1 text-xs text-muted-foreground">
            {/* Both backends, separately. Averaging them would hide the thing
                worth knowing: the resolver behind dying and the thing owning
                :53 dying are different faults with different fixes, and only
                one of them is ours. */}
            {(status?.services ?? []).map((s) => (
              <Detail key={s.unit} term={s.unit}>
                {s.status
                  ? `${s.status.state}${s.status.sub_state ? ` (${s.status.sub_state})` : ''}`
                  : (s.error ?? 'unknown')}
              </Detail>
            ))}
            <Detail term="Resolving by">{describeUpstream(config)}</Detail>
            <Detail term="Matches what is running">
              {status?.drift_error
                ? `unknown — ${status.drift_error}`
                : status?.drifted
                  ? 'no'
                  : 'yes'}
            </Detail>
            <Detail term="Read at">
              {status ? new Date(status.as_of).toLocaleTimeString() : '—'}
            </Detail>
            <p className="pt-1">
              Read from the system on every request and never cached, so this is
              what is true right now rather than what was last written.
            </p>
          </dl>
        </Disclosure>
      </CardContent>
    </Card>
  )
}

function Detail({ term, children }: { term: string; children: React.ReactNode }) {
  return (
    <div className="flex gap-2">
      <dt className="shrink-0">{term}:</dt>
      <dd className="min-w-0 break-words font-mono">{children}</dd>
    </div>
  )
}

/**
 * The headline.
 *
 * Two daemons make this harder than dhcp's version, and the extra cases are the
 * point rather than an inconvenience. "Devices can reach us but we cannot look
 * anything up" and "devices cannot reach us at all" both present to a human as
 * the internet being broken, and they have completely different fixes.
 *
 * "We could not tell" stays a third answer throughout (design.md §5.4): a box
 * with no system bus reports unknown, and flattening that into "stopped" would
 * put a red dot on a working router.
 */
function describeStatus(
  config: DnsConfig,
  status: DnsStatus | undefined,
  error: Error | null,
): { headline: string; detail: string; dot: string } {
  if (error) {
    return { headline: 'Cannot reach the router', detail: error.message, dot: 'bg-destructive' }
  }
  if (!status) {
    return {
      headline: 'Checking…',
      detail: 'Reading the current state.',
      dot: 'bg-muted-foreground/40',
    }
  }
  if (!config.enabled) {
    return {
      headline: 'Off',
      detail: 'Devices cannot look up names through this router.',
      dot: 'bg-muted-foreground/40',
    }
  }

  const relay = serviceOf(status, RELAY_UNIT)
  const resolver = serviceOf(status, RESOLVER_UNIT)

  if (!relay?.status && !resolver?.status) {
    return {
      headline: 'Status unknown',
      detail: relay?.error ?? resolver?.error ?? 'The router could not read the service state.',
      dot: 'bg-warning',
    }
  }
  if (relay?.status && !relay.status.active) {
    return {
      headline: 'Not answering',
      detail: 'Turned on, but nothing is serving DNS. Devices cannot look up names.',
      dot: 'bg-destructive',
    }
  }
  if (resolver?.status && !resolver.status.active) {
    return {
      headline: 'Answering, but nothing is resolving',
      detail:
        'Devices reach this router, but it cannot look anything up behind the scenes. Lookups will fail.',
      dot: 'bg-destructive',
    }
  }
  return {
    headline: 'Answering queries',
    detail: describeUpstream(config),
    dot: 'bg-success',
  }
}

function describeUpstream(config: DnsConfig): string {
  const upstream = config.upstream
  if ((upstream.mode || 'recurse') === 'forward') {
    const servers = upstream.servers?.length ? upstream.servers.join(', ') : 'nowhere yet'
    return upstream.tls
      ? `Forwarding to ${servers} over an encrypted connection.`
      : `Forwarding to ${servers}.`
  }
  return 'Looking names up from the root servers itself.'
}

/* -------------------------------------------------------------------------- */

function BlockingCard({
  config,
  onChange,
  busy,
}: {
  config: DnsConfig
  onChange: (next: DnsConfig) => void
  busy: boolean
}) {
  const [editing, setEditing] = useState<Policy | undefined>(undefined)
  const [open, setOpen] = useState(false)
  const policies = config.policies ?? []

  function upsert(policy: Policy) {
    const rest = policies.filter((p) => p.name !== policy.name)
    onChange({ ...config, policies: [...rest, policy] })
  }

  function remove(name: string) {
    onChange({ ...config, policies: policies.filter((p) => p.name !== name) })
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Blocking</CardTitle>
        <CardDescription>
          What each set of devices is allowed to look up. Anything blocked here
          never resolves, whatever app asked for it.
        </CardDescription>
        <CardAction>
          <Button
            size="sm"
            disabled={busy}
            onClick={() => {
              setEditing(undefined)
              setOpen(true)
            }}
          >
            <Plus className="size-4" aria-hidden /> Add
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent>
        {policies.length === 0 ? (
          <ListEmpty>No rules yet. Every device may look up anything.</ListEmpty>
        ) : (
          <List>
            {policies.map((p) => (
              <ListRow
                key={p.name}
                title={p.name}
                // Not a dash for the client-less rule: an operator scanning this
                // needs to see which one is the catch-all, and "—" reads as
                // "nobody" rather than "everybody".
                subtitle={
                  p.clients?.length ? p.clients.join(', ') : 'Everyone not covered by another rule'
                }
                trailing={`${p.block?.length ?? 0} blocked${
                  p.allow?.length ? ` · ${p.allow.length} exception${p.allow.length === 1 ? '' : 's'}` : ''
                }`}
                onSelect={
                  busy
                    ? undefined
                    : () => {
                        setEditing(p)
                        setOpen(true)
                      }
                }
              />
            ))}
          </List>
        )}
      </CardContent>

      <PolicyDialog
        key={editing?.name ?? 'new'}
        open={open}
        onOpenChange={setOpen}
        initial={editing}
        onSubmit={upsert}
        onRemove={editing ? () => remove(editing.name) : undefined}
      />
    </Card>
  )
}

/* -------------------------------------------------------------------------- */

const MODE_OPTIONS: { value: Exclude<UpstreamMode, ''>; label: string; hint: string }[] = [
  {
    value: 'recurse',
    label: 'Find answers itself',
    hint: 'Asks the root servers directly. Nobody else sees everything your network looks up.',
  },
  {
    value: 'forward',
    label: 'Ask another server',
    hint: 'Sends every lookup to the servers you name. Faster from cold, and the only way to use their filtering.',
  },
]

const MODE_LABEL = new Map(MODE_OPTIONS.map((o) => [o.value as string, o.label]))

function ResolvingCard({
  config,
  onChange,
  busy,
}: {
  config: DnsConfig
  onChange: (next: DnsConfig) => void
  busy: boolean
}) {
  const upstream = config.upstream
  const forwarding = (upstream.mode || 'recurse') === 'forward'

  const set = (next: Partial<typeof upstream>) =>
    onChange({ ...config, upstream: { ...upstream, ...next } })

  return (
    <Card>
      <CardHeader>
        <CardTitle>How names get resolved</CardTitle>
        <CardDescription>Where this router goes to turn a name into an address.</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid gap-2">
          <Label htmlFor="dns-mode">Method</Label>
          <Select
            value={upstream.mode || 'recurse'}
            onValueChange={(v) => set({ mode: v as UpstreamMode })}
          >
            <SelectTrigger id="dns-mode" className="w-full sm:max-w-sm" disabled={busy}>
              <SelectValue>
                {(value: string) => MODE_LABEL.get(value) ?? MODE_OPTIONS[0].label}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              {MODE_OPTIONS.map(({ value, label, hint }) => (
                <SelectItem key={value} value={value}>
                  <span className="flex flex-col gap-0.5">
                    <span>{label}</span>
                    <span className="text-xs text-muted-foreground">{hint}</span>
                  </span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        {forwarding && (
          <>
            <EditableField
              id="dns-servers"
              label="Servers"
              busy={busy}
              placeholder="1.1.1.1, 9.9.9.9"
              stored={(upstream.servers ?? []).join(', ')}
              onSave={(value) => set({ servers: splitList(value) })}
              hint="Comma separated. A server without a port means 53, or 853 when encrypted."
            />

            <div className="flex items-center gap-3">
              <div className="min-w-0 flex-1">
                <div className="text-sm font-medium">Encrypt the connection</div>
                <p className="text-xs text-muted-foreground">
                  Without this, every name your network looks up is visible to
                  whoever carries the traffic — which is most of what choosing a
                  particular server was meant to avoid.
                </p>
              </div>
              <Switch
                id="dns-tls"
                aria-label="Encrypt the connection to the upstream servers"
                checked={upstream.tls ?? false}
                disabled={busy}
                onCheckedChange={(tls) => set({ tls })}
              />
            </div>

            {upstream.tls && (
              <EditableField
                id="dns-tls-name"
                label="Certificate name"
                busy={busy}
                placeholder="cloudflare-dns.com"
                stored={upstream.tls_name ?? ''}
                onSave={(value) => set({ tls_name: value.trim() || undefined })}
                hint="Without this the connection is encrypted but not verified — you cannot tell you are talking to the server you meant."
              />
            )}
          </>
        )}
      </CardContent>
    </Card>
  )
}

/* -------------------------------------------------------------------------- */

function ListeningCard({
  config,
  onChange,
  busy,
}: {
  config: DnsConfig
  onChange: (next: DnsConfig) => void
  busy: boolean
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Where it answers</CardTitle>
        <CardDescription>
          Which addresses this router serves DNS on, and who is allowed to ask.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <EditableField
          id="dns-listen"
          label="Listening on"
          busy={busy}
          placeholder="192.168.1.1:53"
          stored={(config.listen ?? []).join(', ')}
          onSave={(value) => onChange({ ...config, listen: splitList(value) })}
          hint="Comma separated, and always specific addresses. A resolver reachable from the internet is something other people can abuse to attack a third party, so there is deliberately no way to say 'everywhere' here."
        />

        <EditableField
          id="dns-allow"
          label="Who may ask"
          busy={busy}
          placeholder="the networks it listens on"
          stored={(config.allow_from ?? []).join(', ')}
          onSave={(value) => onChange({ ...config, allow_from: splitList(value) })}
          hint="Ranges, comma separated. Blank means the networks of the addresses above — not everybody. Anything else is dropped without an answer."
        />
      </CardContent>
    </Card>
  )
}

/* -------------------------------------------------------------------------- */

function RedirectCard({
  config,
  onChange,
  busy,
}: {
  config: DnsConfig
  onChange: (next: DnsConfig) => void
  busy: boolean
}) {
  const hijack = config.hijack
  const set = (next: Partial<typeof hijack>) =>
    onChange({ ...config, hijack: { ...hijack, ...next } })

  return (
    <Card>
      <CardHeader>
        <CardTitle>Catch devices that ignore this router</CardTitle>
        <CardDescription>
          Handing out a DNS server is only advice. Browsers and phones routinely
          go straight to a public resolver instead — and when they do, nothing
          here applies to them and nothing here can see them.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex items-center gap-3">
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium">Redirect their lookups back here</div>
            <p className="text-xs text-muted-foreground">
              Anything sent to another DNS server is answered by this router
              instead. Devices need no configuration and notice nothing.
            </p>
          </div>
          <Switch
            id="dns-hijack"
            aria-label="Redirect lookups sent to other DNS servers"
            checked={hijack.enabled}
            disabled={busy}
            onCheckedChange={(enabled) => set({ enabled })}
          />
        </div>

        {hijack.enabled && (
          <>
            <EditableField
              id="dns-hijack-interfaces"
              label="On these networks"
              busy={busy}
              placeholder="lan0, lan1"
              stored={(hijack.interfaces ?? []).join(', ')}
              onSave={(value) => set({ interfaces: splitList(value) })}
              hint="Comma separated, and required — leaving it blank does not mean 'everywhere', because that reading would capture your internet connection too."
            />

            <div className="flex items-center gap-3">
              <div className="min-w-0 flex-1">
                <div className="text-sm font-medium">Also block encrypted DNS</div>
                <p className="text-xs text-muted-foreground">
                  A device that cannot be redirected can still slip out over an
                  encrypted connection. Blocking that makes it wait, give up, and
                  fall back to this router.
                </p>
              </div>
              <Switch
                id="dns-blockdot"
                aria-label="Block encrypted DNS"
                checked={hijack.block_dot ?? false}
                disabled={busy}
                onCheckedChange={(block_dot) => set({ block_dot })}
              />
            </div>
          </>
        )}
      </CardContent>
    </Card>
  )
}

/* -------------------------------------------------------------------------- */

function AdvancedCard({
  config,
  onChange,
  busy,
}: {
  config: DnsConfig
  onChange: (next: DnsConfig) => void
  busy: boolean
}) {
  const stored = config.extra_unbound_conf ?? ''
  const entries = config.query_log.entries ?? 0

  return (
    <Card>
      <CardContent>
        {/* Open by default only when there is something in it — an operator who
            has used this wants to see it; one who never has should not have to
            scroll past an unbound textarea to reach the end of the page. */}
        <Disclosure summary="Advanced" defaultOpen={stored !== ''}>
          <div className="space-y-5 pt-2">
            <EditableField
              id="dns-log-entries"
              label="How many lookups to keep"
              busy={busy}
              placeholder="5000"
              stored={entries ? String(entries) : ''}
              onSave={(value) =>
                onChange({
                  ...config,
                  query_log: { ...config.query_log, entries: Number(value.trim()) || undefined },
                })
              }
              hint="Blank for the default of 5000. A count rather than a length of time, because the log lives in memory and the number that matters is the one bounding it."
            />

            <div className="space-y-3">
              <Label htmlFor="dns-extra">Extra unbound configuration</Label>
              <p className="text-sm text-muted-foreground">
                Settings this router does not model, passed straight through to
                unbound. Kept here rather than hand-edited into the daemon's
                file, so they stay part of your configuration and survive an
                upgrade. Directives the router writes itself are refused.
              </p>
              <EditableField
                id="dns-extra"
                busy={busy}
                multiline
                stored={stored}
                onSave={(value) =>
                  onChange({ ...config, extra_unbound_conf: value || undefined })
                }
              />
            </div>
          </div>
        </Disclosure>
      </CardContent>
    </Card>
  )
}

/* -------------------------------------------------------------------------- */

/**
 * A text field that saves on a button rather than as you type.
 *
 * design.md §5.1 wants the GUI to apply instantly, and switches here do. Free
 * text cannot: every keystroke of "192.168.1.1:53" is an invalid address, so
 * applying per character would mean a plan request per keystroke and a toast
 * storm of validation errors on the way to a correct value. The dhcp page draws
 * the same line in the same place.
 */
function EditableField({
  id,
  label,
  hint,
  placeholder,
  stored,
  onSave,
  busy,
  multiline,
}: {
  id: string
  label?: string
  hint?: React.ReactNode
  placeholder?: string
  stored: string
  onSave: (value: string) => void
  busy: boolean
  multiline?: boolean
}) {
  const [draft, setDraft] = useState(stored)

  // Re-sync when the value changes underneath — after an apply lands, or when
  // another client changed it and the poll noticed.
  useEffect(() => setDraft(stored), [stored])

  const dirty = draft !== stored

  return (
    <div className="grid gap-2">
      {label && <Label htmlFor={id}>{label}</Label>}
      {multiline ? (
        <Textarea
          id={id}
          className="min-h-28 font-mono text-xs"
          spellCheck={false}
          placeholder={placeholder}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
        />
      ) : (
        <Input
          id={id}
          placeholder={placeholder}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
        />
      )}
      {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
      {dirty && (
        <div className="flex justify-end gap-2">
          <Button variant="ghost" size="sm" onClick={() => setDraft(stored)}>
            Revert
          </Button>
          <Button size="sm" disabled={busy} onClick={() => onSave(draft)}>
            Save
          </Button>
        </div>
      )}
    </div>
  )
}

function splitList(value: string): string[] | undefined {
  const items = value
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
  return items.length ? items : undefined
}

/* -------------------------------------------------------------------------- */

function ConfirmDialog({ applier }: { applier: ReturnType<typeof useDnsApply> }) {
  const pending = applier.confirming

  return (
    <Dialog open={pending !== null} onOpenChange={(open) => !open && applier.cancel()}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <AlertTriangle className="size-4 text-destructive" aria-hidden />
            This will break the internet for everyone
          </DialogTitle>
          <DialogDescription>
            {pending && impactHint(pending.plan.impact)} Every other change
            applies the moment you make it — this one asks first.
          </DialogDescription>
        </DialogHeader>

        {/* The reasons stay in front of the operator; the diff is the evidence
            behind them. design.md §6.4 needs the diff to stay inspectable, so
            it is one click away rather than the first thing in the dialog. */}
        {pending && (
          <div className="space-y-2">
            <PlanReasons plan={pending.plan} />
            <Disclosure summary="Show exactly what will change">
              <div className="max-h-[45vh] overflow-auto pt-2">
                <PlanDiff plan={pending.plan} />
              </div>
            </Disclosure>
          </div>
        )}

        <DialogFooter>
          <Button variant="ghost" onClick={applier.cancel}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={applier.confirm}>
            Apply anyway
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/* -------------------------------------------------------------------------- */

function PageSkeleton() {
  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <Skeleton className="h-8 w-24" />
        <Skeleton className="h-4 w-80 max-w-full" />
      </div>
      <Skeleton className="h-32 w-full rounded-xl" />
      <Skeleton className="h-96 w-full rounded-xl" />
      <Skeleton className="h-48 w-full rounded-xl" />
    </div>
  )
}
