import { Monitor, Moon, Sun } from 'lucide-react'
import { useTheme } from 'next-themes'

import { Button } from '@/components/ui/button'

export function ThemeToggle() {
  const { theme, setTheme } = useTheme()
  const current = theme === 'light' || theme === 'dark' ? theme : 'system'
  const next = { system: 'light', light: 'dark', dark: 'system' } as const
  const Icon = current === 'system' ? Monitor : current === 'light' ? Sun : Moon

  return (
    <Button
      variant="ghost"
      size="icon"
      className="size-11 text-muted-foreground transition-colors duration-150 hover:bg-transparent hover:text-foreground active:bg-transparent dark:hover:bg-transparent lg:items-end"
      aria-label={`Theme: ${current === 'system' ? 'auto' : current}. Switch to ${next[current] === 'system' ? 'auto' : next[current]}`}
      title={`Theme: ${current === 'system' ? 'auto' : current}. Switch to ${next[current] === 'system' ? 'auto' : next[current]}`}
      onClick={() => setTheme(next[current])}
    >
      <Icon className="size-3.5" aria-hidden />
    </Button>
  )
}
