import { Languages } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { setLocale, useLocale } from '@/lib/i18n'
import { cn } from '@/lib/utils'

export function LanguageToggle({ className }: { className?: string }) {
  const locale = useLocale()
  const next = locale === 'en' ? 'zh-CN' : 'en'
  const label = locale === 'en' ? 'Switch language to Chinese' : '切换为 English'
  return (
    <Button variant="ghost" size="sm" className={cn('min-h-11 gap-1.5 px-1.5 text-xs font-normal text-muted-foreground transition-colors duration-150 hover:bg-transparent hover:text-foreground active:bg-transparent dark:hover:bg-transparent lg:items-end', className)}
      aria-label={label} title={label}
      onClick={() => setLocale(next)}>
      <Languages className="size-3.5" aria-hidden />
      <span lang={next}>{locale === 'en' ? '中文' : 'English'}</span>
    </Button>
  )
}
