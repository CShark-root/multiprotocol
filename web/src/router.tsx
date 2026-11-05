import { QueryClient, queryOptions } from '@tanstack/react-query'
import { Outlet, createRootRouteWithContext, createRoute, createRouter, redirect } from '@tanstack/react-router'
import { api, basePath } from '@/lib/api'
import { AppShell } from '@/components/AppShell'
import { LoginPage, SetupPage } from '@/pages/AuthPages'
import { DashboardPage } from '@/pages/DashboardPage'
import { UsersPage } from '@/pages/UsersPage'
import { InboundsPage } from '@/pages/InboundsPage'
import { LogsPage } from '@/pages/LogsPage'
import { SettingsPage } from '@/pages/SettingsPage'
import { ServersPage } from '@/pages/ServersPage'
import { AmneziaPage } from '@/pages/AmneziaPage'
import { RoutingPage } from '@/pages/RoutingPage'

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
const usersRoute = createRoute({
  getParentRoute: () => appRoute,
  path: '/users',
  component: UsersPage,
  // ?user=7 открывает карточку пользователя (из быстрого поиска).
  validateSearch: (s: Record<string, unknown>): { user?: number } => ({ user: Number(s.user) > 0 ? Number(s.user) : undefined }),
})
const inboundsRoute = createRoute({ getParentRoute: () => appRoute, path: '/inbounds', component: InboundsPage })
const logsRoute = createRoute({ getParentRoute: () => appRoute, path: '/logs', component: LogsPage })
const settingsRoute = createRoute({ getParentRoute: () => appRoute, path: '/settings', component: SettingsPage })

const routingRoute = createRoute({ getParentRoute: () => appRoute, path: '/routing', component: RoutingPage })
const amneziaRoute = createRoute({ getParentRoute: () => appRoute, path: '/amnezia', component: AmneziaPage })
const serversRoute = createRoute({ getParentRoute: () => appRoute, path: '/nodes', component: ServersPage })

const routeTree = rootRoute.addChildren([setupRoute, loginRoute, appRoute.addChildren([dashboardRoute, usersRoute, inboundsRoute, logsRoute, settingsRoute, routingRoute, amneziaRoute, serversRoute])])

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
