import { Search, X } from 'lucide-react'
import { useRef, useState } from 'react'

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
      <div className="flex items-center gap-3">
        <label htmlFor="client-dns-inspection" className="sr-only text-sm sm:not-sr-only">Inspection</label>
        <Switch id="client-dns-inspection" checked={inspection} onCheckedChange={(checked) => { setInspection(checked); setSearchOpen(false); setFilter('') }} />
        {inspection && <Button ref={searchButton} className="order-last shrink-0" variant="ghost" size="icon" aria-label={searchOpen ? 'Close DNS search' : 'Search DNS queries'} aria-expanded={searchOpen} aria-controls="client-dns-search" onClick={() => { setSearchOpen(!searchOpen); setFilter(''); if (!searchOpen) requestAnimationFrame(() => searchInput.current?.focus()) }}>{searchOpen ? <X className="size-5" /> : <Search className="size-5" />}</Button>}
      {inspection && <div id="client-dns-search" className={`overflow-hidden transition-[width,opacity] duration-200 motion-reduce:transition-none ${searchOpen ? 'w-28 opacity-100 sm:w-56' : 'w-0 opacity-0'}`} inert={!searchOpen}>
        <div className="overflow-hidden">
          <Input ref={searchInput} id="client-dns-filter" aria-label="Filter DNS queries" className="w-full" value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Search queries" onKeyDown={(e) => { if (e.key === 'Escape') { setSearchOpen(false); setFilter(''); searchButton.current?.focus() } }} />
        </div>
      </div>}
      </div>
    </CardHeader>
    {inspection && <CardContent className="mx-4 space-y-4 border-t px-0 py-4 sm:mx-6 sm:py-6">
      {queries.isError ? (
        <p className="text-sm text-muted-foreground">The DNS query log is unavailable. The resolver may be stopped.</p>
      ) : queries.isPending ? (
        <p className="text-sm text-muted-foreground">Loading recent queries…</p>
      ) : config.isError ? (
        <p role="alert" className="text-sm text-muted-foreground">The DNS query log configuration is unavailable.</p>
      ) : config.isPending ? (
        <p className="text-sm text-muted-foreground">Loading DNS configuration…</p>
      ) : !config.data?.query_log.enabled ? (
        <p className="text-sm text-muted-foreground">The DNS query log is off. Queries are answered but not kept.</p>
      ) : shown.length === 0 ? (
        <p className="text-sm text-muted-foreground">{matching.length ? 'Nothing matches that filter.' : 'No matching queries in the recent sample.'}</p>
      ) : (
        <div className="max-h-80 overflow-y-auto">
          <Table aria-label="Recent DNS queries: time, record, type, answer" className="[&_td:last-child]:text-right">
            <TableBody>{shown.map((q, i) => <TableRow key={`${q.at}-${q.client}-${q.name}-${i}`}>
              <TableCell><time dateTime={q.at} className="font-mono text-xs text-muted-foreground">{new Date(q.at).toLocaleTimeString()}</time></TableCell>
              <TableCell className="min-w-32 whitespace-normal break-all">{q.name}</TableCell>
              <TableCell className="text-muted-foreground">{q.type}</TableCell>
              <TableCell className="min-w-40 whitespace-normal break-all">{q.blocked ? <Badge variant="secondary">Blocked{q.policy ? ` · ${q.policy}` : ''}</Badge> : q.rcode !== 'NOERROR' ? <Badge variant="warning">{q.rcode}</Badge> : q.answers?.length ? <DnsAnswers answers={q.answers} /> : <span className="text-xs text-muted-foreground">No answer</span>}</TableCell>
            </TableRow>)}</TableBody>
          </Table>
        </div>
      )}
    </CardContent>}
  </Card>
}

function DnsAnswers({ answers }: { answers: string[] }) {
  return <span className="font-mono text-xs text-muted-foreground">{answers.join(', ')}</span>
}
