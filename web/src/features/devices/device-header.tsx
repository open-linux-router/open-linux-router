import type { ReactNode } from 'react'

import { Heading } from '@/components/ui/heading'

/**
 * The top of a device page: one big picture, one editable name, and the traffic
 * matrix behind both.
 *
 * Shared by `/devices/:mac` and `/devices/wireguard/:name` so that a device at
 * home and one that dialed in open the same way. The two pages used to draw this
 * block twice, and the copies drifted: the peer's kept the pointer on its title
 * block and the device's did not, which cost the peer two rows of activity dots
 * their hover ([311204f]).
 *
 * What is *not* shared is as deliberate. A name is the only editable thing here,
 * and both pages store it in a different place — `devices` in olr.json, `remote`
 * on the peer entry — so each page keeps its own editor and passes it in. The
 * same goes for what follows the name:
 *
 *   - `keyName` is the machine key — the MAC, or the peer's configured name —
 *     shown only when the label hides it. The rule is one rule for both pages:
 *     a name you gave the device is not a name the network answers to.
 *   - `status` is where the two pages part company. A peer's connection state is
 *     a fact about the tunnel: `PersistentKeepalive = 25` in every configuration
 *     this router renders means a connected peer handshakes at least every two
 *     minutes, so "no handshake for three minutes" really is "not connected". A
 *     device at home has no equivalent — its lease outlives its presence by
 *     hours and the neighbour table keeps a STALE entry looking complete — so it
 *     gets no status at all, and reports when it was last heard instead. See
 *     docs/ui-content-guide.md.
 */
export function DeviceHeader({
  activity,
  picture,
  onPicture,
  pictureBusy,
  title,
  keyName,
  status,
}: {
  /** The traffic matrix, drawn behind everything else. */
  activity?: ReactNode
  /** The picture, already sized and decorated by the caller. */
  picture: ReactNode
  onPicture: () => void
  pictureBusy?: boolean
  /** The name, either a button or the field that replaces it. */
  title: ReactNode
  /** The key the name hides, when it hides one. */
  keyName?: ReactNode
  /** A connection state, for a page that has evidence for one. */
  status?: ReactNode
}) {
  return (
    <div className="relative flex min-h-20 items-center gap-4 border-b pb-4">
      {activity}
      <button
        type="button"
        onClick={onPicture}
        disabled={pictureBusy}
        aria-label="Change device icon"
        className="relative shrink-0 rounded-lg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
      >
        {picture}
      </button>
      {/* The activity dots are painted behind this block and a transparent box
          still takes the pointer, so it gives the pointer up — and the name
          takes it back, because the name is a button. Without that pairing the
          dots at the title's height answer no hover at all, which reads as
          "this one has traffic but no numbers". */}
      <div className="pointer-events-none relative min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <Heading as="h1" size="lg" className="pointer-events-auto min-w-0 max-w-full [overflow-wrap:anywhere]">
            {title}
          </Heading>
          {keyName}
          {status}
        </div>
      </div>
    </div>
  )
}
