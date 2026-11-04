import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, Cable, CheckCircle2, ChevronDown, Pencil, Plus, Trash2 } from 'lucide-react'
import { ApiError, api, protocolOf, type Inbound, type InboundInput, type Transport } from '@/lib/api'
import { cn } from '@/lib/utils'
import { Badge, Button, Card, Dialog, EmptyState, Field, Input, PageHeader, Select, Switch } from '@/components/ui'

const kinds: Transport[] = ['xhttp', 'tcp', 'grpc', 'quic']

// Which core serves which protocol, and the install script for it.
const cores = {
  vless: { name: 'xray', title: 'Xray', script: 'xray' },
  hysteria2: { name: 'hysteria', title: 'Hysteria', script: 'hysteria' },
} as const

function CoreBanners({ inbounds }: { inbounds: Inbound[] }) {
  const { t } = useTranslation()
  const system = useQuery({ queryKey: ['system'], queryFn: api.system, refetchInterval: 5_000 })
  if (!system.data) return null
  if (!system.data.localAgent) return <Banner tone="warning" title={t('inbounds.banner.noAgent')} />

  const used = [...new Set(inbounds.filter((i) => i.enabled).map((i) => i.protocol))]
  return (
    <>
      {used.map((p) => {
        const c = cores[p]
        const h = system.data.localAgent?.cores.find((x) => x.name === c.name)
        if (!h) return null
        if (!h.installed) {
          return (
            <Banner
              key={p}
              tone="warning"
              title={t('inbounds.banner.notInstalled', { core: c.title })}
              text={t('inbounds.banner.notInstalledHint', { script: c.script })}
            />
          )
        }
        if (h.message) return <Banner key={p} tone="danger" title={t('inbounds.banner.error', { core: c.title })} text={h.message} />
        if (h.running) return <Banner key={p} tone="success" title={t('inbounds.banner.running', { core: c.title, version: h.version })} />
        return null
      })}
      {system.data.sync?.lastError && !used.length && (
        <Banner tone="danger" title={t('common.error')} text={system.data.sync.lastError} />
      )}
    </>
  )
}

function Banner({ tone, title, text }: { tone: 'success' | 'warning' | 'danger'; title: string; text?: string }) {
  const styles = {
    success: 'bg-success-soft text-success',
    warning: 'bg-warning-soft text-warning',
    danger: 'bg-destructive-soft text-destructive',
  }
  const Icon = tone === 'success' ? CheckCircle2 : AlertTriangle
  return (
    <div className={cn('mb-3 flex gap-3 rounded-2xl px-4 py-3 text-sm', styles[tone])}>
      <Icon className="mt-0.5 h-4 w-4 shrink-0" />
      <div className="min-w-0">
        <div className="font-medium">{title}</div>
        {text && <div className="mt-0.5 break-words font-mono text-xs opacity-90">{text}</div>}
      </div>
    </div>
  )
}

