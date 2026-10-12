import { SubPage } from '@/components/layout/sub-page'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { ListEmpty } from '@/components/ui/list'
import { Skeleton } from '@/components/ui/skeleton'
import { useGatewayStatus } from '@/features/gateway/queries'

export function GatewayUnmanagedPage() {
  const status = useGatewayStatus()
  const foreign = status.data?.foreign ?? []

  return (
    <SubPage section="/gateway" slug="unmanaged">
      {status.isError ? (
        <Alert variant="destructive">
          <AlertTitle>Could not read routing rules</AlertTitle>
          <AlertDescription>{status.error.message}<Button variant="outline" size="sm" onClick={() => void status.refetch()}>Try again</Button></AlertDescription>
        </Alert>
      ) : status.isPending ? (
        <Skeleton className="h-24 w-full" />
      ) : !status.data?.known ? (
        <Alert><AlertTitle>Routing state unknown</AlertTitle><AlertDescription>External rules could not be verified. Check the router's permissions.</AlertDescription></Alert>
      ) : foreign.length === 0 ? (
        <ListEmpty>No external routing rules were reported.</ListEmpty>
      ) : (
        <pre className="overflow-auto rounded-xl border bg-card p-4 font-mono text-sm leading-relaxed">
          {foreign.map((f) => `priority ${f.priority}  ${f.family}  table ${f.table}  ${f.selector}`).join('\n')}
        </pre>
      )}
    </SubPage>
  )
}
