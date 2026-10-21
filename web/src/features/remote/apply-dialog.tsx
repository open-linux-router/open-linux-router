import { t, useLocale } from '@/lib/i18n'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Disclosure } from '@/components/ui/disclosure'
import { ImpactBadge, PlanDiff, PlanReasons, impactHint } from '@/features/remote/impact'
import type { useRemoteApply } from '@/features/remote/use-apply'
import type { ProxyPlan } from '@/lib/api-types'

/**
 * The one thing that interrupts an operator (design.md §6.3).
 *
 * Everything else applies on the click. This appears only when the plan came
 * back `disruptive`, which here means somebody loses their way into the network
 * from outside it — or a configuration already on their phone stops working.
 * Neither is undone by trying again.
 *
 * Two pages open it, because two pages can make one of those changes: the
 * tunnel's own, and a device's, which is where a configuration is re-issued.
 */
export function ConfirmDialog({ applier }: { applier: ReturnType<typeof useRemoteApply> }) {
  useLocale()
  const held = applier.confirming
  if (!held) return null

  return (
    <Dialog open onOpenChange={(open) => !open && applier.cancel()}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"> {t('Apply this change?')} <ImpactBadge impact={held.plan.impact} />
          </DialogTitle>
          <DialogDescription>{impactHint(held.plan.impact)}</DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          {'action' in held.plan ? (
            <ProxyConfirmation plan={held.plan} />
          ) : (
            <>
              <PlanReasons plan={held.plan} />
              <Disclosure summary={t('What would change')}>
                <PlanDiff plan={held.plan} />
              </Disclosure>
            </>
          )}
        </div>

        <DialogFooter>
          <Button variant="quiet" onClick={applier.cancel}> {t('Cancel')}</Button>
          <Button onClick={applier.confirm} disabled={applier.busy}> {t('Apply anyway')} </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function ProxyConfirmation({ plan }: { plan: ProxyPlan }) {
  useLocale()
  return (
    <>
      {plan.reasons?.map((reason) => <p key={reason} className="text-sm">{reason}</p>)}
      {plan.password_generated && (
        <p className="text-sm text-destructive"> {t('A new password will be generated. Copy the new link after applying this change.')} </p>
      )}
      <Disclosure summary={t('What would change')}>
        <ul className="space-y-2 text-sm">
          {plan.changes.map((change) => (
            <li key={change.path}>
              {change.kind} {change.path}
              {!change.secret && change.diff && <pre className="overflow-x-auto text-xs">{change.diff}</pre>}
            </li>
          ))}
          {plan.action !== 'none' && <li>{t('Service:')} {plan.action}</li>}
        </ul>
      </Disclosure>
    </>
  )
}
