import { SubPage } from '@/components/layout/sub-page'
import { ListEmpty } from '@/components/ui/list'
import { useForwardsStatus } from '@/features/nat/queries'

export function FilteringPage() {
  const status = useForwardsStatus()
  const foreign = status.data?.foreign ?? []

  return (
    <SubPage section="/gateway" slug="filtering">
      {foreign.length === 0 ? (
        <ListEmpty>Nothing else on this box is filtering forwarded traffic.</ListEmpty>
      ) : (
        <div className="space-y-3">
          <pre className="overflow-auto rounded-xl border bg-card p-4 font-mono text-xs leading-relaxed">
            {foreign
              .map((f) => `${f.family} ${f.table}  chain ${f.chain}  policy ${f.policy}`)
              .join('\n')}
          </pre>
          <p className="text-[0.8rem] text-muted-foreground">
            It may well be accepting this traffic already — olr cannot tell, because in nftables a
            drop is final and nothing records what did not happen. The count beside each forward is
            what settles it.
          </p>
        </div>
      )}
    </SubPage>
  )
}
