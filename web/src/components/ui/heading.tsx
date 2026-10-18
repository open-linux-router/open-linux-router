import type { ComponentProps } from 'react'

import { cn } from '@/lib/utils'

export type HeadingProps = ComponentProps<'h2'> & {
  as?: 'h1' | 'h2' | 'h3' | 'h4'
  size?: 'sm' | 'md' | 'lg'
}

/** Heading level describes the document; size describes its visual emphasis. */
export function Heading({ as: Tag = 'h2', size = 'md', className, ...props }: HeadingProps) {
  return <Tag data-slot="heading" data-size={size} className={cn(
    'font-heading font-semibold',
    size === 'sm' ? 'text-sm leading-6' : size === 'lg' ? 'text-2xl leading-8 tracking-[-0.015em]' : 'text-base leading-6',
    className,
  )} {...props} />
}
