import { Logo } from '@/components/layout/logo'

export function AuthFrame({ children }: { children: React.ReactNode }) {
  return (
    <main className="flex min-h-svh flex-col items-center justify-center gap-6 px-4 py-8 sm:px-6">
      <div className="flex items-center gap-2.5 font-semibold tracking-tight">
        <Logo className="size-6" aria-hidden />Open Linux Router
      </div>
      {children}
    </main>
  )
}
