import { useEffect, useState } from 'react'
import { useLocale } from '@/lib/i18n'
import { brandLogoHref } from '@/features/brand-logos/brand-logos'
import { getToken } from '@/lib/api'

export function SiteIcon({ name, url, icon: iconChoice, big = false }: { name: string; url: string; icon?: string; big?: boolean }) {
  useLocale()
  const [autoIcon, setAutoIcon] = useState<string>()
  const [broken, setBroken] = useState<string>()
  useEffect(() => {
    if (iconChoice?.startsWith('data:')) return
    const controller = new AbortController()
    let objectURL: string | undefined
    const token = getToken()
    fetch(`/api/gateway/latency/sites/${encodeURIComponent(name)}/icon?v=${encodeURIComponent(`${url}:${iconChoice ?? 'auto'}`)}`, {
      signal: controller.signal,
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    }).then(async (response) => {
      if (!response.ok) return
      objectURL = URL.createObjectURL(await response.blob())
      if (!controller.signal.aborted) setAutoIcon(objectURL)
      else URL.revokeObjectURL(objectURL)
    }).catch(() => {})
    return () => {
      controller.abort()
      if (objectURL) URL.revokeObjectURL(objectURL)
    }
  }, [name, url, iconChoice])
  const preview = iconChoice?.startsWith('data:') ? iconChoice : (brandLogoHref(iconChoice) ?? autoIcon)
  // A mark this build does not carry falls back to the site's initials, the
  // same answer as a site with no favicon at all.
  const box = big ? 'size-10' : 'size-5'
  return preview && preview !== broken ? <img src={preview} alt="" className={`${box} shrink-0 rounded-sm object-contain`} onError={() => setBroken(preview)} />
    : <span aria-hidden className={`flex ${box} shrink-0 items-center justify-center rounded-md bg-foreground/10 ${big ? 'text-lg' : 'text-[10px]'} font-bold uppercase`}>{name.slice(0, 2)}</span>
}

