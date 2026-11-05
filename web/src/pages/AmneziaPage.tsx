import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Boxes, Download, Info, Pencil, Plus, ScrollText, Shield, Trash2 } from 'lucide-react'
import { ApiError, api, type AmneziaClient, type AmneziaContainer } from '@/lib/api'
import { formatBytes, formatDuration } from '@/lib/utils'
import { Badge, Button, Card, Dialog, EmptyState, Field, Input, PageHeader, Select } from '@/components/ui'
import { ShareRow } from '@/components/share'

// Display names for the containers the Amnezia app creates.
const kindNames: Record<string, string> = {
  awg: 'AmneziaWG',
  // The container is named awg2 by the app whatever AmneziaWG version runs
  // inside, so no number is shown.
  awg2: 'AmneziaWG',
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
        <span>{t('amnezia.managedInfo')}</span>
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
  const [adding, setAdding] = useState(false)
  const [action, setAction] = useState<{ client: AmneziaClient; mode: "rename" | "remove" } | null>(null)
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
        <div className="flex items-center justify-between gap-2">
          <div className="text-sm font-medium">{t('amnezia.clients', { count: c.clients.length })}</div>
          {c.managed && running && (
            <Button size="sm" variant="secondary" onClick={() => setAdding(true)}>
              <Plus className="h-4 w-4" />
              {t('amnezia.add')}
            </Button>
          )}
        </div>
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
                  {c.managed && <th className="w-px px-1 py-2" />}
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
                      {c.managed && (
                        <td className="px-1 py-2">
                          <div className="flex justify-end">
                            <Button size="icon" variant="ghost" aria-label={t('amnezia.renameTitle')} onClick={() => setAction({ client: cl, mode: 'rename' })}>
                              <Pencil className="h-4 w-4" />
                            </Button>
                            <Button size="icon" variant="ghost" aria-label={t('amnezia.remove')} onClick={() => setAction({ client: cl, mode: 'remove' })}>
                              <Trash2 className="h-4 w-4" />
                            </Button>
                          </div>
                        </td>
                      )}
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
      {adding && <AddClientDialog container={c.name} onClose={() => setAdding(false)} />}
      {action && <ClientActionDialog container={c.name} client={action.client} mode={action.mode} onClose={() => setAction(null)} />}
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

function AddClientDialog({ container, onClose }: { container: string; onClose: () => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const users = useQuery({ queryKey: ['users'], queryFn: api.users.list })
  const [name, setName] = useState('')
  const [userId, setUserId] = useState(0)
  const add = useMutation({
    mutationFn: () => api.amnezia.addClient(container, name, userId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['amnezia'] })
      qc.invalidateQueries({ queryKey: ['users'] })
    },
  })
  const err = add.error instanceof ApiError ? add.error.message : add.error ? t('common.error') : ''
  return (
    <Dialog open onClose={onClose} title={t('amnezia.addTitle')}>
      {add.data ? (
        <div className="space-y-3">
          <p className="text-sm">{t('amnezia.added', { ip: add.data.ip })}</p>
          <ShareRow label={t('amnezia.configLabel')} hint={t('amnezia.configHint')} text={add.data.config} />
          <Button
            variant="secondary"
            onClick={() => {
              const a = document.createElement('a')
              a.href = URL.createObjectURL(new Blob([add.data.config], { type: 'text/plain' }))
              a.download = (name.replace(/[^\p{L}\p{N}_-]+/gu, '_') || 'amnezia') + '.conf'
              a.click()
              URL.revokeObjectURL(a.href)
            }}
          >
            <Download className="h-4 w-4" />
            {t('amnezia.download')}
          </Button>
          <p className="rounded-xl bg-warning-soft px-3.5 py-2.5 text-xs text-warning">
            {add.data.savedToUser ? t('amnezia.savedToUser') : t('amnezia.onlyOnce')}
          </p>
          <div className="flex justify-end">
            <Button onClick={onClose}>OK</Button>
          </div>
        </div>
      ) : (
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault()
            add.mutate()
          }}
        >
          <Field label={t('amnezia.clientName')} hint={t('amnezia.nameHint')}>
            {(id) => <Input id={id} value={name} onChange={(e) => setName(e.target.value)} maxLength={64} autoFocus required />}
          </Field>
          <Field label={t('amnezia.linkUser')} hint={t('amnezia.linkUserHint')}>
            {(id) => (
              <Select id={id} value={userId} onChange={(e) => setUserId(Number(e.target.value))}>
                <option value={0}>{t('amnezia.noUser')}</option>
                {users.data?.map((u) => (
                  <option key={u.id} value={u.id}>
                    {u.name}
                  </option>
                ))}
              </Select>
            )}
          </Field>
          <p className="text-xs text-muted-foreground">{t('amnezia.safety')}</p>
          {err && <p className="text-sm text-destructive">{err}</p>}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="secondary" onClick={onClose}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={add.isPending}>
              {add.isPending ? t('amnezia.working') : t('amnezia.add')}
            </Button>
          </div>
        </form>
      )}
    </Dialog>
  )
}

function ClientActionDialog({ container, client, mode, onClose }: { container: string; client: AmneziaClient; mode: 'rename' | 'remove'; onClose: () => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [name, setName] = useState(client.name)
  const m = useMutation({
    mutationFn: () => (mode === 'remove' ? api.amnezia.removeClient(container, client.key) : api.amnezia.renameClient(container, client.key, name)),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['amnezia'] })
      onClose()
    },
  })
  const err = m.error instanceof ApiError ? m.error.message : m.error ? t('common.error') : ''
  const label = client.name || client.id
  return (
    <Dialog open onClose={onClose} title={mode === 'remove' ? t('amnezia.removeTitle', { name: label }) : t('amnezia.renameTitle')}>
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault()
          m.mutate()
        }}
      >
        {mode === 'remove' ? (
          <p className="text-sm">{t('amnezia.removeText')}</p>
        ) : (
          <Field label={t('amnezia.clientName')}>
            {(id) => <Input id={id} value={name} onChange={(e) => setName(e.target.value)} maxLength={64} autoFocus required />}
          </Field>
        )}
        {err && <p className="text-sm text-destructive">{err}</p>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" variant={mode === 'remove' ? 'danger' : 'primary'} disabled={m.isPending}>
            {m.isPending ? t('amnezia.working') : mode === 'remove' ? t('amnezia.remove') : t('common.save')}
          </Button>
        </div>
      </form>
    </Dialog>
  )
}
