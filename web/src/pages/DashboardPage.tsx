import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import { useRouteContext } from '@tanstack/react-router'
import { Check, Clock, Cpu, MemoryStick, Users } from 'lucide-react'
import { api } from '@/lib/api'
import { formatBytes, formatDuration } from '@/lib/utils'
import { Badge, Card, PageHeader } from '@/components/ui'

function Stat({ icon, label, value, hint }: { icon: ReactNode; label: string; value: string; hint?: string }) {
  return (
    <Card>
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        {icon}
        {label}
      </div>
      <div className="mt-3 text-2xl font-semibold tracking-tight">{value}</div>
      {hint && <div className="mt-1 text-xs text-muted-foreground">{hint}</div>}
    </Card>
  )
}

export function DashboardPage() {
  const { t, i18n } = useTranslation()
  const { admin } = useRouteContext({ from: '/_app' })
  const system = useQuery({ queryKey: ['system'], queryFn: api.system, refetchInterval: 10_000 })
  const users = useQuery({ queryKey: ['users'], queryFn: api.users.list })

  const agent = system.data?.localAgent
  // memTotal is 0 when the host exposes no /proc (dev builds on Windows/macOS).
  const sys = agent?.system.memTotal ? agent.system : undefined
  const activeUsers = users.data?.filter((u) => u.status === 'active').length ?? 0
  const lng = i18n.language

  return (
    <>
      <PageHeader title={t('dashboard.hello', { name: admin.username })} subtitle={t('dashboard.subtitle')} />

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <Stat
          icon={<Users className="h-4 w-4" />}
          label={t('dashboard.users')}
          value={String(users.data?.length ?? '—')}
          hint={t('dashboard.usersActive', { count: activeUsers })}
        />
        <Stat
          icon={<MemoryStick className="h-4 w-4" />}
          label={t('dashboard.memory')}
          value={sys ? `${Math.round(((sys.memTotal - sys.memAvailable) / sys.memTotal) * 100)}%` : '—'}
          hint={sys ? t('dashboard.memoryFree', { free: formatBytes(sys.memAvailable, lng), total: formatBytes(sys.memTotal, lng) }) : undefined}
        />
        <Stat
          icon={<Cpu className="h-4 w-4" />}
          label={t('dashboard.load')}
          value={sys ? `${sys.load1.toFixed(2)} · ${sys.load5.toFixed(2)} · ${sys.load15.toFixed(2)}` : '—'}
          hint={t('dashboard.loadHint')}
        />
        <Stat
          icon={<Clock className="h-4 w-4" />}
          label={t('dashboard.uptime')}
          value={sys ? formatDuration(sys.uptimeSeconds, lng) : '—'}
          hint={agent ? `${agent.hostname} · ${agent.os}/${agent.arch}` : t('dashboard.noAgent')}
        />
      </div>

      <div className="mt-4 grid gap-4 lg:grid-cols-2">
        <Card>
          <h2 className="font-semibold">{t('dashboard.cores')}</h2>
          {agent && agent.cores.length > 0 ? (
            <ul className="mt-4 space-y-2">
              {agent.cores.map((c) => (
                <li key={c.protocol} className="flex items-center justify-between text-sm">
                  <span>{c.protocol}</span>
                  <Badge tone={c.running ? 'success' : 'danger'}>{c.version}</Badge>
                </li>
              ))}
            </ul>
          ) : (
            <p className="mt-2 text-sm text-muted-foreground">{t('dashboard.coresEmpty')}</p>
          )}
        </Card>

        <Card>
          <h2 className="font-semibold">{t('dashboard.security')}</h2>
          <ul className="mt-4 space-y-2.5 text-sm">
            {(['secHttps', 'secPath', 'secCookies'] as const).map((k) => (
              <li key={k} className="flex items-start gap-2.5">
                <Check className="mt-0.5 h-4 w-4 shrink-0 text-success" />
                {t(`dashboard.${k}`)}
              </li>
            ))}
            {(['sec2fa', 'secMtls'] as const).map((k) => (
              <li key={k} className="flex items-start gap-2.5 text-muted-foreground">
                <span className="mt-1.5 ml-1 h-2 w-2 shrink-0 rounded-full border border-muted-foreground" />
                <span className="flex-1">{t(`dashboard.${k}`)}</span>
                <Badge tone="muted">{t('common.soon')}</Badge>
              </li>
            ))}
          </ul>
          <p className="mt-5 text-xs text-muted-foreground">
            {t('dashboard.version')}: {system.data?.version ?? '—'}
          </p>
        </Card>
      </div>
    </>
  )
}
