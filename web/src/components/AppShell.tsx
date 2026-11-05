import { useEffect, useState, type ComponentType } from 'react'
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
  PanelLeftClose,
  PanelLeftOpen,
  Route as RouteIcon,
  Search,
  Server,
  Settings,
  Sun,
  Users,
  X,
} from 'lucide-react'
import { api } from '@/lib/api'
import { getTheme, setLanguage, setTheme, type Theme } from '@/lib/prefs'
import { cn } from '@/lib/utils'
import { Button } from './ui'
import { CommandPalette } from './CommandPalette'

type Icon = ComponentType<{ className?: string }>
type NavItem = { to: string; key: string; icon: Icon }

// AmneziaIcon — штатный знак Amnezia: буква «A» в круге.
export function AmneziaIcon({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" className={className} aria-hidden>
      <circle cx="12" cy="12" r="9.5" />
      <path d="M8.6 16.4 12 7.6l3.4 8.8M9.9 13.2h4.2" />
    </svg>
  )
}

// Logo — минималистичная буква «M» в скруглённом квадрате.
export function Logo({ className }: { className?: string }) {
  return (
    <div className={cn('flex h-8 w-8 shrink-0 select-none items-center justify-center rounded-[10px] bg-primary text-[17px] font-semibold tracking-tighter text-primary-foreground', className)}>
      M
    </div>
  )
}

// Меню по группам, как на макете.
const groups: { key: string; items: NavItem[] }[] = [
  { key: 'overview', items: [{ to: '/', key: 'dashboard', icon: Gauge }] },
  {
    key: 'manage',
    items: [
      { to: '/users', key: 'users', icon: Users },
      { to: '/inbounds', key: 'inbounds', icon: Cable },
      { to: '/nodes', key: 'nodes', icon: Server },
      { to: '/routing', key: 'routing', icon: RouteIcon },
      { to: '/amnezia', key: 'amnezia', icon: AmneziaIcon },
    ],
  },
  {
    key: 'system',
    items: [
      { to: '/logs', key: 'logs', icon: Activity },
      { to: '/settings', key: 'settings', icon: Settings },
    ],
  },
]
export const navItems = groups.flatMap((g) => g.items)

const nextTheme: Record<Theme, Theme> = { system: 'light', light: 'dark', dark: 'system' }
const themeIcon = { system: Monitor, light: Sun, dark: Moon }

function savedCollapsed() {
  try {
    return localStorage.getItem('nav.collapsed') === '1'
  } catch {
    return false
  }
}

