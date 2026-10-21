import { useSyncExternalStore } from 'react'
import { zhCN } from './locales/zh-CN.ts'

export type Locale = 'en' | 'zh-CN'
export const localeStorageKey = 'olr.locale'

export function resolveLocale(saved: string | null, languages: readonly string[]): Locale {
  if (saved === 'en' || saved === 'zh-CN') return saved
  return languages[0]?.toLowerCase().startsWith('zh') ? 'zh-CN' : 'en'
}

function initialLocale(): Locale {
  let saved: string | null = null
  try { saved = localStorage.getItem(localeStorageKey) } catch { /* Storage may be disabled. */ }
  return resolveLocale(saved, typeof navigator === 'undefined' ? [] : navigator.languages)
}

let locale = initialLocale()
const listeners = new Set<() => void>()
export function getLocale(): Locale { return locale }
function publish(next: Locale) {
  locale = next
  if (typeof document !== 'undefined') document.documentElement.lang = locale
  listeners.forEach(listener => listener())
}
export function setLocale(next: Locale) {
  try { localStorage.setItem(localeStorageKey, next) } catch { /* Switching still works in memory. */ }
  publish(next)
}
if (typeof window !== 'undefined') {
  document.documentElement.lang = locale
  window.addEventListener('storage', event => {
    if (event.key === localeStorageKey || event.key === null) {
      publish(resolveLocale(event.newValue, navigator.languages))
    }
  })
}
export function useLocale() {
  return useSyncExternalStore(listener => {
    listeners.add(listener)
    return () => { listeners.delete(listener) }
  }, getLocale, () => 'en' as Locale)
}

/** English is the source catalogue and the fallback for new messages. */
export function t(message: string, values?: Record<string, string | number>): string {
  const translated = locale === 'zh-CN' ? zhCN[message] ?? message : message
  return values ? translated.replace(/\{(\w+)\}/g, (match, key: string) => String(values[key] ?? match)) : translated
}
