import { Languages } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { setLocale, t, useLocale } from '@/lib/i18n'

export function LanguageToggle() {
  useLocale()
  const locale = useLocale()
  const label = locale === 'en' ? 'Switch language to Chinese' : '切换为 English'
  return (
    <Button variant="ghost" className="min-h-11 gap-2 text-muted-foreground" aria-label={label} title={label}
      onClick={() => setLocale(locale === 'en' ? 'zh-CN' : 'en')}>
      <Languages className="size-4" aria-hidden />
      <span>{t('Language')}</span>
      <span lang={locale === 'en' ? 'zh-CN' : 'en'}>{locale === 'en' ? '中文' : 'English'}</span>
    </Button>
  )
}
