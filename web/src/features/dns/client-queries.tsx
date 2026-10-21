import { t, useLocale } from '@/lib/i18n'
import { Search, X } from 'lucide-react'
import { useLayoutEffect, useRef, useState } from 'react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableRow } from '@/components/ui/table'
import { useDnsConfig, useDnsQueries } from '@/features/dns/queries'

/**
 * The DNS card for one client, identified the way the query log identifies it:
 * by source address.
 *
 * That is the whole of the join, and it is why this card can sit on a page for
 * something that is not in the device list. `devices` knows a device by its MAC
 * and the resolver knows a client by the address its packets came from; a
 * device that dialed in over WireGuard has only the second (the tunnel address
 * baked into its configuration), so it matches here and nowhere else.
 *
 * Off by default, and the switch is the operator asking: the log holds every
 * name their devices looked up. Three empty states rather than one, because
 * "the log is off", "the resolver is down" and "nothing matched" are three
 * different things to go and do.
 */
export function ClientQueries({ addresses }: { addresses: string[] }) {
  useLocale()
  const [inspection, setInspection] = useState(false)
  const [searchOpen, setSearchOpen] = useState(false)
  const [filter, setFilter] = useState('')
  const searchInput = useRef<HTMLInputElement>(null)
  const searchButton = useRef<HTMLButtonElement>(null)
  const queries = useDnsQueries(inspection)
  const config = useDnsConfig()
  const watched = new Set(addresses.map((address) => address.toLowerCase()))
  const matching = queries.data?.queries.filter((q) => watched.has(q.client.toLowerCase())) ?? []
  const shown = matching.filter((q) => !filter.trim() || [q.name, q.type, q.rcode, q.policy ?? '', ...(q.answers ?? [])].some((value) => value.toLowerCase().includes(filter.trim().toLowerCase())))

  return <Card className="gap-0 py-0 [overflow-anchor:none]">
    <CardHeader className="mx-4 flex h-18 flex-row items-center justify-between gap-3 rounded-none px-0 sm:mx-6">
      <CardTitle>DNS</CardTitle>
      {/* The label and switch come last, so their trailing edge is pinned to
          the card's right edge. Turning inspection on, or opening the filter,
          adds controls to their left and the field grows away from the search
          button, so the Inspection control is always where the operator left
          it. */}
      <div className="flex items-center gap-3">
        {inspection && <div id="client-dns-search" className={`overflow-hidden transition-[width,opacity] duration-200 motion-reduce:transition-none ${searchOpen ? 'w-28 opacity-100 sm:w-56' : 'w-0 opacity-0'}`} inert={!searchOpen}>
          <div className="overflow-hidden">
            <Input ref={searchInput} id="client-dns-filter" aria-label={t('Filter DNS queries')} className="w-full" value={filter} onChange={(e) => setFilter(e.target.value)} placeholder={t('Search queries')} onKeyDown={(e) => { if (e.key === 'Escape') { setSearchOpen(false); setFilter(''); searchButton.current?.focus() } }} />
          </div>
        </div>}
        {inspection && <Button ref={searchButton} className="shrink-0" variant="quiet" size="icon" aria-label={searchOpen ? 'Close DNS search' : 'Search DNS queries'} aria-expanded={searchOpen} aria-controls="client-dns-search" onClick={() => { setSearchOpen(!searchOpen); setFilter(''); if (!searchOpen) requestAnimationFrame(() => searchInput.current?.focus()) }}>{searchOpen ? <X className="size-5" /> : <Search className="size-5" />}</Button>}
        <label htmlFor="client-dns-inspection" className="sr-only text-sm sm:not-sr-only">{t('Inspection')}</label>
        <Switch id="client-dns-inspection" checked={inspection} onCheckedChange={(checked) => { setInspection(checked); setSearchOpen(false); setFilter('') }} />
      </div>
    </CardHeader>
    {inspection && <CardContent className="mx-4 space-y-4 border-t px-0 py-4 sm:mx-6 sm:py-6">
      {queries.isError ? (
        <p className="text-sm text-muted-foreground">{t('The DNS query log is unavailable. The resolver may be stopped.')}</p>
      ) : queries.isPending ? (
        <p className="text-sm text-muted-foreground">{t('Loading recent queries…')}</p>
      ) : config.isError ? (
        <p role="alert" className="text-sm text-muted-foreground">{t('The DNS query log configuration is unavailable.')}</p>
      ) : config.isPending ? (
        <p className="text-sm text-muted-foreground">{t('Loading DNS configuration…')}</p>
      ) : !config.data?.query_log.enabled ? (
        <p className="text-sm text-muted-foreground">{t('The DNS query log is off. Queries are answered but not kept.')}</p>
      ) : shown.length === 0 ? (
        <p className="text-sm text-muted-foreground">{matching.length ? t('Nothing matches that filter.') : 'No matching queries in the recent sample.'}</p>
      ) : (
        <div className="max-h-80 overflow-y-auto">
          {/* One line per query. A name or a CDN answer list wraps into four
              lines and the table scrolls sideways; each column owns its width
              instead and cuts what does not fit. */}
          <Table aria-label={t('Recent DNS queries: time, record, type, answer')} className="min-w-[36rem] table-fixed">
            <colgroup>
              <col className="w-24" />
              <col className="w-[36%]" />
              <col className="w-[4.5rem]" />
              <col />
            </colgroup>
            <TableBody>{shown.map((q, i) => <TableRow key={`${q.at}-${q.client}-${q.name}-${i}`} className="h-10">
              <TableCell><time dateTime={q.at} className="font-mono text-xs text-muted-foreground">{new Date(q.at).toLocaleTimeString()}</time></TableCell>
              <TableCell><span className="block truncate" title={q.name}>{q.name}</span></TableCell>
              <TableCell className="text-muted-foreground"><span className="block truncate">{q.type}</span></TableCell>
              <TableCell>{q.blocked ? <Badge variant="secondary" className="max-w-full"><span className="min-w-0 truncate">{t('Blocked')}{q.policy ? ` · ${q.policy}` : ''}</span></Badge> : q.rcode !== 'NOERROR' ? <Badge variant="warning">{q.rcode}</Badge> : q.answers?.length ? <DnsAnswers answers={q.answers} /> : <span className="text-xs text-muted-foreground">{t('No answer')}</span>}</TableCell>
            </TableRow>)}</TableBody>
          </Table>
        </div>
      )}
    </CardContent>}
  </Card>
}