export function InboundsPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const inbounds = useQuery({ queryKey: ['inbounds'], queryFn: api.inbounds.list })
  const [editing, setEditing] = useState<Inbound | 'new' | null>(null)
  const [deleting, setDeleting] = useState<Inbound | null>(null)

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ['inbounds'] })
    setTimeout(() => qc.invalidateQueries({ queryKey: ['system'] }), 1500)
  }
  const toggle = useMutation({
    mutationFn: (ib: Inbound) => api.inbounds.update(ib.id, { ...toInput(ib), enabled: !ib.enabled }),
    onSuccess: invalidate,
  })
  const remove = useMutation({
    mutationFn: (ib: Inbound) => api.inbounds.remove(ib.id),
    onSuccess: () => {
      setDeleting(null)
      invalidate()
    },
  })

  const addButton = (
    <Button onClick={() => setEditing('new')}>
      <Plus className="h-4 w-4" />
      {t('inbounds.add')}
    </Button>
  )

  return (
    <>
      <PageHeader title={t('inbounds.title')} subtitle={t('inbounds.subtitle')} actions={inbounds.data?.length ? addButton : undefined} />
      {inbounds.data && <CoreBanners inbounds={inbounds.data} />}

      {inbounds.isPending ? (
        <p className="text-sm text-muted-foreground">{t('common.loading')}</p>
      ) : inbounds.isError ? (
        <p className="text-sm text-destructive">{t('common.error')}</p>
      ) : inbounds.data.length === 0 ? (
        <Card>
          <EmptyState icon={<Cable className="h-6 w-6" />} title={t('inbounds.emptyTitle')} text={t('inbounds.emptyText')} action={addButton} />
        </Card>
      ) : (
        <div className="mt-1 grid gap-4 md:grid-cols-2">
          {inbounds.data.map((ib) => (
            <Card key={ib.id} className={cn(!ib.enabled && 'opacity-70')}>
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="truncate font-semibold">{ib.remark}</div>
                  <div className="mt-1.5 flex flex-wrap gap-1.5">
                    <Badge tone="accent">{t(`inbounds.transports.${ib.transport}.title`)}</Badge>
                    <Badge tone="muted">{t('inbounds.portLabel', { network: ib.network.toUpperCase(), port: ib.port })}</Badge>
                    {ib.obfs && <Badge tone="muted">salamander</Badge>}
                  </div>
                </div>
                <Switch checked={ib.enabled} label={t('inbounds.enabled')} disabled={toggle.isPending} onChange={() => toggle.mutate(ib)} />
              </div>
              <dl className="mt-4 space-y-1.5 text-sm">
                <div className="flex justify-between gap-4">
                  <dt className="text-muted-foreground">{t(ib.protocol === 'hysteria2' ? 'inbounds.masquerade' : 'inbounds.target')}</dt>
                  <dd className="truncate">{ib.protocol === 'hysteria2' ? ib.masquerade?.replace(/^https:\/\/|\/$/g, '') : ib.serverName}</dd>
                </div>
                {ib.address && (
                  <div className="flex justify-between gap-4">
                    <dt className="text-muted-foreground">{t('inbounds.address')}</dt>
                    <dd className="truncate">{ib.address}</dd>
                  </div>
                )}
              </dl>
              <div className="mt-4 flex justify-end gap-1">
                <Button variant="ghost" size="sm" onClick={() => setEditing(ib)}>
                  <Pencil className="h-4 w-4" />
                  {t('common.edit')}
                </Button>
                <Button variant="ghost" size="sm" onClick={() => setDeleting(ib)}>
                  <Trash2 className="h-4 w-4" />
                  {t('common.delete')}
                </Button>
              </div>
            </Card>
          ))}
        </div>
      )}

      {editing && <InboundDialog inbound={editing === 'new' ? null : editing} onClose={() => setEditing(null)} onSaved={invalidate} />}

      <Dialog
        open={deleting !== null}
        onClose={() => setDeleting(null)}
        title={t('inbounds.deleteTitle', { name: deleting?.remark ?? '' })}
        description={t('inbounds.deleteText')}
      >
        <div className="flex justify-end gap-2">
          <Button variant="secondary" onClick={() => setDeleting(null)}>
            {t('common.cancel')}
          </Button>
          <Button variant="danger" disabled={remove.isPending} onClick={() => deleting && remove.mutate(deleting)}>
            {t('common.delete')}
          </Button>
        </div>
      </Dialog>
    </>
  )
}

function toInput(ib: Inbound): InboundInput {
  return {
    remark: ib.remark,
    protocol: ib.protocol,
    transport: ib.transport,
    port: ib.port,
    address: ib.address,
    target: ib.target ?? '',
    serverName: ib.serverName,
    fingerprint: ib.fingerprint ?? 'chrome',
    xhttpPath: ib.xhttpPath ?? '',
    xhttpMode: ib.xhttpMode ?? 'auto',
    grpcServiceName: ib.grpcServiceName ?? '',
    obfs: ib.obfs,
    masquerade: ib.masquerade ?? '',
    upMbps: ib.upMbps ?? 0,
    downMbps: ib.downMbps ?? 0,
    enabled: ib.enabled,
  }
}

const emptyInput: InboundInput = {
  remark: '',
  protocol: 'vless',
  transport: 'xhttp',
  port: 0,
  address: '',
  target: 'www.microsoft.com',
  serverName: '',
  fingerprint: 'chrome',
  xhttpPath: '',
  xhttpMode: 'auto',
  grpcServiceName: '',
  obfs: false,
  masquerade: 'https://www.bing.com/',
  upMbps: 0,
  downMbps: 0,
  enabled: true,
}

