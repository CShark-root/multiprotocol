import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import { useRouteContext } from '@tanstack/react-router'
import { Check, Clock, Cpu, MemoryStick, Wifi } from 'lucide-react'
import { api } from '@/lib/api'
import { formatBytes, formatDuration } from '@/lib/utils'
import { Badge, Card, PageHeader } from '@/components/ui'
import { TrafficChart } from '@/components/TrafficChart'

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
  const online = useQuery({ queryKey: ['online'], queryFn: api.online, refetchInterval: 10_000 })
  const traffic = useQuery({ queryKey: ['traffic', 30], queryFn: () => api.trafficByDay(30), refetchInterval: 60_000 })
  const account = useQuery({ queryKey: ['account'], queryFn: api.account.get })

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
          icon={<Wifi className="h-4 w-4" />}
          label={t('dashboard.online')}
          value={online.data ? String(online.data.length) : '—'}
          hint={t('dashboard.onlineHint', { count: users.data?.length ?? 0, active: activeUsers })}
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

      <Card className="mt-4">
        <h2 className="font-semibold">{t('dashboard.traffic')}</h2>
        <p className="mb-4 text-xs text-muted-foreground">{t('dashboard.trafficHint')}</p>
        {traffic.data ? <TrafficChart data={traffic.data} /> : <div className="h-[180px]" />}
      </Card>

      <div className="mt-4 grid gap-4 lg:grid-cols-2">
        <Card>
          <h2 className="font-semibold">{t('dashboard.cores')}</h2>
          {agent && agent.cores.length > 0 ? (
            <>
              <ul className="mt-4 space-y-2.5">
                {agent.cores.map((c) => (
                  <li key={c.name} className="flex items-center justify-between gap-3 text-sm">
                    <span className="font-medium capitalize">
                      {c.name}
                      {c.version && <span className="ml-1.5 font-normal text-muted-foreground">{c.version}</span>}
                    </span>
                    {!c.installed ? (
                      <Badge tone="warning">{t('dashboard.coreMissing')}</Badge>
                    ) : c.running ? (
                      <Badge tone="success">{t('dashboard.coreRunning')}</Badge>
                    ) : c.message ? (
                      <Badge tone="danger">{t('dashboard.coreStopped')}</Badge>
                    ) : (
                      <Badge tone="muted">{t('dashboard.coreIdle')}</Badge>
                    )}
                  </li>
                ))}
              </ul>
              {system.data?.sync?.lastSync && (
                <p className="mt-4 text-xs text-muted-foreground">
                  {t('dashboard.lastSync', { time: new Date(system.data.sync.lastSync).toLocaleTimeString(lng) })}
                </p>
              )}
            </>
          ) : (
            <p className="mt-2 text-sm text-muted-foreground">{t('dashboard.coresEmpty')}</p>
          )}
        </Card>

        <Card>
          <h2 className="font-semibold">{t('dashboard.security')}</h2>
          <ul className="mt-4 space-y-2.5 text-sm">
            {(['secHttps', 'secPath', 'secCookies', ...(account.data?.totpEnabled ? (['sec2fa'] as const) : [])] as const).map((k) => (
              <li key={k} className="flex items-start gap-2.5">
                <Check className="mt-0.5 h-4 w-4 shrink-0 text-success" />
                {t(`dashboard.${k}`)}
              </li>
            ))}
            {([...(account.data?.totpEnabled ? [] : (['sec2fa'] as const)), 'secMtls'] as const).map((k) => (
              <li key={k} className="flex items-start gap-2.5 text-muted-foreground">
                <span className="mt-1.5 ml-1 h-2 w-2 shrink-0 rounded-full border border-muted-foreground" />
                <span className="flex-1">{t(`dashboard.${k}`)}</span>
                <Badge tone="muted">{k === 'sec2fa' ? t('dashboard.sec2faOff') : t('common.soon')}</Badge>
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
