import { AlertTriangle, ArrowUpRight } from 'lucide-react'
import { useEffect } from 'react'
import { Link, useLocation } from 'react-router'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { InterfacesContent, ServedNetworksContent } from '@/routes/gateway/networks'
import { DhcpContent } from '@/routes/dhcp/index'
import { DnsContent } from '@/routes/dns/index'
import { useGatewayConfig, useGatewayStatus } from '@/features/gateway/queries'

/**
 * The gateway, on the page you land on.
 *
 * The everyday controls stay here; routing and DNS diagnostics live behind
 * their detail links.
 */
export function GatewayPage() {
  const config = useGatewayConfig()
  const status = useGatewayStatus()
  const { hash } = useLocation()

  useEffect(() => {
    if (config.data && hash) document.getElementById(hash.slice(1))?.scrollIntoView()
  }, [config.data, hash])

  if (config.isPending) return <p className="text-sm text-muted-foreground">Loading Gateway…</p>
  if (config.isError) return <Alert variant="destructive"><AlertTriangle /><AlertTitle>Could not load Gateway</AlertTitle><AlertDescription>{config.error.message}</AlertDescription></Alert>

  return (
    <div className="space-y-6">
      {status.data && !status.data.known && (
        <Alert><AlertTriangle /><AlertTitle>Routing state unknown</AlertTitle><AlertDescription>Check the router's permissions before relying on saved settings.</AlertDescription></Alert>
      )}
      {status.data?.drifted && (
        <Alert>
          <AlertTriangle />
          <AlertTitle>Saved routing differs from what is running</AlertTitle>
          <AlertDescription><Link className="underline underline-offset-4" to="/gateway/interfaces">Review and restore routing settings</Link></AlertDescription>
        </Alert>
      )}

      <section id="interfaces" className="space-y-6 scroll-mt-20" aria-labelledby="interfaces-heading">
        <h2 id="interfaces-heading" className="text-xl font-semibold tracking-tight">Interfaces</h2>
        <InterfacesContent />

        <Link className="inline-flex items-center gap-1 text-sm font-medium text-muted-foreground hover:text-foreground" to="/gateway/interfaces">
          Advanced interfaces and routing <ArrowUpRight className="size-4" aria-hidden />
        </Link>
      </section>

      <section id="dhcp" className="space-y-6 border-t pt-6 scroll-mt-20" aria-labelledby="dhcp-heading">
        <h2 id="dhcp-heading" className="text-xl font-semibold tracking-tight">DHCP</h2>
        <ServedNetworksContent />
        <DhcpContent />
      </section>

      <section id="dns" className="space-y-4 border-t pt-6 scroll-mt-20" aria-labelledby="dns-heading">
        <h2 id="dns-heading" className="text-xl font-semibold tracking-tight">DNS</h2>
        <DnsContent />
      </section>
    </div>
  )
}
