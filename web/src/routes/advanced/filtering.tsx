import { t, useLocale } from '@/lib/i18n'
import { SubPage } from '@/components/layout/sub-page'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { ListEmpty } from '@/components/ui/list'
import { Skeleton } from '@/components/ui/skeleton'
import { useForwardsStatus } from '@/features/nat/queries'

export function FilteringPage() {
  useLocale()
  const status = useForwardsStatus()
  const foreign = status.data?.foreign ?? []
  const drops = foreign.some((rule) => rule.policy.toLowerCase() === 'drop')

  return (
    <SubPage section="/advanced" slug="filtering">
      {status.isError ? (
        <Alert variant="destructive"><AlertTitle>{t('Could not read filtering rules')}</AlertTitle><AlertDescription>{status.error.message}<Button variant="outline" size="sm" onClick={() => void status.refetch()}>{t('Try again')}</Button></AlertDescription></Alert>
      ) : status.isPending ? (
        <Skeleton className="h-24 w-full" />
      ) : !status.data?.known ? (
        <Alert><AlertTitle>{t('Filtering state unknown')}</AlertTitle><AlertDescription>{t('External filtering could not be verified. Check the router\'s permissions.')}</AlertDescription></Alert>
      ) : foreign.length === 0 ? (
        <ListEmpty>{t('No external forwarding chains were reported.')}</ListEmpty>
      ) : (
        <div className="space-y-3">
          <pre className="overflow-auto rounded-xl border bg-card p-4 font-mono text-sm leading-relaxed">
            {foreign.map((f) => `${f.family} ${f.table}  chain ${f.chain}  policy ${f.policy}`).join('\n')}
          </pre>
          {drops && <Alert><AlertTitle>{t('External drop policy')}</AlertTitle><AlertDescription>These chains may block forwarded traffic. OLR cannot override a drop in another chain.</AlertDescription></Alert>}
          <p className="text-sm text-muted-foreground">Chain policies do not describe every rule. Forward counters show matching traffic, not proof that the destination is reachable.</p>
        </div>
      )}
    </SubPage>
  )
}
