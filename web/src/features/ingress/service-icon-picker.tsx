import { t, useLocale } from '@/lib/i18n'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { brandLogoHref, brandLogoSrc, useBrandLogoSearch } from '@/features/brand-logos/brand-logos'

export function ServiceIconPicker({ name, icon, onChange }: { name: string; icon?: string; onChange: (icon?: string) => void }) {
  useLocale()
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const [error, setError] = useState('')
  const { slugs: matches } = useBrandLogoSearch(search)

  async function upload(file?: File) {
    if (!file) return
    setError('')
    if (!['image/png', 'image/jpeg', 'image/webp'].includes(file.type) || file.size > 2 * 1024 * 1024) {
      setError('Choose a PNG, JPEG or WebP under 2 MiB')
      return
    }
    try {
      const image = await createImageBitmap(file)
      const canvas = document.createElement('canvas')
      const scale = Math.min(1, 128 / Math.max(image.width, image.height))
      canvas.width = Math.max(1, Math.round(image.width * scale))
      canvas.height = Math.max(1, Math.round(image.height * scale))
      canvas.getContext('2d')!.drawImage(image, 0, 0, canvas.width, canvas.height)
      image.close()
      const data = canvas.toDataURL('image/png')
      if (data.length > 130000) throw new Error('Image is too large after resizing')
      onChange(data)
      setOpen(false)
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Could not read image') }
  }

  const preview = icon?.startsWith('data:') ? icon : brandLogoHref(icon)
  return <>
    <div className="space-y-1.5">
      <span className="text-sm font-medium">{t('Site logo')}</span>
      <div className="flex items-center gap-3">
        <span className="flex size-9 items-center justify-center rounded-lg bg-muted">
          {preview ? <img src={preview} alt="" className="size-6 object-contain" /> : <span className="text-xs font-semibold uppercase">{name.slice(0, 2)}</span>}
        </span>
        <Button type="button" variant="secondary" onClick={() => { setSearch(''); setError(''); setOpen(true) }}>{t('Choose logo')}</Button>
        <span className="text-xs text-muted-foreground">{icon ? 'Custom' : 'Auto favicon'}</span>
      </div>
    </div>
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader><DialogTitle>{t('Logo for')} {name || 'new site'}</DialogTitle><DialogDescription>{t('Upload an image or search theSVG library. Auto uses the site favicon.')}</DialogDescription></DialogHeader>
        <div className="flex gap-2">
          <Button variant="secondary" onClick={() => { onChange(undefined); setOpen(false) }}>{t('Auto')}</Button>
          <label className="inline-flex h-8 cursor-pointer items-center rounded-lg border px-3 text-sm">{t('Upload image')}<input className="sr-only" type="file" accept="image/png,image/jpeg,image/webp" onChange={(event) => upload(event.target.files?.[0])} />
          </label>
        </div>
        <Input aria-label={t('Search theSVG icons')} placeholder={t('Search theSVG (e.g. grafana)')} value={search} onChange={(event) => setSearch(event.target.value)} />
        <div className="grid max-h-52 grid-cols-4 gap-2 overflow-y-auto">{matches.map((slug) => <button key={slug} type="button" className="flex min-w-0 flex-col items-center gap-1 rounded-lg border p-2 text-xs hover:bg-muted" title={slug}
          onClick={() => { onChange(`thesvg:${slug}`); setOpen(false) }}>
          <img src={brandLogoSrc(slug)} alt="" className="size-7" loading="lazy" />
          <span className="w-full truncate">{slug}</span></button>)}</div>
        {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
        <p className="text-xs text-muted-foreground">{t('Icons by theSVG (MIT). Only the selected ID is saved.')}</p>
      </DialogContent>
    </Dialog>
  </>
}