export function AppShell() {
  const { t, i18n } = useTranslation()
  const { admin } = useRouteContext({ from: '/_app' })
  const [open, setOpen] = useState(false)
  const [collapsed, setCollapsed] = useState(savedCollapsed)
  const [palette, setPalette] = useState(false)
  const [theme, setThemeState] = useState<Theme>(getTheme())
  const navigate = useNavigate()
  const qc = useQueryClient()
  const ThemeIcon = themeIcon[theme]

  // Ctrl+K / ⌘K открывает быстрый поиск.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setPalette((v) => !v)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  const toggleCollapsed = () => {
    setCollapsed((c) => {
      try {
        localStorage.setItem('nav.collapsed', c ? '0' : '1')
      } catch {
        // приватный режим: просто не запомнится
      }
      return !c
    })
  }

  async function logout() {
    await api.logout().catch(() => {})
    qc.clear()
    navigate({ to: '/login' })
  }

  const rowCls = (narrow: boolean) =>
    cn(
      'flex w-full cursor-pointer items-center gap-3 rounded-xl py-2 text-sm text-muted-foreground transition hover:bg-muted hover:text-foreground',
      narrow ? 'justify-center px-0' : 'px-3',
    )

  const sidebar = (narrow: boolean) => (
    <div className="flex h-full flex-col">
      <div className={cn('flex items-center gap-2.5 pb-4 pt-1', narrow ? 'justify-center' : 'px-2')}>
        <Logo />
        {!narrow && <span className="flex-1 text-[15px] font-semibold tracking-tight">MultiProtocol</span>}
      </div>
      <button type="button" onClick={() => setPalette(true)} className={cn(rowCls(narrow), 'mb-2 border')} title={t('palette.open')}>
        <Search className="h-4 w-4" />
        {!narrow && (
          <>
            <span className="flex-1 text-left">{t('palette.search')}</span>
            <kbd className="rounded-md border px-1.5 text-[10px] font-normal">Ctrl K</kbd>
          </>
        )}
      </button>
      <nav className="flex-1 space-y-0.5 overflow-y-auto">
        {groups.map((g) => (
          <div key={g.key} className="pb-1">
            {narrow ? <div className="mx-auto my-2 h-px w-6 bg-border" /> : <div className="px-3 pb-1 pt-3 text-[11px] text-muted-foreground/80">{t(`nav.group_${g.key}`)}</div>}
            {g.items.map((item) => (
              <Link
                key={item.to}
                to={item.to}
                onClick={() => setOpen(false)}
                activeOptions={{ exact: item.to === '/' }}
                title={narrow ? t(`nav.${item.key}`) : undefined}
                className={cn('group flex items-center gap-3 rounded-xl py-2 text-sm text-muted-foreground transition hover:bg-muted hover:text-foreground', narrow ? 'justify-center' : 'px-3')}
                activeProps={{ className: '!bg-accent !text-accent-foreground font-medium' }}
              >
                <item.icon className="h-[18px] w-[18px]" />
                {!narrow && <span className="flex-1">{t(`nav.${item.key}`)}</span>}
              </Link>
            ))}
          </div>
        ))}
      </nav>
      <div className="space-y-0.5 border-t pt-3">
        <button
          onClick={() => {
            const n = nextTheme[theme]
            setTheme(n)
            setThemeState(n)
          }}
          className={rowCls(narrow)}
          title={t(`nav.theme.${theme}`)}
        >
          <ThemeIcon className="h-4 w-4" />
          {!narrow && t(`nav.theme.${theme}`)}
        </button>
        <button
          onClick={() => {
            const lng = i18n.language === 'ru' ? 'en' : 'ru'
            i18n.changeLanguage(lng)
            setLanguage(lng)
          }}
          className={rowCls(narrow)}
          title={t('nav.language')}
        >
          <Languages className="h-4 w-4" />
          {!narrow && t('nav.language')}
        </button>
        <button onClick={logout} className={rowCls(narrow)} title={t('nav.logout')}>
          <LogOut className="h-4 w-4" />
          {!narrow && (
            <>
              <span className="flex-1 text-left">{t('nav.logout')}</span>
              <span className="truncate text-xs opacity-70">{admin.username}</span>
            </>
          )}
        </button>
        <button onClick={toggleCollapsed} className={cn(rowCls(narrow), 'hidden lg:flex')} title={collapsed ? t('nav.expand') : t('nav.collapse')}>
          {collapsed ? <PanelLeftOpen className="h-4 w-4" /> : <PanelLeftClose className="h-4 w-4" />}
          {!narrow && t('nav.collapse')}
        </button>
      </div>
    </div>
  )

  return (
    <div className="min-h-dvh lg:flex">
      <aside className={cn('hidden shrink-0 border-r bg-card/60 py-4 transition-[width] lg:sticky lg:top-0 lg:block lg:h-dvh', collapsed ? 'w-[68px] px-2.5' : 'w-64 px-4')}>
        {sidebar(collapsed)}
      </aside>

      <header className="sticky top-0 z-10 flex items-center justify-between gap-2 border-b bg-background/90 px-4 py-3 backdrop-blur lg:hidden">
        {/* Меню выезжает слева, поэтому кнопка тоже слева. */}
        <Button variant="ghost" size="icon" aria-label={t('nav.menu')} onClick={() => setOpen(true)}>
          <Menu className="h-5 w-5" />
        </Button>
        <div className="flex items-center gap-2">
          <Logo className="h-7 w-7 text-[15px]" />
          <span className="font-semibold tracking-tight">MultiProtocol</span>
        </div>
        <Button variant="ghost" size="icon" aria-label={t('palette.open')} onClick={() => setPalette(true)}>
          <Search className="h-5 w-5" />
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
        {sidebar(false)}
      </aside>

      <main className="mx-auto w-full max-w-6xl flex-1 px-4 py-6 sm:px-8 sm:py-10">
        <Outlet />
      </main>
      {palette && <CommandPalette onClose={() => setPalette(false)} />}
    </div>
  )
}
