import { Button } from '@/components/ui/button'
import { setLocale, useLocale } from '@/lib/i18n'
import { cn } from '@/lib/utils'

/**
 * The destination language in its own writing, as footer text: no icon and no
 * separate "Language" label, because the word is the language. The 8px side
 * padding makes the target reach into the gap between items; the footer cancels
 * it so the visible gap stays 16px.
 */
export function LanguageToggle({ className }: { className?: string }) {
  const locale = useLocale()
  const next = locale === 'en' ? 'zh-CN' : 'en'
  const label = locale === 'en' ? 'Switch language to Chinese' : '切换为 English'
  return (
    <Button variant="ghost" size="sm" className={cn('min-h-11 px-2 text-xs font-normal text-muted-foreground transition-colors duration-150 hover:bg-transparent hover:text-foreground active:bg-transparent dark:hover:bg-transparent lg:items-end', className)}
      aria-label={label} title={label}
      onClick={() => setLocale(next)}>
      <span lang={next}>{locale === 'en' ? '中文' : 'English'}</span>
    </Button>
  )
}
