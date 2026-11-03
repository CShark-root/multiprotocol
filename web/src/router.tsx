import { QueryClient, queryOptions } from '@tanstack/react-query'
import { Outlet, createRootRouteWithContext, createRoute, createRouter, redirect } from '@tanstack/react-router'
import { api, basePath } from '@/lib/api'
import { AppShell } from '@/components/AppShell'
import { LoginPage, SetupPage } from '@/pages/AuthPages'
import { DashboardPage } from '@/pages/DashboardPage'
import { UsersPage } from '@/pages/UsersPage'
import { InboundsPage } from '@/pages/InboundsPage'
import { ComingSoonPage, type SoonSection } from '@/pages/ComingSoonPage'

export const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false } },
})

export const authStateQuery = queryOptions({ queryKey: ['auth', 'state'], queryFn: api.authState })

const rootRoute = createRootRouteWithContext<{ queryClient: QueryClient }>()({
  component: Outlet,
})

const setupRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/setup',
  beforeLoad: async ({ context }) => {
    const s = await context.queryClient.ensureQueryData(authStateQuery)
    if (!s.setupRequired) throw redirect({ to: s.authenticated ? '/' : '/login' })
  },
  component: SetupPage,
})

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/login',
  beforeLoad: async ({ context }) => {
    const s = await context.queryClient.ensureQueryData(authStateQuery)
    if (s.setupRequired) throw redirect({ to: '/setup' })
    if (s.authenticated) throw redirect({ to: '/' })
  },
  component: LoginPage,
})

const appRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: '_app',
  beforeLoad: async ({ context }) => {
    const s = await context.queryClient.ensureQueryData(authStateQuery)
    if (s.setupRequired) throw redirect({ to: '/setup' })
    if (!s.authenticated || !s.admin) throw redirect({ to: '/login' })
    return { admin: s.admin }
  },
  component: AppShell,
})

const dashboardRoute = createRoute({ getParentRoute: () => appRoute, path: '/', component: DashboardPage })
const usersRoute = createRoute({ getParentRoute: () => appRoute, path: '/users', component: UsersPage })
const inboundsRoute = createRoute({ getParentRoute: () => appRoute, path: '/inbounds', component: InboundsPage })

const soonSections: SoonSection[] = ['nodes', 'routing', 'logs', 'amnezia', 'settings']
const soonRoutes = soonSections.map((section) =>
  createRoute({ getParentRoute: () => appRoute, path: `/${section}`, component: () => <ComingSoonPage section={section} /> }),
)

const routeTree = rootRoute.addChildren([setupRoute, loginRoute, appRoute.addChildren([dashboardRoute, usersRoute, inboundsRoute, ...soonRoutes])])

export const router = createRouter({
  routeTree,
  basepath: basePath.replace(/\/$/, ''),
  context: { queryClient },
  defaultPreload: 'intent',
})

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}
