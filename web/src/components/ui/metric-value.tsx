import { t, useLocale } from '@/lib/i18n'
import type { ComponentProps, ReactNode } from 'react'

import { cn } from '@/lib/utils'

/** sm: inline measurement; md: summary; lg: diagnostic result. */
export function MetricValue({ value, unit, size = 'sm', className, ...props }: Omit<ComponentProps<'span'>, 'children'> & {
  value: ReactNode
  unit?: string
  size?: 'sm' | 'md' | 'lg'
}) {
  useLocale()
  return <span data-slot="metric-value" data-size={size} className={cn(
    'inline-flex max-w-full items-baseline gap-1 whitespace-nowrap font-sans font-semibold tabular-nums',
    size === 'lg' ? 'text-3xl leading-9 tracking-[-0.015em]' : size === 'md' ? 'text-xl leading-7' : 'text-sm leading-5',
    className,
  )} {...props}>
    {value == null ? <span aria-label={t('Unavailable')}>—</span> : value}
    {value != null && unit && <span className={cn('text-sm leading-5 font-normal', size !== 'sm' && 'text-muted-foreground tracking-normal')}>{unit}</span>}
  </span>
}
