import { useState } from 'react'

import type { Density } from '@/features/topology/layout'

const STORAGE_KEY = 'olr.overview.density'

/**
 * The operator's chosen node density, or null while they have not chosen —
 * in which case the map picks for itself (see NetworkMap's `density`).
 *
 * Remembered per browser, in localStorage, not stored on the router: it is how
 * this person likes to look at this screen, and olr.json is what the network
 * does.
 *
 * The automatic choice is not a device count. It used to be — detail up to
 * thirty — and that was wrong in both directions: four groups of five on a
 * laptop did not fit as detail, and one group of forty on a wide screen did.
 * What decides is whether the detail layout shows everything comfortably, and
 * only the layout knows that.
 */
export function useMapDensity(): [Density | null, (density: Density) => void] {
  const [chosen, setChosen] = useState<Density | null>(read)
  const choose = (density: Density) => {
    setChosen(density)
    try {
      localStorage.setItem(STORAGE_KEY, density)
    } catch {
      // Private mode, a full quota: the choice still holds for this visit.
    }
  }
  return [chosen, choose]
}

function read(): Density | null {
  try {
    const v = localStorage.getItem(STORAGE_KEY)
    return v === 'compact' || v === 'detail' ? v : null
  } catch {
    return null
  }
}
