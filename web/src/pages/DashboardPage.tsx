import { useEffect, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useRouteContext } from '@tanstack/react-router'
import { ArrowDown, ArrowUp, Check, Clock, Cpu, Gauge, MemoryStick, Power, RotateCw, Wifi } from 'lucide-react'
import { ApiError, api } from '@/lib/api'
import { formatBitrate, formatBytes, formatDuration, loadPercent } from '@/lib/utils'
import { Badge, Button, Card, Dialog, Field, Input, PageHeader, Ring } from '@/components/ui'
import type { User } from '@/lib/api'
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
  const system = useQuery({ queryKey: ['system'], queryFn: api.system, refetchInterval: 5_000 })
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
      <PageHeader
        title={t('dashboard.hello', { name: admin.username })}
        subtitle={t('dashboard.subtitle')}
        actions={
          (system.data?.ipv4 || system.data?.ipv6) && (
            <div className="flex flex-wrap gap-2">
              {system.data.ipv4 && <ServerIP label="IPv4" ip={system.data.ipv4} />}
              {system.data.ipv6 && <ServerIP label="IPv6" ip={system.data.ipv6} />}
            </div>
          )
        }
      />

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5">
        <Stat
          icon={<Wifi className="h-4 w-4" />}
          label={t('dashboard.online')}
          value={online.data ? String(online.data.length) : '—'}
          hint={t('dashboard.onlineHint', { count: users.data?.length ?? 0, active: activeUsers })}
        />
        <Card>
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Gauge className="h-4 w-4" />
            {t('dashboard.speed')}
          </div>
          <div className="mt-3 space-y-0.5 text-lg font-semibold tracking-tight">
            <div className="flex items-center gap-1.5" title="in">
              <ArrowDown className="h-4 w-4 text-muted-foreground" />
              {sys?.netIface ? formatBitrate(sys.netRxBps, lng) : '—'}
            </div>
            <div className="flex items-center gap-1.5" title="out">
              <ArrowUp className="h-4 w-4 text-muted-foreground" />
              {sys?.netIface ? formatBitrate(sys.netTxBps, lng) : '—'}
            </div>
          </div>
          {sys?.netIface && <div className="mt-1 text-xs text-muted-foreground">{t('dashboard.speedHint', { iface: sys.netIface })}</div>}
        </Card>
        <Stat
          icon={<MemoryStick className="h-4 w-4" />}
          label={t('dashboard.memory')}
          value={sys ? `${Math.round(((sys.memTotal - sys.memAvailable) / sys.memTotal) * 100)}%` : '—'}
          hint={sys ? t('dashboard.memoryFree', { free: formatBytes(sys.memAvailable, lng), total: formatBytes(sys.memTotal, lng) }) : undefined}
        />
        <Stat
          icon={<Cpu className="h-4 w-4" />}
          label={t('dashboard.load')}
          value={sys?.cpus ? `${loadPercent(sys.load1, sys.cpus)}%` : '—'}
          hint={
            sys?.cpus
              ? t('dashboard.loadHint', { l1: sys.load1.toFixed(2), l5: sys.load5.toFixed(2), l15: sys.load15.toFixed(2), cpus: sys.cpus })
              : undefined
          }
        />
        <Stat
          icon={<Clock className="h-4 w-4" />}
          label={t('dashboard.uptime')}
          value={sys ? formatDuration(sys.uptimeSeconds, lng) : '—'}
          hint={agent ? `${agent.hostname} · ${agent.os}/${agent.arch}` : t('dashboard.noAgent')}
        />
      </div>

      <div className="mt-4 grid gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(0,2fr)]">
        <UsersCard users={users.data ?? []} online={online.data?.length ?? 0} />
        <Card>
          <h2 className="font-semibold">{t('dashboard.traffic')}</h2>
          <p className="mb-4 text-xs text-muted-foreground">{t('dashboard.trafficHint')}</p>
          {traffic.data ? <TrafficChart data={traffic.data} /> : <div className="h-[180px]" />}
        </Card>
      </div>

      <div className="mt-4 grid gap-4 lg:grid-cols-2 xl:grid-cols-3">
        <Card>
          <h2 className="font-semibold">{t('dashboard.cores')}</h2>
          {agent && agent.cores.length > 0 ? (
            <>
              <ul className="mt-4 space-y-2.5">
                {agent.cores.map((c) => (
                  <li key={c.name} className="flex items-center justify-between gap-3 text-sm">
                    <span className={c.name === 'sniproxy' ? 'font-medium' : 'font-medium capitalize'}>
                      {c.name === 'sniproxy' ? t('dashboard.sniproxy') : c.name}
                      {c.version && c.name !== 'sniproxy' && <span className="ml-1.5 font-normal text-muted-foreground">{c.version}</span>}
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
          <ServerActions hasAgent={Boolean(agent)} />
        </Card>

        <Card>
          <h2 className="font-semibold">{t('dashboard.server')}</h2>
          {sys ? (
            <dl className="mt-4 space-y-2.5 text-sm">
              <Row label={t('dashboard.serverOs')} value={sys.osName || agent?.os || '—'} hint={sys.kernel ? `${t('dashboard.serverKernel')} ${sys.kernel}` : undefined} />
              <Row
                label={t('dashboard.serverDisk')}
                value={sys.diskTotal ? t('dashboard.freeOf', { free: formatBytes(sys.diskFree, lng), total: formatBytes(sys.diskTotal, lng) }) : '—'}
                warn={sys.diskTotal > 0 && sys.diskFree / sys.diskTotal < 0.1}
              />
              <Row label={t('dashboard.serverSwap')} value={sys.swapTotal ? t('dashboard.freeOf', { free: formatBytes(sys.swapFree, lng), total: formatBytes(sys.swapTotal, lng) }) : t('dashboard.serverNoSwap')} />
              <Row
                label={t('dashboard.serverTcp')}
                value={sys.congestion ? `${sys.congestion}${sys.qdisc ? ' + ' + sys.qdisc : ''}` : '—'}
                hint={sys.congestion && sys.congestion !== 'bbr' ? t('dashboard.serverNoBbr') : undefined}
                warn={Boolean(sys.congestion) && sys.congestion !== 'bbr'}
              />
              <Row label={t('dashboard.serverCpu')} value={t('dashboard.serverCpuValue', { count: sys.cpus, procs: sys.processes })} />
            </dl>
          ) : (
            <p className="mt-2 text-sm text-muted-foreground">{t('dashboard.noAgent')}</p>
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

function ServerActions({ hasAgent }: { hasAgent: boolean }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [confirmCores, setConfirmCores] = useState(false)
  const [rebootOpen, setRebootOpen] = useState(false)
  const [password, setPassword] = useState('')
  const [rebooting, setRebooting] = useState(false)
  const cores = useMutation({
    mutationFn: api.restartCores,
    onSettled: () => {
      setConfirmCores(false)
      qc.invalidateQueries({ queryKey: ['system'] })
    },
  })
  const reboot = useMutation({
    mutationFn: () => api.reboot(password),
    onSuccess: () => {
      setRebootOpen(false)
      setRebooting(true)
    },
  })
  const rebootError =
    reboot.error instanceof ApiError
      ? reboot.error.code === 'wrong_password'
        ? t('dashboard.wrongPassword')
        : reboot.error.message
      : reboot.isError
        ? t('common.error')
        : ''

  // While the machine is down every request fails; once the panel answers
  // again, reload so the page picks up the fresh state.
  useEffect(() => {
    if (!rebooting) return
    let seenDown = false
    const id = setInterval(async () => {
      try {
        await api.authState()
        if (seenDown) window.location.reload()
      } catch {
        seenDown = true
      }
    }, 3000)
    return () => clearInterval(id)
  }, [rebooting])

  if (rebooting) {
    return <p className="mt-4 rounded-xl bg-accent px-3.5 py-2.5 text-sm text-accent-foreground">{t('dashboard.rebooting')}</p>
  }
  return (
    <div className="mt-4 border-t pt-4">
      {confirmCores ? (
        <div className="flex flex-wrap items-center justify-between gap-2 text-sm">
          <span className="text-muted-foreground">{t('dashboard.restartCoresConfirm')}</span>
          <div className="flex gap-2">
            <Button variant="secondary" size="sm" onClick={() => setConfirmCores(false)}>
              {t('common.cancel')}
            </Button>
            <Button size="sm" disabled={cores.isPending} onClick={() => cores.mutate()}>
              {t('dashboard.restartCores')}
            </Button>
          </div>
        </div>
      ) : (
        <div className="flex flex-wrap gap-2">
          <Button variant="secondary" size="sm" disabled={!hasAgent} onClick={() => setConfirmCores(true)}>
            <RotateCw className="h-4 w-4" />
            {t('dashboard.restartCores')}
          </Button>
          <Button variant="ghost" size="sm" onClick={() => setRebootOpen(true)}>
            <Power className="h-4 w-4" />
            {t('dashboard.rebootServer')}
          </Button>
        </div>
      )}
      {cores.isError && <p className="mt-2 text-sm text-destructive">{cores.error instanceof ApiError ? cores.error.message : t('common.error')}</p>}
      {cores.isSuccess && !confirmCores && <p className="mt-2 text-xs text-muted-foreground">{t('dashboard.coresRestarted')}</p>}

      <Dialog open={rebootOpen} onClose={() => setRebootOpen(false)} title={t('dashboard.rebootTitle')} description={t('dashboard.rebootText')}>
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault()
            reboot.mutate()
          }}
        >
          <Field label={t('dashboard.rebootPassword')} error={rebootError || undefined}>
            {(id) => <Input id={id} type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} autoFocus />}
          </Field>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="secondary" onClick={() => setRebootOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" variant="danger" disabled={reboot.isPending || !password}>
              {t('dashboard.rebootNow')}
            </Button>
          </div>
        </form>
      </Dialog>
    </div>
  )
}

function Row({ label, value, hint, warn }: { label: string; value: string; hint?: string; warn?: boolean }) {
  return (
    <div className="flex items-start justify-between gap-3">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="text-right">
        <div className={warn ? 'font-medium text-warning' : 'font-medium'}>{value}</div>
        {hint && <div className="text-xs text-muted-foreground">{hint}</div>}
      </dd>
    </div>
  )
}

// UsersCard — кольцо пользователей по статусам, как на макете.
function UsersCard({ users, online }: { users: User[]; online: number }) {
  const { t } = useTranslation()
  const count = (s: User['status']) => users.filter((u) => u.status === s).length
  const parts = [
    { key: 'active', value: count('active'), color: 'var(--color-success)' },
    { key: 'expired', value: count('expired'), color: 'var(--color-warning)' },
    { key: 'limited', value: count('limited'), color: 'var(--color-destructive)' },
    { key: 'disabled', value: count('disabled'), color: 'var(--color-muted-foreground)' },
  ]
  return (
    <Card>
      <h2 className="font-semibold">{t('dashboard.usersTitle')}</h2>
      <div className="mt-4 flex items-center gap-5">
        <Ring segments={parts} total={users.length}>
          <div className="text-2xl font-semibold tracking-tight">{users.length}</div>
          <div className="text-[11px] text-muted-foreground">{t('dashboard.usersTotal')}</div>
        </Ring>
        <ul className="space-y-1.5 text-sm">
          {parts.map((p) => (
            <li key={p.key} className="flex items-center gap-2 whitespace-nowrap">
              <span className="h-2.5 w-2.5 rounded-full" style={{ background: p.color }} />
              <span className="text-muted-foreground">{t(`users.status.${p.key}`)}</span>
              <span className="font-medium tabular-nums">{p.value}</span>
            </li>
          ))}
          <li className="flex items-center gap-2 whitespace-nowrap border-t pt-1.5">
            <span className="h-2.5 w-2.5 rounded-full border-2 border-success" />
            <span className="text-muted-foreground">{t('dashboard.online')}</span>
            <span className="font-medium tabular-nums">{online}</span>
          </li>
        </ul>
      </div>
    </Card>
  )
}

// Адрес сервера размыт, пока на него не нажали: главную часто показывают на экране.
function ServerIP({ label, ip }: { label: string; ip: string }) {
  const { t } = useTranslation()
  const [shown, setShown] = useState(false)
  return (
    <button
      type="button"
      onClick={() => setShown((v) => !v)}
      title={shown ? t('dashboard.ipHide') : t('dashboard.ipShow')}
      className="flex max-w-full items-center gap-2 rounded-full border bg-card px-3 py-1.5 text-sm transition-colors hover:bg-accent"
    >
      <span className="text-xs font-medium text-muted-foreground">{label}</span>
      <span className={'truncate font-mono transition-[filter] duration-200 ' + (shown ? '' : 'blur-[5px] select-none')}>{ip}</span>
    </button>
  )
}
