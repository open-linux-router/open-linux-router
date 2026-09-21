import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Disclosure } from '@/components/ui/disclosure'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

/**
 * Is this working, and can I turn it off.
 *
 * The first thing on every section's landing page, and — with the live list
 * under it — very often the only thing an operator came for. It was written
 * three times before this (dhcp, dns and gateway each had their own), which is
 * three places for a dot colour or a disclosure to go its own way.
 *
 * Three states and not two: "we could not tell" is a real answer (design.md
 * §5.4), and flattening it into "stopped" puts a red dot on a working router.
 * The caller decides which of the three it is in; this only draws it.
 */
export function StatusStrip({
  headline,
  detail,
  dot,
  control,
  drifted,
  driftNote,
  driftAction,
  details,
  children,
}: {
  headline: string
  detail: React.ReactNode
  /** A background class: bg-success, bg-destructive, bg-warning, bg-muted-foreground/40. */
  dot: string
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
  /** Anything that must be seen without opening a disclosure — faults, mostly. */
  children?: React.ReactNode
}) {
  return (
    <Card>
      <CardContent className="space-y-4">
        <div className="flex items-center gap-3">
          <span className={`size-2.5 shrink-0 rounded-full ${dot}`} aria-hidden />
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
                disabled={control.busy}
                onCheckedChange={control.onChange}
              />
            </>
          )}
        </div>

        {drifted && (
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant="warning">Not yet applied</Badge>
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
          <Disclosure summary="Technical details">
            <dl className="space-y-1.5 pt-1 text-xs text-muted-foreground">{details}</dl>
          </Disclosure>
        )}
      </CardContent>
    </Card>
  )
}

/** One term and value inside a status strip's technical details. */
export function StatusDetail({ term, children }: { term: string; children: React.ReactNode }) {
  return (
    <div className="flex gap-2">
      <dt className="shrink-0">{term}:</dt>
      <dd className="min-w-0 break-words font-mono">{children}</dd>
    </div>
  )
}
