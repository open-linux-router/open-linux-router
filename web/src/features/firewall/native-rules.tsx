import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { api } from '@/lib/api'
import type { FirewallPlan } from '@/lib/api-types'
import {
  nativeJSON,
  parseNativeJSON,
  ownedRuleset,
  parseDocument,
  ruleFlow,
  ruleKey,
  type NativeView,
  type NftObject,
} from './native-model'

export function NativeRules() {
  const client = useQueryClient()
  const query = useQuery({
    queryKey: ['firewall', 'ruleset'],
    queryFn: () =>
      api.get<NativeView>('/api/firewall/ruleset', parseNativeJSON),
    refetchInterval: 10000,
  })
  const [draft, setDraft] = useState<string | null>(null)
  const [selected, setSelected] = useState<string | null>(null)
  const [plan, setPlan] = useState<FirewallPlan | null>(null)
  const [error, setError] = useState('')
  const [revision, setRevision] = useState('')
  const [ruleDraft, setRuleDraft] = useState<{
    index: number
    text: string
  } | null>(null)
  const check = useMutation({
    mutationFn: () =>
      api.send<FirewallPlan>(
        'PATCH',
        '/api/firewall/config?dry_run=true',
        { ruleset: parseDocument(draft!) },
        { 'If-Match': `"${revision}"` },
        nativeJSON,
      ),
    onSuccess: (result) => {
      setPlan(result)
      setError('')
    },
    onError: (err) => setError(err.message),
  })
  const apply = useMutation({
    mutationFn: () =>
      api.send(
        'PATCH',
        '/api/firewall/config?confirm=true',
        { ruleset: parseDocument(draft!) },
        { 'If-Match': `"${revision}"` },
        nativeJSON,
      ),
    onSuccess: () => {
      setDraft(null)
      setPlan(null)
      setError('')
      void client.invalidateQueries({ queryKey: ['firewall'] })
    },
    onError: (err) => setError(err.message),
  })

  if (!query.data)
    return (
      <section className="rounded-xl border p-4">
        <h2 className="font-medium">nftables rules</h2>
        <p className="mt-2 text-sm text-muted-foreground">
          {query.error ? query.error.message : 'Reading the kernel ruleset…'}
        </p>
        <Button
          variant="outline"
          className="mt-3"
          onClick={() => void query.refetch()}
        >
          Refresh
        </Button>
      </section>
    )
  const view = query.data
  const document = view.ruleset
  const chains = document.nftables.flatMap((entry) =>
    entry.chain ? [entry.chain] : [],
  )
  const tables = document.nftables.flatMap((entry) =>
    entry.table ? [entry.table] : [],
  )
  const rules = document.nftables.flatMap((entry, index) =>
    entry.rule ? [{ rule: entry.rule, key: ruleKey(entry.rule, index) }] : [],
  )
  const flows = rules.flatMap((item) => {
    const flow = ruleFlow(item.rule)
    return flow ? [{ ...item, ...flow }] : []
  })
  function edit() {
    setRevision(view.revision)
    const live = ownedRuleset(document)
    setDraft(nativeJSON(live.nftables.length ? live : (view.saved ?? live), 2))
    setRuleDraft(null)
    setPlan(null)
    setError('')
  }
  function updateDraft(
    index: number,
    action: 'edit' | 'remove' | 'up' | 'down',
  ) {
    try {
      const doc = parseDocument(draft!)
      const rule = doc.nftables[index].rule
      if (action === 'edit') {
        setRuleDraft({ index, text: nativeJSON(rule, 2) })
        return
      }
      if (action === 'remove') doc.nftables.splice(index, 1)
      else {
        const peers = doc.nftables.flatMap((entry, i) =>
          entry.rule &&
          entry.rule.chain === rule.chain &&
          entry.rule.table === rule.table &&
          entry.rule.family === rule.family
            ? [i]
            : [],
        )
        const target = peers[peers.indexOf(index) + (action === 'up' ? -1 : 1)]
        if (target === undefined) return
        const previous = doc.nftables[index]
        doc.nftables[index] = doc.nftables[target]
        doc.nftables[target] = previous
      }
      setDraft(nativeJSON(doc, 2))
      setPlan(null)
      setError('')
    } catch (err) {
      setError(String(err))
    }
  }
  function saveRuleDraft() {
    try {
      const doc = parseDocument(draft!)
      const rule = parseNativeJSON(ruleDraft!.text) as NftObject
      if (!rule || typeof rule !== 'object' || Array.isArray(rule))
        throw new Error('Expected a native rule object.')
      const before = doc.nftables[ruleDraft!.index].rule
      if (
        rule.family !== before.family ||
        rule.table !== before.table ||
        rule.chain !== before.chain
      )
        throw new Error(
          'Keep family, table and chain unchanged here; use the full document editor to move a rule to another chain.',
        )
      doc.nftables[ruleDraft!.index] = { rule }
      setDraft(nativeJSON(doc, 2))
      setRuleDraft(null)
      setPlan(null)
      setError('')
    } catch (err) {
      setError(String(err))
    }
  }
  let draftRules: { rule: NftObject; index: number }[] = []
  let draftChains: NftObject[] = []
  if (draft !== null) {
    try {
      const doc = parseDocument(draft)
      draftRules = doc.nftables.flatMap((entry, index) =>
        entry.rule ? [{ rule: entry.rule, index }] : [],
      )
      draftChains = doc.nftables.flatMap((entry) =>
        entry.chain ? [entry.chain] : [],
      )
    } catch {
      /* The document editor displays validation errors on request. */
    }
  }
  function locate(key: string) {
    setSelected(key)
    window.setTimeout(
      () =>
        documentElement(key)?.scrollIntoView({
          behavior: 'instant',
          block: 'center',
        }),
      0,
    )
  }
  function documentElement(key: string) {
    return window.document.getElementById(`nft-${key}`)
  }

  return (
    <section className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-base font-medium">nftables rules</h2>
          <p className="mt-1 text-sm text-muted-foreground">
            {view.native
              ? 'Native rules saved by you. Service changes do not regenerate this table.'
              : 'Automatic rules. Editing takes ownership of the current olr_filter table.'}
          </p>
        </div>
        <Button
          variant="outline"
          onClick={edit}
          disabled={draft !== null || !!query.error}
        >
          Edit olr_filter
        </Button>
      </div>
      {query.error && (
        <p role="alert" className="text-sm text-destructive">
          Refresh failed: {query.error.message}. Showing the last successful
          read.
        </p>
      )}

      {draft !== null && (
        <div className="space-y-3 rounded-xl border p-4">
          <label htmlFor="native-nft-json" className="text-sm font-medium">
            Native nftables JSON — inet olr_filter
          </label>
          <p className="text-sm text-muted-foreground">
            Edit rules, chains, sets and their order directly. This document
            persists across restarts. Other tables remain unchanged.
          </p>
          {draftChains.map((chain) => (
            <div
              key={String(chain.name)}
              className="space-y-2 rounded-lg border p-3"
            >
              <div className="flex flex-wrap items-center justify-between gap-2">
                <h3 className="font-mono text-sm">
                  Draft chain {String(chain.name)}
                </h3>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={!!ruleDraft || apply.isPending || check.isPending}
                  onClick={() => {
                    const doc = parseDocument(draft)
                    const newRule = {
                      family: chain.family,
                      table: chain.table,
                      chain: chain.name,
                      expr: [{ counter: null }],
                    }
                    const peers = doc.nftables.flatMap((entry, index) =>
                      entry.rule?.chain === chain.name ? [index] : [],
                    )
                    const index = peers.length
                      ? peers[peers.length - 1] + 1
                      : doc.nftables.length
                    doc.nftables.splice(index, 0, { rule: newRule })
                    setDraft(nativeJSON(doc, 2))
                    setPlan(null)
                    setRuleDraft({
                      index,
                      text: nativeJSON(newRule, 2),
                    })
                  }}
                >
                  Add rule
                </Button>
              </div>
              {draftRules
                .filter(({ rule }) => rule.chain === chain.name)
                .map(({ rule, index }, order) => (
                  <div
                    key={index}
                    className="flex flex-wrap items-center gap-2 text-xs"
                  >
                    <span className="min-w-0 flex-1 break-words">
                      {order + 1}.{' '}
                      {String(
                        rule.comment ??
                          ruleFlow(rule)?.conditions ??
                          'Native expression',
                      )}
                    </span>
                    {(['edit', 'up', 'down', 'remove'] as const).map(
                      (action) => (
                        <Button
                          key={action}
                          variant="outline"
                          size="sm"
                          disabled={
                            !!ruleDraft || apply.isPending || check.isPending
                          }
                          onClick={() => updateDraft(index, action)}
                        >
                          {action}
                        </Button>
                      ),
                    )}
                  </div>
                ))}
            </div>
          ))}
          {ruleDraft && (
            <div className="space-y-2 rounded-lg border p-3">
              <label htmlFor="native-rule-json" className="text-sm font-medium">
                Edit native rule object
              </label>
              <Textarea
                id="native-rule-json"
                spellCheck={false}
                className="h-64 font-mono text-xs [field-sizing:fixed]"
                value={ruleDraft.text}
                onChange={(event) =>
                  setRuleDraft({ ...ruleDraft, text: event.target.value })
                }
              />
              <Button onClick={saveRuleDraft}>Update draft rule</Button>
              <Button
                variant="outline"
                className="ml-2"
                onClick={() => setRuleDraft(null)}
              >
                Close rule editor
              </Button>
            </div>
          )}
          <Textarea
            id="native-nft-json"
            spellCheck={false}
            value={draft}
            className="h-96 resize-y font-mono text-xs [field-sizing:fixed]"
            disabled={!!ruleDraft || apply.isPending || check.isPending}
            onChange={(event) => {
              setDraft(event.target.value)
              setPlan(null)
              setError('')
            }}
          />
          {error && (
            <p role="alert" className="break-words text-sm text-destructive">
              {error}
            </p>
          )}
          <div className="flex flex-wrap gap-2">
            <Button
              onClick={() => check.mutate()}
              disabled={!!ruleDraft || check.isPending || apply.isPending}
            >
              {check.isPending ? 'Checking…' : 'Validate and preview'}
            </Button>
            <Button
              variant="outline"
              onClick={() => {
                setDraft(null)
                setPlan(null)
                setError('')
                setRuleDraft(null)
              }}
              disabled={apply.isPending || check.isPending}
            >
              Cancel
            </Button>
          </div>
          {plan && (
            <div className="space-y-3 rounded-lg bg-muted p-3">
              <p className="text-sm">
                nft validation passed.{' '}
                {plan.empty
                  ? 'No kernel changes.'
                  : 'The table will be replaced atomically; rule counters restart.'}
              </p>
              {plan.warnings?.map((warning, index) => (
                <p key={index} className="text-sm">
                  {warning.message}
                </p>
              ))}
              <Button
                variant="destructive"
                disabled={!!ruleDraft || check.isPending || apply.isPending}
                onClick={() => apply.mutate()}
              >
                {apply.isPending ? 'Applying…' : 'Save and apply native rules'}
              </Button>
            </div>
          )}
        </div>
      )}

      {tables.map((table) => (
        <div
          key={`${table.family}/${table.name}`}
          className="space-y-3 rounded-xl border p-4"
        >
          <h3 className="font-mono text-sm font-medium">
            table {String(table.family)} {String(table.name)}{' '}
            <span className="font-sans text-xs text-muted-foreground">
              {table.family === 'inet' && table.name === 'olr_filter'
                ? view.native
                  ? '· User managed'
                  : '· Generated by OLR'
                : '· External — view only'}
            </span>
          </h3>
          {chains
            .filter(
              (chain) =>
                chain.family === table.family && chain.table === table.name,
            )
            .map((chain) => (
              <div key={String(chain.name)} className="space-y-2">
                <h4 className="break-words font-mono text-sm">
                  chain {String(chain.name)}{' '}
                  {chain.hook
                    ? `· ${chain.type} hook ${chain.hook} priority ${chain.prio} · policy ${chain.policy ?? 'accept'}`
                    : '· regular chain'}
                </h4>
                {rules
                  .filter(
                    ({ rule }) =>
                      rule.family === chain.family &&
                      rule.table === chain.table &&
                      rule.chain === chain.name,
                  )
                  .map(({ rule, key }, index) => (
                    <details
                      key={key}
                      id={`nft-${key}`}
                      open={selected === key || undefined}
                      className={`rounded-lg border p-3 ${selected === key ? 'border-ring bg-muted' : ''}`}
                    >
                      <summary className="cursor-pointer break-words text-sm focus-visible:outline-2 focus-visible:outline-ring">
                        <span className="font-mono">
                          {index + 1}. handle {String(rule.handle ?? '—')}
                        </span>{' '}
                        · {String(rule.comment ?? 'No comment')} ·{' '}
                        {ruleFlow(rule)?.verdict ??
                          'Complex / non-terminal rule'}
                      </summary>
                      <p className="mt-2 break-words font-mono text-xs text-muted-foreground">
                        {ruleFlow(rule)?.conditions ?? nativeJSON(rule.expr)}
                      </p>
                      <pre className="mt-3 max-h-80 overflow-auto whitespace-pre-wrap break-all text-xs">
                        {nativeJSON(rule, 2)}
                      </pre>
                    </details>
                  ))}
              </div>
            ))}
          <details>
            <summary className="cursor-pointer text-sm">
              All table objects, including sets and counters
            </summary>
            <pre className="mt-3 max-h-96 overflow-auto whitespace-pre-wrap break-all text-xs">
              {nativeJSON(
                document.nftables.filter((entry) =>
                  Object.values(entry).some(
                    (obj: NftObject) =>
                      obj.family === table.family &&
                      (obj.table === table.name || obj.name === table.name),
                  ),
                ),
                2,
              )}
            </pre>
          </details>
        </div>
      ))}
      {!tables.length && (
        <p className="text-sm text-muted-foreground">
          No nftables tables are currently installed.
        </p>
      )}

      <details className="rounded-xl border p-4">
        <summary className="cursor-pointer text-sm font-medium">
          Rule flows · {flows.length} of {rules.length} rules can be shown
        </summary>
        <p className="mt-3 text-xs text-muted-foreground">
          Each ribbon references one kernel rule. Equal widths are schematic.
          These are rule matches, not a simulation of packet paths: earlier
          rules, jumps and other base chains affect the final verdict.
        </p>
        <div className="mt-4 space-y-2">
          {flows.map((flow) => (
            <button
              key={flow.key}
              onClick={() => locate(flow.key)}
              className="grid min-h-16 w-full cursor-pointer grid-cols-[1fr_64px_auto] items-center gap-3 rounded-lg border p-3 text-left hover:bg-muted focus-visible:outline-2 focus-visible:outline-ring"
            >
              <span className="min-w-0 break-words text-xs">
                <span className="block font-mono font-medium">
                  {String(flow.rule.family)} {String(flow.rule.table)} /{' '}
                  {String(flow.rule.chain)} · handle {String(flow.rule.handle)}
                </span>
                {flow.conditions}
              </span>
              <svg aria-hidden="true" viewBox="0 0 64 32" className="h-8 w-16">
                <path
                  d="M0 4 C32 4 32 16 64 16 L64 28 C32 28 32 16 0 16 Z"
                  fill="currentColor"
                  className={
                    flow.verdict === 'accept'
                      ? 'text-primary/40'
                      : 'text-destructive/40'
                  }
                />
              </svg>
              <span className="text-xs font-medium">{flow.verdict}</span>
            </button>
          ))}
        </div>
        {!flows.length && (
          <p className="mt-3 text-sm text-muted-foreground">
            No rules can be represented without simplifying their expressions.
            All rules remain visible above.
          </p>
        )}
      </details>
    </section>
  )
}
