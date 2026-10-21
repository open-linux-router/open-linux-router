import { ArrowUpRight } from 'lucide-react'

import { Heading } from '@/components/ui/heading'
import { Table, TableBody, TableCell, TableRow } from '@/components/ui/table'
import { t, useLocale } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import { CREDITS, type Credit } from './credits-data'

// Values that describe how a project reaches the box rather than a release it
// was built against. They stay in the Version column because that is where a
// reader looks, but they read quieter than a number.
const DESCRIPTOR_VERSIONS = new Set(['distro', 'in-tree', 'unpinned', 'API', 'registry'])

function CreditMark({ name, logo }: { name: string; logo?: string }) {
  const initial = name.match(/[a-z0-9]/i)?.[0].toUpperCase() ?? '?'
  if (!logo) {
    return (
      <span aria-hidden className="flex size-9 shrink-0 items-center justify-center rounded-[0.625rem] border border-border/60 bg-muted/70 text-[0.6875rem] font-semibold text-muted-foreground">
        {initial}
      </span>
    )
  }
  return (
    // Brand marks keep their own colours on a white backing in both themes,
    // the same rule Access uses for project artwork.
    <span className="flex size-9 shrink-0 items-center justify-center rounded-[0.625rem] border border-border/70 bg-white p-1 shadow-[0_1px_1px_rgb(23_27_34/0.04)]">
      <img src={logo} alt="" loading="lazy" decoding="async" className="max-h-full max-w-full object-contain" />
    </span>
  )
}

function Version({ value, className }: { value: string; className?: string }) {
  return (
    <span className={cn('font-mono text-xs tabular-nums', DESCRIPTOR_VERSIONS.has(value) ? 'text-muted-foreground/80' : 'text-foreground/75', className)}>
      {value}
    </span>
  )
}

// The whole first cell is the destination: the mark and the name are one link,
// so there is no separate website column to repeat the same address.
function CreditLink({ credit, className }: { credit: Credit; className?: string }) {
  return (
    <a
      href={credit.url}
      target="_blank"
      rel="noreferrer"
      title={credit.url}
      aria-label={t('{name} website, opens in a new tab', { name: credit.name })}
      className={cn('group/link flex min-w-0 items-center gap-3 rounded-md underline-offset-4 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring', className)}
    >
      <CreditMark name={credit.name} logo={credit.logo} />
      <span className="truncate font-medium group-hover/link:underline">{credit.name}</span>
      <ArrowUpRight aria-hidden className="size-3.5 shrink-0 opacity-40 transition-[transform,opacity] duration-150 group-hover/link:-translate-y-0.5 group-hover/link:translate-x-0.5 group-hover/link:opacity-100" />
    </a>
  )
}

function CreditRow({ credit }: { credit: Credit }) {
  return (
    <TableRow>
      <TableCell className="w-[38%] py-2.5 pl-4 sm:pl-5 lg:w-[30%] xl:w-[26%]"><CreditLink credit={credit} /></TableCell>
      <TableCell className="w-[7rem] py-2.5"><Version value={credit.version} /></TableCell>
      <TableCell className="whitespace-normal py-2.5 pr-4 leading-5 text-muted-foreground sm:pr-5">{t(credit.use)}</TableCell>
    </TableRow>
  )
}

// Below the table breakpoint the rows become stacked records with the same
// three fields in the same reading order; the list is still one flat table of
// projects, just not a multi-column one.
function CreditCard({ credit }: { credit: Credit }) {
  return (
    <li className="px-4 py-3">
      <div className="flex items-center justify-between gap-3">
        <CreditLink credit={credit} className="min-w-0 flex-1" />
        <Version value={credit.version} className="shrink-0" />
      </div>
      <p className="mt-1.5 pl-12 text-sm leading-5 text-muted-foreground">{t(credit.use)}</p>
    </li>
  )
}

export function CreditsPage() {
  useLocale()
  return (
    <div className="space-y-6 pb-12">
      <Heading as="h1" size="lg">{t('Built on open work')}</Heading>

      <div className="panel-surface hidden overflow-hidden md:block">
        <Table aria-label={t('Projects and versions')} className="min-w-[36rem]">
          <TableBody>
            {CREDITS.map((credit) => <CreditRow key={credit.name} credit={credit} />)}
          </TableBody>
        </Table>
      </div>

      <ul className="panel-surface divide-y divide-border/60 overflow-hidden md:hidden">
        {CREDITS.map((credit) => <CreditCard key={credit.name} credit={credit} />)}
      </ul>

      <div className="space-y-1.5 text-xs leading-5 text-muted-foreground">
        <p>{t('distro = your distribution’s package · in-tree = part of the Linux kernel · unpinned = installed separately.')}</p>
        <p>
          {t('Full dependency and license lists')}:{' '}
          <a className="underline underline-offset-4 hover:text-foreground" href="https://github.com/open-linux-router/open-linux-router/blob/main/go.mod" target="_blank" rel="noreferrer">go.mod</a>
          {' · '}
          <a className="underline underline-offset-4 hover:text-foreground" href="https://github.com/open-linux-router/open-linux-router/blob/main/web/package.json" target="_blank" rel="noreferrer">web/package.json</a>
        </p>
      </div>
    </div>
  )
}
