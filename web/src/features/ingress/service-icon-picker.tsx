import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import thesvgSlugs from '@/features/gateway/thesvg-slugs.json'

export function ServiceIconPicker({ name, icon, onChange }: { name: string; icon?: string; onChange: (icon?: string) => void }) {
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const [error, setError] = useState('')
  const matches = search.trim() ? thesvgSlugs.filter((slug) => slug.includes(search.trim().toLowerCase())).slice(0, 32) : []

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

  const preview = icon?.startsWith('data:') ? icon : icon?.startsWith('thesvg:')
    ? `https://raw.githubusercontent.com/GLINCKER/thesvg/main/public/icons/${icon.slice(7)}/default.svg` : undefined
  return <>
    <div className="space-y-1.5">
      <span className="text-sm font-medium">Site logo</span>
      <div className="flex items-center gap-3">
        <span className="flex size-9 items-center justify-center rounded-lg bg-muted">
          {preview ? <img src={preview} alt="" className="size-6 object-contain" /> : <span className="text-xs font-semibold uppercase">{name.slice(0, 2)}</span>}
        </span>
        <Button type="button" variant="outline" onClick={() => { setSearch(''); setError(''); setOpen(true) }}>Choose logo</Button>
        <span className="text-xs text-muted-foreground">{icon ? 'Custom' : 'Auto favicon'}</span>
      </div>
    </div>
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader><DialogTitle>Logo for {name || 'new site'}</DialogTitle><DialogDescription>Upload an image or search theSVG library. Auto uses the site favicon.</DialogDescription></DialogHeader>
        <div className="flex gap-2">
          <Button variant="outline" onClick={() => { onChange(undefined); setOpen(false) }}>Auto</Button>
          <label className="inline-flex h-8 cursor-pointer items-center rounded-lg border px-3 text-sm">Upload image
            <input className="sr-only" type="file" accept="image/png,image/jpeg,image/webp" onChange={(event) => upload(event.target.files?.[0])} />
          </label>
        </div>
        <Input aria-label="Search theSVG icons" placeholder="Search theSVG (e.g. grafana)" value={search} onChange={(event) => setSearch(event.target.value)} />
        <div className="grid max-h-52 grid-cols-4 gap-2 overflow-y-auto">{matches.map((slug) => <button key={slug} type="button" className="flex min-w-0 flex-col items-center gap-1 rounded-lg border p-2 text-xs hover:bg-muted" title={slug}
          onClick={() => { onChange(`thesvg:${slug}`); setOpen(false) }}>
          <img src={`https://raw.githubusercontent.com/GLINCKER/thesvg/main/public/icons/${slug}/default.svg`} alt="" className="size-7" loading="lazy" />
          <span className="w-full truncate">{slug}</span></button>)}</div>
        {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
        <p className="text-xs text-muted-foreground">Icons by theSVG (MIT). Only the selected ID is saved.</p>
      </DialogContent>
    </Dialog>
  </>
}
