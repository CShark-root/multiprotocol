import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import { AlertTriangle, ChevronDown, CircleCheck, Info, Pause, Play, Search, ShieldAlert } from 'lucide-react'
import { api, logStreamUrl, type AuditRecord, type LogEntry, type LogFilter, type PanelEvent } from '@/lib/api'
import { cn, formatBytes } from '@/lib/utils'
import { Badge, Button, Card, EmptyState, Input, PageHeader, Select } from '@/components/ui'

type Tab = 'system' | 'events' | 'audit'

export function LogsPage() {
  const { t } = useTranslation()
  const [tab, setTab] = useState<Tab>('system')
  return (
    <>
      <PageHeader title={t('logs.title')} subtitle={t('logs.subtitle')} />
      <div className="mb-4 inline-flex rounded-xl bg-muted p-1">
        {(['system', 'events', 'audit'] as Tab[]).map((k) => (
          <button
            key={k}
            onClick={() => setTab(k)}
            className={cn(
              'cursor-pointer rounded-lg px-4 py-1.5 text-sm font-medium transition',
              tab === k ? 'bg-card text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground',
            )}
          >
            {t(`logs.tabs.${k}`)}
          </button>
        ))}
      </div>
      {tab === 'system' && <SystemLogs />}
      {tab === 'events' && <EventsList />}
      {tab === 'audit' && <AuditList />}
    </>
  )
}

function when(iso: string, lng: string) {
  const d = new Date(iso)
  const today = new Date().toDateString() === d.toDateString()
  return today
    ? d.toLocaleTimeString(lng, { hour: '2-digit', minute: '2-digit', second: '2-digit' })
    : d.toLocaleString(lng, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })
}

const levelDot: Record<LogEntry['level'], string> = {
  debug: 'bg-muted-foreground/40',
  info: 'bg-success',
  warn: 'bg-warning',
  error: 'bg-destructive',
}

const MAX_LIVE = 500

