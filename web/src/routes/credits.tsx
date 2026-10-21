import { ArrowUpRight } from 'lucide-react'
import { Link } from 'react-router'

import { Heading } from '@/components/ui/heading'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { t, useLocale } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import { CREDIT_GROUPS, type Credit } from './credits-data'

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

// The visible label is the host; the accessible name says which project it
// belongs to, so a column of github.com links stays distinguishable.
function WebsiteLink({ credit, className }: { credit: Credit; className?: string }) {
  return (
    <a
      href={credit.url}
      target="_blank"
      rel="noreferrer"
      aria-label={t('{name} website at {site}, opens in a new tab', { name: credit.name, site: credit.website })}
      className={cn('group/link inline-flex max-w-full items-center gap-1 text-muted-foreground underline-offset-4 transition-colors duration-150 hover:text-foreground hover:underline', className)}
    >
      <span className="truncate">{credit.website}</span>
      <ArrowUpRight aria-hidden className="size-3.5 shrink-0 opacity-50 transition-[transform,opacity] duration-150 group-hover/link:-translate-y-0.5 group-hover/link:translate-x-0.5 group-hover/link:opacity-100" />
    </a>
  )
}

function CreditRow({ credit }: { credit: Credit }) {
  return (
    <TableRow>
      <TableCell className="py-3 pl-4 sm:pl-5">
        <div className="flex min-w-0 items-center gap-3">
          <CreditMark name={credit.name} logo={credit.logo} />
          <span className="truncate font-medium">{credit.name}</span>
        </div>
      </TableCell>
      <TableCell className="py-3"><Version value={credit.version} /></TableCell>
      <TableCell className="py-3"><WebsiteLink credit={credit} className="text-sm" /></TableCell>
      <TableCell className="whitespace-normal py-3 pr-4 leading-5 text-muted-foreground sm:pr-5">{t(credit.use)}</TableCell>
    </TableRow>
  )
}

// Below the table breakpoint the rows become stacked records with the same
// four fields in the same reading order; a four-column table does not fit a
// phone without hiding the column that explains the dependency.
function CreditCard({ credit }: { credit: Credit }) {
  return (
    <li className="flex gap-3 px-4 py-3.5">
      <CreditMark name={credit.name} logo={credit.logo} />
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline justify-between gap-3">
          <span className="truncate font-medium">{credit.name}</span>
          <Version value={credit.version} className="shrink-0" />
        </div>
        <p className="mt-0.5 text-sm leading-5 text-muted-foreground">{t(credit.use)}</p>
        <WebsiteLink credit={credit} className="mt-1 text-xs" />
      </div>
    </li>
  )
}

function GroupMark({ title, count }: { title: string; count: number }) {
  return (
    <span className="flex items-center gap-2">
      <span aria-hidden className="h-3 w-0.5 rounded-full bg-primary/60" />
      <span className="text-xs font-medium text-foreground/80">{t(title)}</span>
      <span className="font-mono text-[0.6875rem] font-normal tabular-nums text-muted-foreground/70">{count}</span>
    </span>
  )
}

export function CreditsPage() {
  useLocale()
  return (
    <div className="space-y-8 pb-12">
      <div className="space-y-3">
        <Link to="/" className="text-sm text-muted-foreground underline-offset-4 hover:text-foreground hover:underline">{t('Back to Overview')}</Link>
        <Heading as="h1" size="lg">{t('Built on open work')}</Heading>
        <p className="max-w-2xl text-muted-foreground">{t('Thank you to the maintainers of the projects that make Open Linux Router possible.')}</p>
        <p className="max-w-3xl text-sm text-muted-foreground">{t('The projects that carry each feature, grouped by what they make possible. Versions are pinned where OLR fixes one; system packages follow your distribution.')}</p>
      </div>

      <div className="panel-surface hidden overflow-hidden md:block">
        <Table aria-label={t('Projects by function')} className="min-w-[44rem]">
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead scope="col" className="h-11 w-[32%] pl-4 text-xs font-medium text-muted-foreground sm:pl-5">{t('Dep')}</TableHead>
              <TableHead scope="col" className="h-11 w-[7rem] text-xs font-medium text-muted-foreground">{t('Version')}</TableHead>
              <TableHead scope="col" className="h-11 w-[10rem] text-xs font-medium text-muted-foreground">{t('Website')}</TableHead>
              <TableHead scope="col" className="h-11 pr-4 text-xs font-medium text-muted-foreground sm:pr-5">{t('Usage')}</TableHead>
            </TableRow>
          </TableHeader>
          {CREDIT_GROUPS.map((group) => (
            <TableBody key={group.title}>
              <TableRow className="bg-muted/40 hover:bg-muted/40">
                <TableHead scope="colgroup" colSpan={4} className="h-auto py-2.5 pl-4 sm:pl-5">
                  <GroupMark title={group.title} count={group.items.length} />
                </TableHead>
              </TableRow>
              {group.items.map((credit) => <CreditRow key={credit.name} credit={credit} />)}
            </TableBody>
          ))}
        </Table>
      </div>

      <div className="panel-surface overflow-hidden md:hidden">
        {CREDIT_GROUPS.map((group, index) => (
          <section key={group.title} aria-labelledby={`credits-group-${index}`}>
            <h2 id={`credits-group-${index}`} className="border-b border-border/60 bg-muted/40 px-4 py-2.5">
              <GroupMark title={group.title} count={group.items.length} />
            </h2>
            <ul className="divide-y divide-border/60">
              {group.items.map((credit) => <CreditCard key={credit.name} credit={credit} />)}
            </ul>
          </section>
        ))}
      </div>

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
