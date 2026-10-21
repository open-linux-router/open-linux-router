import { t, useLocale } from '@/lib/i18n'
import { Heading } from '@/components/ui/heading'
import { Wifi } from 'lucide-react'

import { Card, CardContent } from '@/components/ui/card'

export function WirelessPage() {
  useLocale()
  return (
    <Card>
      <CardContent className="flex flex-col items-center gap-3 py-16 text-center">
        <Wifi className="size-8 text-muted-foreground" aria-hidden />
        <Heading>{t('Under construction')}</Heading>
        <p className="text-sm text-muted-foreground">{t('Wireless settings are coming soon.')}</p>
      </CardContent>
    </Card>
  )
}
