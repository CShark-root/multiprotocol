import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CreditCard, Info, KeyRound, Pencil, Plus, RefreshCw, Server, Trash2 } from 'lucide-react'
import { ApiError, api, extras, type ServerBilling, type ServerNode, type SystemInfo } from '@/lib/api'
import { formatBitrate, formatBytes, formatDuration } from '@/lib/utils'
import { Badge, Button, Card, Dialog, EmptyState, Field, Input, PageHeader } from '@/components/ui'
import { CopyButton } from '@/components/share'

// Вкладка «Серверы»: этот сервер и другие наши серверы с агентом. Агент сам
// приходит к этой панели за настройками, поэтому на узле не нужен открытый
// служебный порт.
export function ServersPage() {
  const { t } = useTranslation()
  const nodes = useQuery({ queryKey: ['nodes'], queryFn: api.nodes.list, refetchInterval: 10_000 })
  const system = useQuery({ queryKey: ['system'], queryFn: api.system, refetchInterval: 10_000 })
  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<ServerNode | null>(null)
  const [removing, setRemoving] = useState<ServerNode | null>(null)
  const [command, setCommand] = useState<{ name: string; command: string } | null>(null)

  return (
    <>
      <PageHeader
        title={t('servers.title')}
        subtitle={t('servers.subtitle')}
        actions={
          <Button onClick={() => setAdding(true)}>
            <Plus className="h-4 w-4" />
            {t('servers.add')}
          </Button>
        }
      />
      <div className="mb-4 flex items-start gap-3 rounded-2xl bg-accent px-4 py-3 text-sm text-accent-foreground">
        <Info className="mt-0.5 h-4 w-4 shrink-0" />
        <span>{t('servers.info')}</span>
      </div>
      <div className="grid gap-4 xl:grid-cols-2">
        <ThisServer system={system.data} />
        {nodes.data?.map((n) => (
          <NodeCard
            key={n.id}
            n={n}
            onEdit={() => setEditing(n)}
            onRemove={() => setRemoving(n)}
            onToken={(command) => setCommand({ name: n.name, command })}
          />
        ))}
      </div>
      {nodes.data?.length === 0 && (
        <Card className="mt-4">
          <EmptyState icon={<Server className="h-6 w-6" />} title={t('servers.emptyTitle')} text={t('servers.emptyText')} />
        </Card>
      )}
      {adding && <NodeDialog onClose={() => setAdding(false)} onCreated={(name, cmd) => setCommand({ name, command: cmd })} />}
      {editing && <NodeDialog node={editing} onClose={() => setEditing(null)} />}
      {removing && <RemoveDialog n={removing} onClose={() => setRemoving(null)} />}
      {command && <CommandDialog name={command.name} command={command.command} onClose={() => setCommand(null)} />}
    </>
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

// Facts — нагрузка сервера, одинаково для этого и для узлов.
function Facts({ h }: { h: NonNullable<SystemInfo['localAgent']> }) {
  const { t, i18n } = useTranslation()
  const lng = i18n.language
  const s = h.system
  const mem = s.memTotal ? `${formatBytes(s.memTotal - s.memAvailable, lng)} / ${formatBytes(s.memTotal, lng)}` : '—'
  return (
    <>
      <div className="mt-4 grid grid-cols-2 gap-3 text-sm sm:grid-cols-4">
        <Fact label={t('servers.load')} value={s.cpus ? `${s.load1.toFixed(2)} · ${s.cpus} CPU` : '—'} />
        <Fact label={t('servers.memory')} value={mem} />
        <Fact label={t('servers.net')} value={s.netIface ? `↓ ${formatBitrate(s.netRxBps, lng)} ↑ ${formatBitrate(s.netTxBps, lng)}` : '—'} />
        <Fact label={t('servers.uptime')} value={s.uptimeSeconds ? formatDuration(s.uptimeSeconds, lng) : '—'} />
      </div>
      <div className="mt-3 flex flex-wrap gap-1.5">
        {h.cores
          .filter((c) => c.installed)
          .map((c) => (
            <Badge key={c.name} tone={c.running ? 'success' : 'muted'}>
              {c.name} {c.version}
            </Badge>
          ))}
      </div>
    </>
  )
}

function ThisServer({ system }: { system?: SystemInfo }) {
  const { t } = useTranslation()
  const h = system?.localAgent
  return (
    <Card>
      <div className="flex flex-wrap items-center gap-2">
        <h2 className="font-semibold">{t('servers.thisServer')}</h2>
        <Badge tone="accent">{t('servers.main')}</Badge>
      </div>
      <div className="mt-0.5 font-mono text-xs text-muted-foreground">
        {system?.publicHost}
        {h?.hostname && ` · ${h.hostname}`}
      </div>
      {h ? <Facts h={h} /> : <p className="mt-3 text-sm text-muted-foreground">{t('common.loading')}</p>}
      <MainBilling />
    </Card>
  )
}

function NodeCard({ n, onEdit, onRemove, onToken }: { n: ServerNode; onEdit: () => void; onRemove: () => void; onToken: (cmd: string) => void }) {
  const { t, i18n } = useTranslation()
  const qc = useQueryClient()
  const restart = useMutation({ mutationFn: () => api.nodes.restart(n.id) })
  const token = useMutation({
    mutationFn: () => api.nodes.token(n.id),
    onSuccess: (d) => {
      qc.invalidateQueries({ queryKey: ['nodes'] })
      onToken(d.command)
    },
  })
  const seen = n.lastSeen && !n.lastSeen.startsWith('0001') ? new Date(n.lastSeen) : null
  const status = !seen ? (
    <Badge tone="warning">{t('servers.waiting')}</Badge>
  ) : n.online ? (
    <Badge tone="success">{t('servers.online')}</Badge>
  ) : (
    <Badge tone="danger">{t('servers.offline')}</Badge>
  )
  return (
    <Card>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="font-semibold">{n.name}</h2>
            {status}
            {n.online && !n.inSync && <Badge tone="warning">{t('servers.applying')}</Badge>}
          </div>
          <div className="mt-0.5 font-mono text-xs text-muted-foreground">
            {n.host}
            {n.health?.hostname && ` · ${n.health.hostname}`}
            {n.agentVersion && ` · ${n.agentVersion}`}
          </div>
        </div>
        <div className="flex">
          <Button size="icon" variant="ghost" aria-label={t('servers.restart')} title={t('servers.restart')} disabled={!n.online || restart.isPending} onClick={() => restart.mutate()}>
            <RefreshCw className="h-4 w-4" />
          </Button>
          <Button size="icon" variant="ghost" aria-label={t('servers.newCommand')} title={t('servers.newCommand')} disabled={token.isPending} onClick={() => token.mutate()}>
            <KeyRound className="h-4 w-4" />
          </Button>
          <Button size="icon" variant="ghost" aria-label={t('servers.edit')} title={t('servers.edit')} onClick={onEdit}>
            <Pencil className="h-4 w-4" />
          </Button>
          <Button size="icon" variant="ghost" aria-label={t('servers.remove')} title={t('servers.remove')} onClick={onRemove}>
            <Trash2 className="h-4 w-4" />
          </Button>
        </div>
      </div>
      {n.applyError && <p className="mt-3 rounded-xl bg-destructive-soft px-3 py-2 text-xs text-destructive">{n.applyError}</p>}
      {n.health && n.online ? (
        <Facts h={n.health} />
      ) : (
        <p className="mt-3 text-sm text-muted-foreground">
          {seen ? t('servers.lastSeen', { time: seen.toLocaleString(i18n.language) }) : t('servers.waitingText')}
        </p>
      )}
      {n.online && <p className="mt-3 text-xs text-muted-foreground">{t('servers.usersNow', { count: n.users })}</p>}
      <BillingRow
        b={{ paidUntil: n.paidUntil, price: n.price }}
        onSave={async (date, price) => {
          await extras.saveNodeBilling(n.id, date, price)
          qc.invalidateQueries({ queryKey: ['nodes'] })
        }}
      />
    </Card>
  )
}

function NodeDialog({ node, onClose, onCreated }: { node?: ServerNode; onClose: () => void; onCreated?: (name: string, command: string) => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [name, setName] = useState(node?.name ?? '')
  const [host, setHost] = useState(node?.host ?? '')
  const m = useMutation({
    mutationFn: async () => {
      if (node) {
        await api.nodes.update(node.id, name, host)
        return null
      }
      return api.nodes.create(name, host)
    },
    onSuccess: (d) => {
      qc.invalidateQueries({ queryKey: ['nodes'] })
      onClose()
      if (d && onCreated) onCreated(d.node.name, d.command)
    },
  })
  const err = m.error instanceof ApiError ? (m.error.field ? `${m.error.field}: ${m.error.message}` : m.error.message) : m.error ? t('common.error') : ''
  return (
    <Dialog open onClose={onClose} title={node ? t('servers.edit') : t('servers.addTitle')}>
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault()
          m.mutate()
        }}
      >
        <Field label={t('servers.name')} hint={t('servers.nameHint')}>
          {(id) => <Input id={id} value={name} onChange={(e) => setName(e.target.value)} maxLength={64} autoFocus required />}
        </Field>
        <Field label={t('servers.host')} hint={t('servers.hostHint')}>
          {(id) => <Input id={id} value={host} onChange={(e) => setHost(e.target.value)} maxLength={253} placeholder="203.0.113.7" required />}
        </Field>
        {err && <p className="text-sm text-destructive">{err}</p>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" disabled={m.isPending}>
            {node ? t('common.save') : t('servers.add')}
          </Button>
        </div>
      </form>
    </Dialog>
  )
}

function CommandDialog({ name, command, onClose }: { name: string; command: string; onClose: () => void }) {
  const { t } = useTranslation()
  return (
    <Dialog open onClose={onClose} title={t('servers.commandTitle', { name })}>
      <div className="space-y-3">
        <p className="text-sm">{t('servers.commandText')}</p>
        <div className="flex items-start gap-2 rounded-xl bg-muted p-3">
          <code className="min-w-0 flex-1 break-all font-mono text-[11px] leading-relaxed">{command}</code>
          <CopyButton text={command} />
        </div>
        <p className="rounded-xl bg-warning-soft px-3.5 py-2.5 text-xs text-warning">{t('servers.commandSecret')}</p>
        <div className="flex justify-end">
          <Button onClick={onClose}>OK</Button>
        </div>
      </div>
    </Dialog>
  )
}

function RemoveDialog({ n, onClose }: { n: ServerNode; onClose: () => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const m = useMutation({
    mutationFn: () => api.nodes.remove(n.id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['nodes'] })
      qc.invalidateQueries({ queryKey: ['inbounds'] })
      onClose()
    },
  })
  return (
    <Dialog open onClose={onClose} title={t('servers.removeTitle', { name: n.name })}>
      <div className="space-y-4">
        <p className="text-sm">{t('servers.removeText')}</p>
        <div className="flex justify-end gap-2">
          <Button variant="secondary" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button variant="danger" disabled={m.isPending} onClick={() => m.mutate()}>
            {t('servers.remove')}
          </Button>
        </div>
      </div>
    </Dialog>
  )
}

function MainBilling() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['billing', 'main'], queryFn: extras.mainBilling })
  if (!q.data) return null
  return (
    <BillingRow
      b={q.data}
      onSave={async (date, price) => {
        qc.setQueryData(['billing', 'main'], await extras.saveMainBilling(date, price))
      }}
    />
  )
}

