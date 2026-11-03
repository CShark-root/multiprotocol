import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider } from '@tanstack/react-router'
import '@fontsource-variable/inter'
import './index.css'
import './i18n'
import { onUnauthorized, type AuthState } from '@/lib/api'
import { applyTheme, getTheme } from '@/lib/prefs'
import { authStateQuery, queryClient, router } from './router'

applyTheme(getTheme())
matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => applyTheme(getTheme()))

onUnauthorized(() => {
  queryClient.setQueryData<AuthState>(authStateQuery.queryKey, { setupRequired: false, authenticated: false })
  router.navigate({ to: '/login' })
})

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
)
