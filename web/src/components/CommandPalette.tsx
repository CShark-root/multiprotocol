import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { Cable, CornerDownLeft, Search, Server, User as UserIcon } from 'lucide-react'
import { api } from '@/lib/api'
import { cn } from '@/lib/utils'
import { navItems } from './AppShell'

type Hit = { id: string; icon: ReactNode; title: string; hint: string; go: () => void }

// CommandPalette — быстрый поиск по Ctrl+K: разделы, пользователи,
// подключения и серверы. Стрелки выбирают, Enter открывает, Esc закрывает.
export function CommandPalette({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [q, setQ] = useState('')
  const [sel, setSel] = useState(0)
  const input = useRef<HTMLInputElement>(null)
  const users = useQuery({ queryKey: ['users'], queryFn: api.users.list })
  const inbounds = useQuery({ queryKey: ['inbounds'], queryFn: api.inbounds.list })
  const nodes = useQuery({ queryKey: ['nodes'], queryFn: api.nodes.list })

  useEffect(() => input.current?.focus(), [])

  const hits = useMemo<Hit[]>(() => {
    const s = q.trim().toLowerCase()
    const has = (...v: (string | undefined)[]) => !s || v.some((x) => x?.toLowerCase().includes(s))
    const out: Hit[] = []
    for (const n of navItems) {
      const title = t(`nav.${n.key}`)
      if (has(title, n.key)) out.push({ id: 'nav' + n.to, icon: <n.icon className="h-4 w-4" />, title, hint: t('palette.section'), go: () => navigate({ to: n.to }) })
    }
    if (s) {
      for (const u of users.data ?? []) {
        if (has(u.name, u.note))
          out.push({ id: 'u' + u.id, icon: <UserIcon className="h-4 w-4" />, title: u.name, hint: t('palette.user'), go: () => navigate({ to: '/users', search: { user: u.id } }) })
      }
      for (const ib of inbounds.data ?? []) {
        if (has(ib.remark, ib.protocol, String(ib.port)))
          out.push({ id: 'i' + ib.id, icon: <Cable className="h-4 w-4" />, title: ib.remark, hint: `${t('palette.inbound')} · ${ib.port}`, go: () => navigate({ to: '/inbounds' }) })
      }
      for (const n of nodes.data ?? []) {
        if (has(n.name, n.host)) out.push({ id: 'n' + n.id, icon: <Server className="h-4 w-4" />, title: n.name, hint: `${t('palette.server')} · ${n.host}`, go: () => navigate({ to: '/nodes' }) })
      }
    }
    return out.slice(0, 12)
  }, [q, users.data, inbounds.data, nodes.data, t, navigate])

  const pick = (h?: Hit) => {
    if (!h) return
    h.go()
    onClose()
  }

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center bg-foreground/25 px-4 pt-[12vh]" onClick={onClose}>
      <div
        role="dialog"
        aria-label={t('palette.open')}
        className="w-full max-w-lg overflow-hidden rounded-2xl border bg-card shadow-xl"
        onClick={(e) => e.stopPropagation()}
        onKeyDown={(e) => {
          if (e.key === 'Escape') onClose()
          if (e.key === 'ArrowDown') (e.preventDefault(), setSel((v) => Math.min(v + 1, hits.length - 1)))
          if (e.key === 'ArrowUp') (e.preventDefault(), setSel((v) => Math.max(v - 1, 0)))
          if (e.key === 'Enter') pick(hits[sel])
        }}
      >
        <div className="flex items-center gap-2 border-b px-4">
          <Search className="h-4 w-4 text-muted-foreground" />
          <input
            ref={input}
            value={q}
            onChange={(e) => (setQ(e.target.value), setSel(0))}
            placeholder={t('palette.placeholder')}
            className="h-12 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
          />
          <kbd className="rounded-md border px-1.5 text-[10px] text-muted-foreground">Esc</kbd>
        </div>
        <ul className="max-h-[50vh] overflow-y-auto p-1.5">
          {hits.length === 0 && <li className="px-3 py-6 text-center text-sm text-muted-foreground">{t('palette.nothing')}</li>}
          {hits.map((h, i) => (
            <li key={h.id}>
              <button
                type="button"
                onMouseEnter={() => setSel(i)}
                onClick={() => pick(h)}
                className={cn('flex w-full cursor-pointer items-center gap-3 rounded-xl px-3 py-2 text-left text-sm', i === sel ? 'bg-accent text-accent-foreground' : 'text-foreground')}
              >
                <span className="text-muted-foreground">{h.icon}</span>
                <span className="flex-1 truncate">{h.title}</span>
                <span className="text-xs text-muted-foreground">{h.hint}</span>
                {i === sel && <CornerDownLeft className="h-3.5 w-3.5 text-muted-foreground" />}
              </button>
            </li>
          ))}
        </ul>
      </div>
    </div>
  )
}
