import { t, useLocale } from '@/lib/i18n'
import type { Tone } from '@/lib/state'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Disclosure } from '@/components/ui/disclosure'
import { Label } from '@/components/ui/label'
import { StatusDot, StatusInline } from '@/components/ui/status'
import { Switch } from '@/components/ui/switch'

/**
 * Is this working, and can I turn it off.
 *
 * The first thing on a section's landing page that has a module switch, and —
 * with the live list under it — often the only thing an operator came for. It
 * was written three times before this (dhcp, dns and gateway each had their
 * own), then five: DNS and DHCP hand-copied the markup into their landing
 * cards. `density="compact"` is that copy, so the block has one owner and one
 * shape instead of one per section.
 *
 * Three states and not two: "we could not tell" is a real answer (design.md
 * §5.4), and flattening it into "stopped" puts a red dot on a working router.
 * The caller decides which of the five tones it is in — by naming a word from
 * `STATE`, not by passing a colour — and this only draws it.
 */
export function StatusStrip({
  tone,
  headline,
  detail,
  control,
  drifted,
  driftNote,
  driftAction,
  details,
  density = 'page',
  children,
}: {
  /** From `state()`, so the strip cannot disagree with the rest of the product. */
  tone: Tone
  headline: string
  detail: React.ReactNode
  /** The one switch this section turns on and off, if it has one. */
  control?: { id: string; label: string; checked: boolean; busy?: boolean; onChange: (on: boolean) => void }
  drifted?: boolean
  driftNote?: string
  /**
   * How to run the pending work, for a section that can re-apply stored intent.
   *
   * Without it the drift row is a diagnosis with no cure, and the advice it
   * used to carry — save any change and it goes away — is only true when the
   * drift is a file. A backend that is enabled but never started drifts with an
   * empty change list and one service action, and then there is no setting on
   * the page left to save: the switch an operator would reach for is already in
   * the position they want, and moving it off is the disruptive direction.
   */
  driftAction?: { label: string; busy?: boolean; onClick: () => void }
  /**
   * Facts about the daemon rather than about the network: unit states, the
   * clock, whether what is running matches. Kept behind a disclosure because
   * they answer "why" and the strip above answers "what", and only one of those
   * is worth a visit's attention by default.
   */
  details?: React.ReactNode
  /**
   * `page` draws its own card. `compact` draws no frame, for a section that
   * already has one and only wants the status rows inside it.
   */
  density?: 'page' | 'compact'
  /** Anything that must be seen without opening a disclosure — faults, mostly. */
  children?: React.ReactNode
}) {
  useLocale()
  const body = (
    <>
      <div className="flex items-center gap-3">
        <StatusDot tone={tone} size="md" />
        <div className="min-w-0 flex-1">
          <div className="font-medium">{headline}</div>
          <div className="text-sm text-muted-foreground">{detail}</div>
        </div>
        {control && (
          <>
            <Label htmlFor={control.id} className="sr-only">
              {control.label}
            </Label>
            <Switch
              id={control.id}
              checked={control.checked}
              busy={control.busy}
              onCheckedChange={control.onChange}
            />
          </>
        )}
      </div>

      {drifted && (
        <div className="flex flex-wrap items-center gap-2">
          <StatusInline tone="warn">{t('Not yet applied')}</StatusInline>
          <span className="text-sm text-muted-foreground">
            {driftNote ?? 'What is running is behind these settings.'}
          </span>
          {driftAction && (
            <Button
              size="sm"
              variant="outline"
              disabled={driftAction.busy}
              onClick={driftAction.onClick}
            >
              {driftAction.label}
            </Button>
          )}
        </div>
      )}

      {children}

      {details && (
        <Disclosure summary={t('Technical details')}>
          <dl className="space-y-1.5 pt-1 text-xs text-muted-foreground">{details}</dl>
        </Disclosure>
      )}
    </>
  )

  if (density === 'compact') return <div className="space-y-4">{body}</div>

  return (
    <Card>
      <CardContent className="space-y-4">{body}</CardContent>
    </Card>
  )
}

/** One term and value inside a status strip's technical details. */
export function StatusDetail({ term, children }: { term: string; children: React.ReactNode }) {
  useLocale()
  return (
    <div className="flex gap-2">
      <dt className="shrink-0">{term}:</dt>
      <dd className="min-w-0 break-words font-mono">{children}</dd>
    </div>
  )
}
