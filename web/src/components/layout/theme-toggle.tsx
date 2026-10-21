import { Monitor, Moon, Sun } from 'lucide-react'
import { useTheme } from 'next-themes'

import { Button } from '@/components/ui/button'
import { t, useLocale } from '@/lib/i18n'
import { cn } from '@/lib/utils'

export function ThemeToggle({ className }: { className?: string }) {
  useLocale()
  const { theme, setTheme } = useTheme()
  const current = theme === 'light' || theme === 'dark' ? theme : 'system'
  const next = { system: 'light', light: 'dark', dark: 'system' } as const
  const Icon = current === 'system' ? Monitor : current === 'light' ? Sun : Moon
  const names = { system: t('Auto'), light: t('Light'), dark: t('Dark') }
  const label = t('Theme: {current}. Switch to {next}', { current: names[current], next: names[next[current]] })

  return (
    <Button
      variant="quiet"
      size="icon"
      className={cn('h-11 w-auto min-w-0 px-2 text-muted-foreground transition-colors duration-150 hover:bg-transparent hover:text-foreground active:bg-transparent dark:hover:bg-transparent lg:items-end', className)}
      aria-label={label}
      title={label}
      onClick={() => setTheme(next[current])}
    >
      <Icon className="size-3.5" aria-hidden />
    </Button>
  )
}
