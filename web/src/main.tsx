import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ThemeProvider } from 'next-themes'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router'

import { App } from '@/App'
import { Toaster } from '@/components/ui/sonner'
import { ApiError } from '@/lib/api'

import './index.css'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // Retrying a 401 only spends requests on a token that will not change
      // until the operator types a new one.
      retry: (count, error) => !(error instanceof ApiError && error.unauthorized) && count < 2,
      // olrd caches nothing and reads the system fresh on every request
      // (design.md §4.5), so a cached answer here would be the only stale copy
      // in the whole stack.
      staleTime: 0,
    },
  },
})

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <ThemeProvider attribute="class" defaultTheme="system" enableSystem disableTransitionOnChange>
        <BrowserRouter>
          <App />
        </BrowserRouter>
        <Toaster richColors closeButton />
      </ThemeProvider>
    </QueryClientProvider>
  </StrictMode>,
)
