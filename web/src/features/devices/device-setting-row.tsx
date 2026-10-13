import type { ReactNode } from 'react'

export function DeviceSettingRow({ label, children }: { label: string; children: ReactNode }) {
  return <div className="grid min-h-11 sm:min-h-9 grid-cols-[6rem_minmax(0,1fr)] items-center gap-3 py-1 text-sm">
    <dt className="text-muted-foreground">{label}</dt>
    <dd className="min-w-0 text-right">{children}</dd>
  </div>
}
