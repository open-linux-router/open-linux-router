import { AlertTriangle, ChevronRight } from 'lucide-react'
import { Link } from 'react-router'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'

/**
 * Said when a module's saved settings no longer validate.
 *
 * The state a module reaches when something it depends on is taken away
 * underneath it: a network removed with an address range still on it, an
 * address the resolver was told to listen on that this box no longer has. olr
 * will not render what does not validate, so the backend keeps running the
 * last settings that did — on a box where that can mean still handing out a
 * deleted network's addresses on a different one.
 *
 * It is not drift (nothing changed behind olr's back), and it used to be shown
 * as nothing at all: a green headline, with the reason one click down in
 * "Technical details" as the answer to "matches the running server". The one
 * thing an operator needs from this state is to be told it exists.
 */
export function StuckSettings({
  error,
  fix,
}: {
  /** The status's drift_error, passed only while the module is switched on. */
  error?: string
  /** The page where what is wrong can be changed, when there is one. */
  fix?: { to: string; label: string }
}) {
  if (!error) return null
  return (
    <Alert>
      <AlertTriangle className="text-warning" />
      <AlertTitle>These settings cannot be put in force</AlertTitle>
      <AlertDescription className="space-y-2">
        <p>
          What is saved no longer checks out, so the running service is still on the last
          settings that did. Nothing you change here takes effect until this is fixed.
        </p>
        <p className="font-mono text-xs whitespace-pre-line">{error}</p>
        {fix && (
          <Link
            to={fix.to}
            className="inline-flex items-center gap-1 text-sm font-medium underline underline-offset-4"
          >
            {fix.label}
            <ChevronRight className="size-3.5" aria-hidden />
          </Link>
        )}
      </AlertDescription>
    </Alert>
  )
}