function InboundDialog({ inbound, onClose, onSaved }: { inbound: Inbound | null; onClose: () => void; onSaved: () => void }) {
  const { t } = useTranslation()
  const [form, setForm] = useState<InboundInput>(inbound ? toInput(inbound) : emptyInput)
  const isHy = form.protocol === 'hysteria2'
  const network = isHy ? 'udp' : 'tcp'
  const suggest = useQuery({ queryKey: ['inbounds', 'suggest', network], queryFn: () => api.inbounds.suggest(network), enabled: !inbound, staleTime: 0 })
  const [advanced, setAdvanced] = useState(false)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const set = <K extends keyof InboundInput>(k: K, v: InboundInput[K]) => setForm((f) => ({ ...f, [k]: v }))
  const setKind = (k: Transport) => setForm((f) => ({ ...f, transport: k, protocol: protocolOf(k), port: 0, serverName: '' }))

  const port = form.port || suggest.data?.port || 443
  const save = useMutation({
    mutationFn: (input: InboundInput) => (inbound ? api.inbounds.update(inbound.id, input) : api.inbounds.create(input)),
    onSuccess: () => {
      onSaved()
      onClose()
    },
    onError: (err) => {
      if (err instanceof ApiError && err.field) {
        setErrors({ [err.field]: err.message })
        if (!['remark', 'port', 'target', 'masquerade', 'transport', 'protocol'].includes(err.field)) setAdvanced(true)
      } else setErrors({ form: t('common.error') })
    },
  })

  function submit(e: FormEvent) {
    e.preventDefault()
    setErrors({})
    const remark = form.remark.trim() || t(`inbounds.transports.${form.transport}.title`)
    save.mutate({ ...form, remark, port })
  }

  const num = (v: string) => Number(v.replace(/\D/g, '')) || 0

  return (
    <Dialog open onClose={onClose} title={inbound ? t('inbounds.editTitle', { name: inbound.remark }) : t('inbounds.createTitle')}>
      <form onSubmit={submit} className="max-h-[75vh] space-y-4 overflow-y-auto pr-1">
        {!inbound && (
          <div className="space-y-2">
            <div className="text-sm font-medium">{t('inbounds.transport')}</div>
            <div className="grid gap-2">
              {kinds.map((k) => (
                <button
                  key={k}
                  type="button"
                  onClick={() => setKind(k)}
                  className={cn(
                    'cursor-pointer rounded-xl border p-3 text-left transition',
                    form.transport === k ? 'border-primary bg-accent/60 ring-2 ring-primary/20' : 'hover:bg-muted',
                  )}
                >
                  <div className="flex items-center gap-2 text-sm font-medium">
                    {t(`inbounds.transports.${k}.title`)}
                    {k === 'xhttp' && <Badge tone="success">{t('inbounds.recommended')}</Badge>}
                    {k === 'quic' && <Badge tone="muted">UDP</Badge>}
                  </div>
                  <div className="mt-0.5 text-xs text-muted-foreground">{t(`inbounds.transports.${k}.text`)}</div>
                </button>
              ))}
            </div>
          </div>
        )}

        <div className="grid gap-4 sm:grid-cols-[1fr_8rem]">
          <Field label={t('inbounds.remark')} error={errors.remark}>
            {(id) => (
              <Input id={id} value={form.remark} onChange={(e) => set('remark', e.target.value)} placeholder={t('inbounds.remarkPlaceholder')} maxLength={64} />
            )}
          </Field>
          <Field label={t('inbounds.port')} error={errors.port}>
            {(id) => <Input id={id} inputMode="numeric" value={String(port)} onChange={(e) => set('port', num(e.target.value))} />}
          </Field>
        </div>
        {!errors.port && <p className="-mt-2 text-xs text-muted-foreground">{t(isHy ? 'inbounds.udpPortHint' : 'inbounds.portHint')}</p>}

        {isHy ? (
          <>
            <Field label={t('inbounds.masquerade')} hint={t('inbounds.masqueradeHint')} error={errors.masquerade}>
              {(id) => (
                <>
                  <Input id={id} list="masquerade-sites" value={form.masquerade} onChange={(e) => set('masquerade', e.target.value)} required />
                  <datalist id="masquerade-sites">
                    {suggest.data?.masquerades.map((s) => <option key={s} value={s} />)}
                  </datalist>
                </>
              )}
            </Field>
            <div className="flex items-start justify-between gap-4 rounded-xl border p-3.5">
              <div>
                <div className="text-sm font-medium">{t('inbounds.obfs')}</div>
                <div className="text-xs text-muted-foreground">{t('inbounds.obfsHint')}</div>
              </div>
              <Switch checked={form.obfs} onChange={(v) => set('obfs', v)} label={t('inbounds.obfs')} />
            </div>
          </>
        ) : (
          <Field label={t('inbounds.target')} hint={t('inbounds.targetHint')} error={errors.target}>
            {(id) => (
              <>
                <Input id={id} list="reality-targets" value={form.target} onChange={(e) => set('target', e.target.value)} required />
                <datalist id="reality-targets">
                  {suggest.data?.targets.map((s) => <option key={s} value={s} />)}
                </datalist>
              </>
            )}
          </Field>
        )}

        <button
          type="button"
          onClick={() => setAdvanced(!advanced)}
          className="flex cursor-pointer items-center gap-1.5 text-sm font-medium text-muted-foreground hover:text-foreground"
        >
          <ChevronDown className={cn('h-4 w-4 transition', advanced && 'rotate-180')} />
          {t('inbounds.advanced')}
        </button>

        {advanced && (
          <div className="space-y-4 rounded-xl bg-muted/50 p-4">
            <Field label={t('inbounds.address')} hint={t('inbounds.addressHint', { host: suggest.data?.publicHost ?? '…' })} error={errors.address}>
              {(id) => <Input id={id} value={form.address} onChange={(e) => set('address', e.target.value)} />}
            </Field>
            {isHy ? (
              <>
                <Field label={t('inbounds.hySni')} hint={t('inbounds.hySniHint')} error={errors.serverName}>
                  {(id) => <Input id={id} value={form.serverName} onChange={(e) => set('serverName', e.target.value)} />}
                </Field>
                <div className="grid gap-4 sm:grid-cols-2">
                  <Field label={t('inbounds.upMbps')} error={errors.upMbps}>
                    {(id) => <Input id={id} inputMode="numeric" value={String(form.upMbps)} onChange={(e) => set('upMbps', num(e.target.value))} />}
                  </Field>
                  <Field label={t('inbounds.downMbps')} error={errors.downMbps}>
                    {(id) => <Input id={id} inputMode="numeric" value={String(form.downMbps)} onChange={(e) => set('downMbps', num(e.target.value))} />}
                  </Field>
                </div>
                <p className="-mt-2 text-xs text-muted-foreground">{t('inbounds.bandwidthHint')}</p>
              </>
            ) : (
              <>
                <div className="grid gap-4 sm:grid-cols-2">
                  <Field label={t('inbounds.serverName')} hint={t('inbounds.serverNameHint')} error={errors.serverName}>
                    {(id) => <Input id={id} value={form.serverName} onChange={(e) => set('serverName', e.target.value)} />}
                  </Field>
                  <Field label={t('inbounds.fingerprint')} hint={t('inbounds.fingerprintHint')} error={errors.fingerprint}>
                    {(id) => (
                      <Select id={id} value={form.fingerprint} onChange={(e) => set('fingerprint', e.target.value)}>
                        {['chrome', 'firefox', 'safari', 'ios', 'edge', 'random'].map((f) => (
                          <option key={f} value={f}>
                            {f}
                          </option>
                        ))}
                      </Select>
                    )}
                  </Field>
                </div>
                {form.transport === 'xhttp' && (
                  <div className="grid gap-4 sm:grid-cols-2">
                    <Field label={t('inbounds.xhttpPath')} hint={t('inbounds.xhttpPathHint')} error={errors.xhttpPath}>
                      {(id) => <Input id={id} value={form.xhttpPath} onChange={(e) => set('xhttpPath', e.target.value)} placeholder="/…" />}
                    </Field>
                    <Field label={t('inbounds.xhttpMode')} hint={t('inbounds.xhttpModeHint')} error={errors.xhttpMode}>
                      {(id) => (
                        <Select id={id} value={form.xhttpMode} onChange={(e) => set('xhttpMode', e.target.value)}>
                          {['auto', 'packet-up', 'stream-up', 'stream-one'].map((m) => (
                            <option key={m} value={m}>
                              {m}
                            </option>
                          ))}
                        </Select>
                      )}
                    </Field>
                  </div>
                )}
                {form.transport === 'grpc' && (
                  <Field label={t('inbounds.grpcServiceName')} hint={t('inbounds.grpcServiceNameHint')} error={errors.grpcServiceName}>
                    {(id) => <Input id={id} value={form.grpcServiceName} onChange={(e) => set('grpcServiceName', e.target.value)} />}
                  </Field>
                )}
                <p className="text-xs text-muted-foreground">{t('inbounds.keysNote')}</p>
              </>
            )}
          </div>
        )}

        <div className="flex items-start justify-between gap-4 rounded-xl bg-muted/60 p-3.5">
          <div>
            <div className="text-sm font-medium">{t('inbounds.enabled')}</div>
            <div className="text-xs text-muted-foreground">{t('inbounds.enabledHint')}</div>
          </div>
          <Switch checked={form.enabled} onChange={(v) => set('enabled', v)} label={t('inbounds.enabled')} />
        </div>

        {errors.form && <p className="text-sm text-destructive">{errors.form}</p>}
        {errors.protocol && <p className="text-sm text-destructive">{errors.protocol}</p>}
        <div className="flex justify-end gap-2 pt-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" disabled={save.isPending}>
            {t('common.save')}
          </Button>
        </div>
      </form>
    </Dialog>
  )
}