/**
 * The answers on one line.
 *
 * A name behind a CDN comes back with a dozen addresses. Left to wrap, one row
 * grew to four lines and pushed the table sideways, so the list keeps the
 * addresses that fit and counts the rest. The addresses the count replaces are
 * still in the accessible text; the ellipsis is only how it looks.
 *
 * The hidden copy is what makes the count honest: it is laid out off-screen in
 * the same font so each address can be measured before anything is dropped.
 */
function DnsAnswers({ answers }: { answers: string[] }) {
  useLocale()
  const cell = useRef<HTMLSpanElement>(null)
  const mirror = useRef<HTMLSpanElement>(null)
  const [fit, setFit] = useState(answers.length)

  useLayoutEffect(() => {
    const element = cell.current
    const measure = mirror.current
    if (!element || !measure) return
    const update = () => {
      // Each shown address carries its own ", " lead, so the list adds up
      // without a gap; only the count needs the gap before it (ml-1).
      const available = element.clientWidth
      const items = [...measure.querySelectorAll<HTMLElement>('[data-answer]')]
      const reserved = (measure.querySelector<HTMLElement>('[data-more]')?.offsetWidth ?? 0) + 4
      let used = 0
      let shown = 0
      for (const [index, item] of items.entries()) {
        const next = used + item.offsetWidth + (index < items.length - 1 ? reserved : 0)
        if (next > available) break
        used += item.offsetWidth
        shown = index + 1
      }
      setFit((previous) => previous === shown ? previous : shown)
    }
    update()
    const observer = new ResizeObserver(update)
    observer.observe(element)
    // Text metrics change when the webfont lands, without the cell resizing.
    observer.observe(measure)
    return () => observer.disconnect()
  }, [answers])

  // Nothing fitting still shows the first address, cut with an ellipsis.
  const visible = answers.slice(0, fit || 1)
  const hidden = answers.length - visible.length
  const counted = fit > 0 && hidden > 0

  return <span ref={cell} className="relative flex min-w-0 items-center overflow-hidden">
    <span ref={mirror} aria-hidden="true" className="pointer-events-none invisible absolute top-0 left-0 w-max font-mono text-xs whitespace-nowrap">
      {answers.map((answer, i) => <span key={i} data-answer>{i ? `, ${answer}` : answer}</span>)}
      <span data-more className="ml-1">+{answers.length} more</span>
    </span>
    {visible.map((answer, i) => (
      // Alone in the cell, the first address is the one that gets cut.
      <span key={`${i}-${answer}`} className={`font-mono text-xs text-muted-foreground ${fit === 0 && i === 0 ? 'min-w-0 truncate' : 'shrink-0'}`}>{i ? `, ${answer}` : answer}</span>
    ))}
    {counted && <span className="ml-1 shrink-0 text-xs text-muted-foreground" title={answers.join(', ')}>+{hidden} more<span className="sr-only">: {answers.slice(visible.length).join(', ')}</span></span>}
  </span>
}
