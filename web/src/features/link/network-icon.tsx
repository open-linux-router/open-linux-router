import { useId } from 'react'

/** The approved filled symbols; local gradient IDs allow multiple instances. */
export function NetworkIcon({ kind, className }: {
  kind: 'interface' | 'dhcp' | 'dns'
  className?: string
}) {
  const id = useId()
  const ink = `url(#${id}-ink)`
  const blue = `url(#${id}-blue)`
  return (
    <svg viewBox="0 0 64 64" className={className} aria-hidden="true">
      <defs>
        <linearGradient id={`${id}-ink`} x1="0" y1="0" x2="1" y2="1">
          <stop stopColor="#707782" /><stop offset="1" stopColor="#3e4550" />
        </linearGradient>
        <linearGradient id={`${id}-blue`} x1="0" y1="0" x2="1" y2="1">
          <stop stopColor="#c3def7" /><stop offset="1" stopColor="#8797d6" />
        </linearGradient>
      </defs>
      {kind === 'interface' && <>
        <path d="M15 13h34a5 5 0 0 1 5 5v20a4 4 0 0 1-4 4h-7v7a2 2 0 0 1-2 2H23a2 2 0 0 1-2-2v-7h-7a4 4 0 0 1-4-4V18a5 5 0 0 1 5-5Z" fill={blue} />
        <g fill={ink}>
          {[20, 27, 34, 41].map(x => <rect key={x} x={x} y="18" width="3" height="11" rx="1.5" />)}
        </g>
      </>}
      {kind === 'dhcp' && <>
        <rect x="9" y="13" width="44" height="31" rx="6" fill={blue} />
        <g fill={ink}>
          {[17, 25, 33, 41].map(x => <rect key={x} x={x} y="25" width="4" height="5" rx="1.5" />)}
        </g>
        <path d="M34 41h12v-5l12 9-12 9v-5H34a3 3 0 0 1-3-3v-2a3 3 0 0 1 3-3Z" fill={ink} />
      </>}
      {kind === 'dns' && <>
        <path d="M38 36a4 4 0 0 1 6 0l12 12a4 4 0 0 1-6 6L38 42a4 4 0 0 1 0-6Z" fill={ink} />
        <circle cx="28" cy="28" r="18" fill={blue} />
        <circle cx="28" cy="28" r="11" fill="#e9f0fc" />
      </>}
    </svg>
  )
}
