import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import { Boxes, Info, ScrollText, Shield } from 'lucide-react'
import { api, type AmneziaContainer } from '@/lib/api'
import { formatBytes, formatDuration } from '@/lib/utils'
import { Badge, Button, Card, Dialog, EmptyState, PageHeader } from '@/components/ui'

// Display names for the containers the Amnezia app creates.
const kindNames: Record<string, string> = {
  awg: 'AmneziaWG',
  awg2: 'AmneziaWG 2',
  wireguard: 'WireGuard',
  xray: 'XRay',
  ssxray: 'Shadowsocks (XRay)',
  openvpn: 'OpenVPN',
  'openvpn-cloak': 'OpenVPN + Cloak',
  shadowsocks: 'OpenVPN + Shadowsocks',
  ipsec: 'IPsec',
  dns: 'AmneziaDNS',
  socks5proxy: 'SOCKS5',
  'torwebsite': 'Tor-сайт',
  sftp: 'SFTP',
}

function ago(iso: string, lng: string, t: (k: string, o?: Record<string, unknown>) => string) {
  if (!iso || iso.startsWith('0001')) return t('amnezia.never')
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000)
  if (s < 180) return t('amnezia.justNow')
  return t('amnezia.ago', { time: formatDuration(s, lng) })
}

export function AmneziaPage() {
  const { t } = useTranslation()
  const q = useQuery({ queryKey: ['amnezia'], queryFn: api.amnezia.status, refetchInterval: 10_000 })
  const [logsOf, setLogsOf] = useState<string | null>(null)
  const st = q.data?.status

  return (
    <>
      <PageHeader title={t('amnezia.title')} subtitle={t('amnezia.subtitle')} />
      <div className="mb-4 flex items-start gap-3 rounded-2xl bg-accent px-4 py-3 text-sm text-accent-foreground">
        <Info className="mt-0.5 h-4 w-4 shrink-0" />
        <span>{t('amnezia.readOnly')}</span>
      </div>
      {q.isPending ? (
        <p className="text-sm text-muted-foreground">{t('common.loading')}</p>
      ) : q.isError ? (
        <p className="text-sm text-destructive">{t('common.error')}</p>
      ) : !q.data.helper ? (
        <Card>
          <EmptyState icon={<Shield className="h-6 w-6" />} title={t('amnezia.noHelperTitle')} text={t('amnezia.noHelperText')} />
        </Card>
      ) : !st?.docker ? (
        <Card>
          <EmptyState icon={<Boxes className="h-6 w-6" />} title={t('amnezia.noDockerTitle')} text={t('amnezia.noDockerText')} />
        </Card>
      ) : st.containers.length === 0 ? (
        <Card>
          <EmptyState icon={<Boxes className="h-6 w-6" />} title={t('amnezia.emptyTitle')} text={t('amnezia.emptyText')} />
        </Card>
      ) : (
        <div className="grid gap-4 xl:grid-cols-2">
          {st.containers.map((c) => (
            <ContainerCard key={c.name} c={c} onLogs={() => setLogsOf(c.name)} />
          ))}
        </div>
      )}
      {logsOf && <LogsDialog name={logsOf} onClose={() => setLogsOf(null)} />}
    </>
  )
}

