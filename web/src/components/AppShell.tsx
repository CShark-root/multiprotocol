import { useState, type ComponentType } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, Outlet, useNavigate, useRouteContext } from '@tanstack/react-router'
import { useQueryClient } from '@tanstack/react-query'
import {
  Activity,
  Cable,
  Gauge,
  Languages,
  LogOut,
  Menu,
  Monitor,
  Moon,
  Route as RouteIcon,
  Server,
  Settings,
  Shield,
  Sun,
  Users,
  X,
} from 'lucide-react'
import { api } from '@/lib/api'
import { getTheme, setLanguage, setTheme, type Theme } from '@/lib/prefs'
import { cn } from '@/lib/utils'
import { Button } from './ui'

type NavItem = { to: string; key: string; icon: ComponentType<{ className?: string }>; soon?: boolean }

const nav: NavItem[] = [
  { to: '/', key: 'dashboard', icon: Gauge },
  { to: '/users', key: 'users', icon: Users },
  { to: '/inbounds', key: 'inbounds', icon: Cable },
  { to: '/nodes', key: 'nodes', icon: Server, soon: true },
  { to: '/routing', key: 'routing', icon: RouteIcon, soon: true },
  { to: '/logs', key: 'logs', icon: Activity, soon: true },
  { to: '/amnezia', key: 'amnezia', icon: Shield, soon: true },
  { to: '/settings', key: 'settings', icon: Settings, soon: true },
]

const nextTheme: Record<Theme, Theme> = { system: 'light', light: 'dark', dark: 'system' }
const themeIcon = { system: Monitor, light: Sun, dark: Moon }

export function AppShell() {
  const { t, i18n } = useTranslation()
  const { admin } = useRouteContext({ from: '/_app' })
  const [open, setOpen] = useState(false)
  const [theme, setThemeState] = useState<Theme>(getTheme())
  const navigate = useNavigate()
  const qc = useQueryClient()
  const ThemeIcon = themeIcon[theme]

  async function logout() {
    await api.logout().catch(() => {})
    qc.clear()
    navigate({ to: '/login' })
  }

  const sidebar = (
    <div className="flex h-full flex-col">
      <div className="flex items-center gap-2.5 px-3 pb-6 pt-1">
        <div className="flex h-8 w-8 items-center justify-center rounded-xl bg-primary text-primary-foreground">
          <Shield className="h-4 w-4" />
        </div>
        <span className="text-[15px] font-semibold tracking-tight">panel</span>
      </div>
      <nav className="flex-1 space-y-0.5">
        {nav.map((item) => (
          <Link
            key={item.to}
            to={item.to}
            onClick={() => setOpen(false)}
            activeOptions={{ exact: item.to === '/' }}
            className="group flex items-center gap-3 rounded-xl px-3 py-2 text-sm text-muted-foreground transition hover:bg-muted hover:text-foreground"
            activeProps={{ className: '!bg-accent !text-accent-foreground font-medium' }}
          >
            <item.icon className="h-4 w-4" />
            <span className="flex-1">{t(`nav.${item.key}`)}</span>
            {item.soon && <span className="text-[10px] uppercase tracking-wide opacity-60">{t('common.soon')}</span>}
          </Link>
        ))}
      </nav>
      <div className="space-y-0.5 border-t pt-3">
        <button
          onClick={() => {
            const n = nextTheme[theme]
            setTheme(n)
            setThemeState(n)
          }}
          className="flex w-full cursor-pointer items-center gap-3 rounded-xl px-3 py-2 text-sm text-muted-foreground hover:bg-muted hover:text-foreground"
        >
          <ThemeIcon className="h-4 w-4" />
          {t(`nav.theme.${theme}`)}
        </button>
        <button
          onClick={() => {
            const lng = i18n.language === 'ru' ? 'en' : 'ru'
            i18n.changeLanguage(lng)
            setLanguage(lng)
          }}
          className="flex w-full cursor-pointer items-center gap-3 rounded-xl px-3 py-2 text-sm text-muted-foreground hover:bg-muted hover:text-foreground"
        >
          <Languages className="h-4 w-4" />
          {t('nav.language')}
        </button>
        <button
          onClick={logout}
          className="flex w-full cursor-pointer items-center gap-3 rounded-xl px-3 py-2 text-sm text-muted-foreground hover:bg-muted hover:text-foreground"
        >
          <LogOut className="h-4 w-4" />
          <span className="flex-1 text-left">{t('nav.logout')}</span>
          <span className="truncate text-xs opacity-70">{admin.username}</span>
        </button>
      </div>
    </div>
  )

  return (
    <div className="min-h-dvh lg:flex">
      <aside className="hidden w-64 shrink-0 border-r bg-card/60 p-4 lg:block lg:sticky lg:top-0 lg:h-dvh">{sidebar}</aside>

      <header className="sticky top-0 z-10 flex items-center justify-between border-b bg-background/90 px-4 py-3 backdrop-blur lg:hidden">
        <span className="font-semibold tracking-tight">panel</span>
        <Button variant="ghost" size="icon" aria-label={t('nav.menu')} onClick={() => setOpen(true)}>
          <Menu className="h-5 w-5" />
        </Button>
      </header>
      <div
        className={cn('fixed inset-0 z-20 bg-foreground/20 transition lg:hidden', open ? 'opacity-100' : 'pointer-events-none opacity-0')}
        onClick={() => setOpen(false)}
      />
      <aside
        className={cn(
          'fixed inset-y-0 left-0 z-30 w-72 border-r bg-card p-4 transition-transform lg:hidden',
          open ? 'translate-x-0' : '-translate-x-full',
        )}
      >
        <Button variant="ghost" size="icon" className="absolute right-3 top-3" aria-label="close" onClick={() => setOpen(false)}>
          <X className="h-5 w-5" />
        </Button>
        {sidebar}
      </aside>

      <main className="mx-auto w-full max-w-6xl flex-1 px-4 py-6 sm:px-8 sm:py-10">
        <Outlet />
      </main>
    </div>
  )
}