// BillingRow — до какого дня оплачен VPS; бот напомнит заранее.
function BillingRow({ b, onSave }: { b: ServerBilling; onSave: (date: string, price: string) => Promise<void> }) {
  const { t, i18n } = useTranslation()
  const [editing, setEditing] = useState(false)
  const [date, setDate] = useState('')
  const [price, setPrice] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const paid = b.paidUntil ? new Date(b.paidUntil) : null
  const days = paid ? Math.round((new Date(paid.toDateString()).getTime() - new Date(new Date().toDateString()).getTime()) / 86_400_000) : null
  const shown = paid?.toLocaleDateString(i18n.language) ?? ''
  const tone = days === null ? 'text-muted-foreground' : days < 0 ? 'text-destructive' : days <= 3 ? 'text-warning' : 'text-muted-foreground'
  const label = days === null ? t('billing.none') : days < 0 ? t('billing.overdue', { date: shown }) : days <= 3 ? t('billing.soon', { date: shown }) : t('billing.left', { date: shown })
  const start = () => {
    setDate(paid ? `${paid.getFullYear()}-${String(paid.getMonth() + 1).padStart(2, '0')}-${String(paid.getDate()).padStart(2, '0')}` : '')
    setPrice(b.price)
    setError('')
    setEditing(true)
  }
  if (!editing)
    return (
      <button type="button" onClick={start} className={'mt-3 flex cursor-pointer items-center gap-1.5 text-xs hover:underline ' + tone}>
        <CreditCard className="h-3.5 w-3.5" />
        {label}
        {b.price && ` · ${b.price}`}
      </button>
    )
  return (
    <form
      className="mt-3 space-y-2 rounded-xl bg-muted/60 p-3"
      onSubmit={async (e) => {
        e.preventDefault()
        setBusy(true)
        try {
          await onSave(date, price)
          setEditing(false)
        } catch (err) {
          setError(err instanceof ApiError ? err.message : t('common.error'))
        } finally {
          setBusy(false)
        }
      }}
    >
      <div className="grid gap-2 sm:grid-cols-2">
        <Field label={t('billing.paidUntil')}>{(id) => <Input id={id} type="date" value={date} onChange={(e) => setDate(e.target.value)} />}</Field>
        <Field label={t('billing.price')}>
          {(id) => <Input id={id} value={price} maxLength={40} placeholder={t('billing.pricePlaceholder')} onChange={(e) => setPrice(e.target.value)} />}
        </Field>
      </div>
      <p className="text-xs text-muted-foreground">{t('billing.hint')}</p>
      {error && <p className="text-xs text-destructive">{error}</p>}
      <div className="flex justify-end gap-2">
        <Button type="button" size="sm" variant="secondary" onClick={() => setEditing(false)}>
          {t('common.cancel')}
        </Button>
        <Button type="submit" size="sm" disabled={busy}>
          {t('common.save')}
        </Button>
      </div>
    </form>
  )
}