function ContainerCard({ c, onLogs }: { c: AmneziaContainer; onLogs: () => void }) {
  const { t, i18n } = useTranslation()
  const lng = i18n.language
  const running = c.state === 'running'
  const uptime = running && c.startedAt ? (Date.now() - new Date(c.startedAt).getTime()) / 1000 : 0
  return (
    <Card>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="flex items-center gap-2">
            <h2 className="font-semibold">{kindNames[c.kind] ?? c.kind}</h2>
            <Badge tone={running ? 'success' : 'danger'}>{running ? t('amnezia.running') : c.state}</Badge>
          </div>
          <div className="mt-0.5 font-mono text-xs text-muted-foreground">{c.name}</div>
        </div>
        <Button variant="ghost" size="sm" onClick={onLogs}>
          <ScrollText className="h-4 w-4" />
          {t('amnezia.logs')}
        </Button>
      </div>
      <div className="mt-4 grid grid-cols-2 gap-3 text-sm sm:grid-cols-4">
        <Fact label={t('amnezia.ports')} value={c.ports.join(', ') || '—'} />
        <Fact label={t('amnezia.uptime')} value={running ? formatDuration(uptime, lng) : c.status} />
        <Fact label="CPU" value={running ? `${c.cpu.toFixed(1)}%` : '—'} />
        <Fact label={t('amnezia.memory')} value={running ? formatBytes(c.memBytes, lng) : '—'} />
      </div>
      <div className="mt-4">
        <div className="text-sm font-medium">{t('amnezia.clients', { count: c.clients.length })}</div>
        {c.clients.length === 0 ? (
          <p className="mt-1 text-xs text-muted-foreground">{running ? t('amnezia.noClients') : t('amnezia.stopped')}</p>
        ) : (
          <div className="mt-2 overflow-x-auto rounded-xl border">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-xs text-muted-foreground">
                  <th className="px-3 py-2 font-medium">{t('amnezia.clientName')}</th>
                  <th className="px-3 py-2 font-medium">{t('amnezia.lastSeen')}</th>
                  <th className="px-3 py-2 text-right font-medium">{t('amnezia.traffic')}</th>
                </tr>
              </thead>
              <tbody>
                {c.clients.map((cl) => {
                  const online = cl.lastHandshake && Date.now() - new Date(cl.lastHandshake).getTime() < 180_000
                  return (
                    <tr key={cl.id} className="border-t">
                      <td className="px-3 py-2">
                        <div className="flex items-center gap-2">
                          <span className={'h-2 w-2 shrink-0 rounded-full ' + (online ? 'bg-success' : 'border border-muted-foreground/50')} />
                          <span className="font-medium">{cl.name || t('amnezia.unnamed')}</span>
                        </div>
                        <div className="font-mono text-[11px] text-muted-foreground">
                          {cl.id}
                          {cl.endpoint && ` · ${cl.endpoint.replace(/:\d+$/, '')}`}
                        </div>
                      </td>
                      <td className="px-3 py-2 text-xs text-muted-foreground">{cl.lastHandshake !== undefined ? ago(cl.lastHandshake ?? '', lng, t) : '—'}</td>
                      <td className="px-3 py-2 text-right text-xs whitespace-nowrap tabular-nums">
                        ↓ {formatBytes(cl.txBytes, lng)} · ↑ {formatBytes(cl.rxBytes, lng)}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>
      <p className="mt-3 truncate text-[11px] text-muted-foreground" title={c.image}>
        {c.image}
      </p>
    </Card>
  )
}

function Fact({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-xl bg-muted/60 p-2.5">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-0.5 truncate font-medium">{value}</div>
    </div>
  )
}

function LogsDialog({ name, onClose }: { name: string; onClose: () => void }) {
  const { t } = useTranslation()
  const q = useQuery({ queryKey: ['amnezia', 'logs', name], queryFn: () => api.amnezia.logs(name) })
  return (
    <Dialog open onClose={onClose} title={t('amnezia.logsTitle', { name })}>
      {q.isPending ? (
        <p className="text-sm text-muted-foreground">{t('common.loading')}</p>
      ) : q.isError ? (
        <p className="text-sm text-destructive">{t('common.error')}</p>
      ) : q.data.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t('amnezia.noLogs')}</p>
      ) : (
        <pre className="max-h-[60vh] overflow-auto rounded-xl bg-muted p-3 text-[11px] leading-relaxed">{q.data.join('\n')}</pre>
      )}
      <div className="mt-4 flex justify-end">
        <Button variant="secondary" onClick={onClose}>
          OK
        </Button>
      </div>
    </Dialog>
  )
}
