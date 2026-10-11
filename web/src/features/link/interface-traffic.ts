import { useState } from 'react'

import type { InterfaceList, InterfaceRow } from '@/lib/api-types'

export interface RateSample { at: number; rx: number; tx: number }
interface Reading { row: InterfaceRow; at: number; history: RateSample[] }

/** Rates are bytes/second from NIC counters, not forwarded gateway traffic. */
export function nextInterfaceReadings(data: InterfaceList, before: Map<string, Reading>) {
  const at = Date.parse(data.as_of)
  const next = new Map<string, Reading>()
  if (!Number.isFinite(at)) return next
  for (const row of data.interfaces) {
    const prev = before.get(row.name)
    const age = prev ? at - prev.at : 0
    const counters = [row.rx_bytes, row.tx_bytes, prev?.row.rx_bytes, prev?.row.tx_bytes]
    const valid = counters.every(n => n !== undefined && Number.isSafeInteger(n) && n >= 0)
    const same = prev && row.index === prev.row.index && row.mac === prev.row.mac
    if (same && age === 0 && row.rx_bytes === prev.row.rx_bytes && row.tx_bytes === prev.row.tx_bytes
      && row.present === prev.row.present && row.up === prev.row.up && row.running === prev.row.running) {
      next.set(row.name, { ...prev, row })
      continue
    }
    const connected = row.present && row.up && row.running
    const continuous = same && connected && prev.row.present && prev.row.up && prev.row.running
      && age > 0 && age <= 15_000 && valid
      && row.rx_bytes! >= prev.row.rx_bytes! && row.tx_bytes! >= prev.row.tx_bytes!
    const history = continuous ? [
      ...prev.history,
      { at, rx: (row.rx_bytes! - prev.row.rx_bytes!) / (age / 1000), tx: (row.tx_bytes! - prev.row.tx_bytes!) / (age / 1000) },
    ].filter(sample => sample.at > at - 60_000) : []
    next.set(row.name, { row, at, history })
  }
  return next
}

/** Reset after counter rollover, unplug/recreation, or a long polling gap. */
export function useInterfaceTraffic(data: InterfaceList | undefined, failed: boolean) {
  const [state, setState] = useState<{ data?: InterfaceList; failed: boolean; readings: Map<string, Reading> }>({
    failed: false, readings: new Map(),
  })
  if (data !== state.data || failed !== state.failed) {
    setState({ data, failed, readings: data && !failed ? nextInterfaceReadings(data, state.failed ? new Map() : state.readings) : new Map() })
  }
  return failed ? new Map<string, Reading>() : state.readings
}
