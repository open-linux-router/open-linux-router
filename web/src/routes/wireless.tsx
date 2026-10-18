import { Wifi } from 'lucide-react'

import { Card, CardContent } from '@/components/ui/card'

export function WirelessPage() {
  return (
    <Card>
      <CardContent className="flex flex-col items-center gap-3 py-16 text-center">
        <Wifi className="size-8 text-muted-foreground" aria-hidden />
        <h2 className="text-lg font-semibold">Under construction</h2>
        <p className="text-sm text-muted-foreground">Wireless settings are coming soon.</p>
      </CardContent>
    </Card>
  )
}
