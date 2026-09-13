import { SubPage } from '@/components/layout/sub-page'
import { ListEmpty } from '@/components/ui/list'
import { useGatewayStatus } from '@/features/gateway/queries'

export function GatewayUnmanagedPage() {
  const status = useGatewayStatus()
  const foreign = status.data?.foreign ?? []

  return (
    <SubPage section="/gateway" slug="unmanaged">
      {foreign.length === 0 ? (
        <ListEmpty>
          Nothing else is managing routing on this box — every rule here is one olr put in
          place.
        </ListEmpty>
      ) : (
        <pre className="overflow-auto rounded-xl border bg-card p-4 font-mono text-xs leading-relaxed">
          {foreign
            .map((f) => `priority ${f.priority}  ${f.family}  table ${f.table}  ${f.selector}`)
            .join('\n')}
        </pre>
      )}
    </SubPage>
  )
}