function SystemLogs() {
  const { t, i18n } = useTranslation()
  const [source, setSource] = useState('')
  const [level, setLevel] = useState('')
  const [search, setSearch] = useState('')
  const [q, setQ] = useState('')
  const [paused, setPaused] = useState(false)
  const [live, setLive] = useState<LogEntry[]>([])
  const [older, setOlder] = useState<LogEntry[]>([])
  const pausedRef = useRef(paused)
  pausedRef.current = paused

  // Debounce the search box.
  useEffect(() => {
    const id = setTimeout(() => setQ(search.trim()), 350)
    return () => clearTimeout(id)
  }, [search])

  const filter: LogFilter = useMemo(() => ({ source, level, q }), [source, level, q])
  const history = useQuery({ queryKey: ['logs', filter], queryFn: () => api.logs(filter) })

  // Live tail: reset when the filter changes, then append from the stream.
  useEffect(() => {
    setLive([])
    setOlder([])
    const es = new EventSource(logStreamUrl(filter))
    es.onmessage = (m) => {
      if (pausedRef.current) return
      const e = JSON.parse(m.data) as LogEntry
      setLive((cur) => [e, ...cur].slice(0, MAX_LIVE))
    }
    return () => es.close()
  }, [filter])

  const rows = [...live, ...(history.data ?? []), ...older]
  const oldestID = (older.length ? older : (history.data ?? [])).at(-1)?.id

  async function loadOlder() {
    if (!oldestID) return
    const more = await api.logs({ ...filter, before: oldestID })
    setOlder((cur) => [...cur, ...more])
  }

  return (
    <Card className="p-0">
      <div className="flex flex-wrap items-center gap-2 border-b p-3">
        <Select value={source} onChange={(e) => setSource(e.target.value)} className="h-9 w-auto">
          <option value="">{t('logs.allSources')}</option>
          {['panel', 'xray', 'hysteria', 'sing-box'].map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </Select>
        <Select value={level} onChange={(e) => setLevel(e.target.value)} className="h-9 w-auto">
          <option value="">{t('logs.allLevels')}</option>
          <option value="warn">{t('logs.levelWarn')}</option>
          <option value="error">{t('logs.levelError')}</option>
        </Select>
        <div className="relative min-w-48 flex-1">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input value={search} onChange={(e) => setSearch(e.target.value)} placeholder={t('logs.search')} className="h-9 pl-9" />
        </div>
        <Button variant="secondary" size="sm" onClick={() => setPaused(!paused)}>
          {paused ? <Play className="h-4 w-4" /> : <Pause className="h-4 w-4" />}
          {paused ? t('logs.resume') : t('logs.pause')}
        </Button>
        <span className={cn('flex items-center gap-1.5 text-xs', paused ? 'text-muted-foreground' : 'text-success')}>
          <span className={cn('h-2 w-2 rounded-full', paused ? 'bg-muted-foreground' : 'animate-pulse bg-success')} />
          {paused ? t('logs.paused') : t('logs.live')}
        </span>
      </div>

      {rows.length === 0 && !history.isPending ? (
        <EmptyState icon={<Info className="h-6 w-6" />} title={t('logs.empty')} text={t('logs.emptyText')} />
      ) : (
        <div className="max-h-[65vh] overflow-y-auto font-mono text-xs">
          {rows.map((e, i) => (
            <div key={`${e.id}-${e.ts}-${i}`} className="flex gap-3 border-b border-border/60 px-4 py-1.5 hover:bg-muted/40">
              <span className="shrink-0 text-muted-foreground">{when(e.ts, i18n.language)}</span>
              <span className={cn('mt-1.5 h-2 w-2 shrink-0 rounded-full', levelDot[e.level])} title={e.level} />
              <span className="w-16 shrink-0 truncate text-accent-foreground">{e.source}</span>
              <span className="min-w-0 flex-1 break-words">
                {e.msg}
                {e.fields &&
                  Object.entries(e.fields).map(([k, v]) => (
                    <span key={k} className="ml-2 text-muted-foreground">
                      {k}={v}
                    </span>
                  ))}
              </span>
            </div>
          ))}
          {oldestID && (
            <div className="p-3 text-center">
              <Button variant="ghost" size="sm" onClick={loadOlder}>
                {t('logs.older')}
              </Button>
            </div>
          )}
        </div>
      )}
    </Card>
  )
}

const severityIcon = {
  info: <CircleCheck className="h-4 w-4 text-success" />,
  warning: <AlertTriangle className="h-4 w-4 text-warning" />,
  danger: <ShieldAlert className="h-4 w-4 text-destructive" />,
}

function eventText(t: (k: string, o?: Record<string, unknown>) => string, e: PanelEvent, lng: string) {
  const key = `logs.events.${e.type.replace('.', '_')}`
  // Byte counts arrive raw; the texts get them formatted as {{bytesText}}.
  const bytes = typeof e.data?.bytes === 'number' ? formatBytes(e.data.bytes as number, lng) : undefined
  const text = t(key, { name: e.userName || '—', ...e.data, bytesText: bytes, defaultValue: '' })
  return text || e.type
}

function EventsList() {
  const { t, i18n } = useTranslation()
  const [more, setMore] = useState<PanelEvent[]>([])
  const events = useQuery({ queryKey: ['events'], queryFn: () => api.events(), refetchInterval: 15_000 })
  const rows = [...(events.data ?? []), ...more]
  const oldest = rows.at(-1)?.id

  return (
    <Card className="p-0">
      {rows.length === 0 && !events.isPending ? (
        <EmptyState icon={<Info className="h-6 w-6" />} title={t('logs.empty')} text={t('logs.emptyText')} />
      ) : (
        <ul>
          {rows.map((e) => (
            <li key={e.id} className="flex items-start gap-3 border-b px-5 py-3 last:border-0">
              <span className="mt-0.5">{severityIcon[e.severity]}</span>
              <div className="min-w-0 flex-1">
                <div className="text-sm">{eventText(t, e, i18n.language)}</div>
                {e.message && <div className="mt-0.5 break-words font-mono text-xs text-muted-foreground">{e.message}</div>}
              </div>
              <span className="shrink-0 text-xs text-muted-foreground">{when(e.ts, i18n.language)}</span>
            </li>
          ))}
          {oldest && events.data && events.data.length >= 100 && (
            <li className="p-3 text-center">
              <Button variant="ghost" size="sm" onClick={async () => setMore([...more, ...(await api.events(oldest))])}>
                {t('logs.older')}
              </Button>
            </li>
          )}
        </ul>
      )}
    </Card>
  )
}

function AuditList() {
  const { t, i18n } = useTranslation()
  const audit = useQuery({ queryKey: ['audit'], queryFn: () => api.audit() })
  const [open, setOpen] = useState<number | null>(null)

  if (audit.data?.length === 0) {
    return (
      <Card>
        <EmptyState icon={<Info className="h-6 w-6" />} title={t('logs.empty')} text={t('logs.emptyText')} />
      </Card>
    )
  }
  return (
    <Card className="p-0">
      <ul>
        {(audit.data ?? []).map((r: AuditRecord) => (
          <li key={r.id} className="border-b px-5 py-3 last:border-0">
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
              <span className="font-medium">{t(`logs.actions.${r.action.replace('.', '_')}`, { defaultValue: r.action })}</span>
              {r.targetId !== '0' && <Badge tone="muted">#{r.targetId}</Badge>}
              <span className="text-muted-foreground">{r.admin || t('logs.system')}</span>
              <span className="ml-auto text-xs text-muted-foreground">{when(r.ts, i18n.language)}</span>
              {r.diff !== null && JSON.stringify(r.diff) !== '{}' && (
                <button
                  onClick={() => setOpen(open === r.id ? null : r.id)}
                  className="flex cursor-pointer items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
                >
                  {t('logs.details')}
                  <ChevronDown className={cn('h-3.5 w-3.5 transition', open === r.id && 'rotate-180')} />
                </button>
              )}
            </div>
            {open === r.id && (
              <pre className="mt-2 max-h-64 overflow-auto rounded-lg bg-muted p-3 text-xs">{JSON.stringify(r.diff, null, 2)}</pre>
            )}
          </li>
        ))}
      </ul>
    </Card>
  )
}
