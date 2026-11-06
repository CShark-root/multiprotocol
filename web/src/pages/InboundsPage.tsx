import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, Cable, CheckCircle2, ChevronDown, FlaskConical, Loader2, Pencil, Plus, Sparkles, Trash2 } from 'lucide-react'
import { ApiError, api, protocolOrder, protocolSpecs, type Inbound, type InboundInput, type Protocol, type Transport } from '@/lib/api'
import { cn } from '@/lib/utils'
import { Badge, Button, Card, Dialog, EmptyState, Field, Input, PageHeader, Select, Switch } from '@/components/ui'
import { ExpertSection } from '@/components/ExpertSection'

// Install scripts per core, for the "not installed" banner.
const cores = {
  xray: { title: 'Xray', script: 'xray' },
  hysteria: { title: 'Hysteria', script: 'hysteria' },
  singbox: { title: 'sing-box', script: 'singbox' },
} as const

const realityProtocols: Protocol[] = ['vless', 'vless-sb', 'trojan']
const certProtocols: Protocol[] = ['tuic', 'anytls']

function CoreBanners({ inbounds }: { inbounds: Inbound[] }) {
  const { t } = useTranslation()
  const system = useQuery({ queryKey: ['system'], queryFn: api.system, refetchInterval: 5_000 })
  if (!system.data) return null
  if (!system.data.localAgent) return <Banner tone="warning" title={t('inbounds.banner.noAgent')} />

  const used = [...new Set(inbounds.filter((i) => i.enabled).map((i) => i.core))]
  return (
    <>
      {used.map((name) => {
        const c = cores[name]
        const h = system.data.localAgent?.cores.find((x) => x.name === name)
        if (!h) return null
        if (!h.installed) {
          return (
            <Banner
              key={name}
              tone="warning"
              title={t('inbounds.banner.notInstalled', { core: c.title })}
              text={t('inbounds.banner.notInstalledHint', { script: c.script })}
            />
          )
        }
        if (h.message) return <Banner key={name} tone="danger" title={t('inbounds.banner.error', { core: c.title })} text={h.message} />
        if (h.running) return <Banner key={name} tone="success" title={t('inbounds.banner.running', { core: c.title, version: h.version })} />
        return null
      })}
      {system.data.sync?.lastError && !used.length && <Banner tone="danger" title={t('common.error')} text={system.data.sync.lastError} />}
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

/** "VLESS + Reality · XHTTP", transport only when the protocol has several. */
export function useKindLabel() {
  const { t } = useTranslation()
  return (p: Protocol, tr: Transport) =>
    protocolSpecs[p].transports.length > 1 ? `${t(`inbounds.protocols.${p}.title`)} · ${t(`inbounds.transportNames.${tr}`)}` : t(`inbounds.protocols.${p}.title`)
}

export function InboundsPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const kindLabel = useKindLabel()
  const inbounds = useQuery({ queryKey: ['inbounds'], queryFn: api.inbounds.list })
  const [editing, setEditing] = useState<Inbound | 'new' | null>(null)
  const [deleting, setDeleting] = useState<Inbound | null>(null)
  const [removingKit, setRemovingKit] = useState(false)

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ['inbounds'] })
    qc.invalidateQueries({ queryKey: ['users'] })
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
  const createKit = useMutation({ mutationFn: api.inbounds.createTestKit, onSuccess: invalidate })
  const deleteKit = useMutation({
    mutationFn: api.inbounds.deleteTestKit,
    onSuccess: () => {
      setRemovingKit(false)
      invalidate()
    },
  })

  const hasKit = inbounds.data?.some((i) => i.testKit) ?? false
  const actions = (
    <div className="flex flex-wrap gap-2">
      {hasKit ? (
        <Button variant="secondary" onClick={() => setRemovingKit(true)}>
          <Trash2 className="h-4 w-4" />
          {t('inbounds.testKitRemove')}
        </Button>
      ) : (
        <Button variant="secondary" title={t('inbounds.testKitHint')} disabled={createKit.isPending} onClick={() => createKit.mutate()}>
          <FlaskConical className="h-4 w-4" />
          {t('inbounds.testKit')}
        </Button>
      )}
      <Button onClick={() => setEditing('new')}>
        <Plus className="h-4 w-4" />
        {t('inbounds.add')}
      </Button>
    </div>
  )

  return (
    <>
      <PageHeader title={t('inbounds.title')} subtitle={t('inbounds.subtitle')} actions={inbounds.data?.length ? actions : undefined} />
      {inbounds.data && <CoreBanners inbounds={inbounds.data} />}
      {createKit.isError && <Banner tone="danger" title={t('common.error')} text={(createKit.error as Error).message} />}

      {inbounds.isPending ? (
        <p className="text-sm text-muted-foreground">{t('common.loading')}</p>
      ) : inbounds.isError ? (
        <p className="text-sm text-destructive">{t('common.error')}</p>
      ) : inbounds.data.length === 0 ? (
        <Card>
          <EmptyState icon={<Cable className="h-6 w-6" />} title={t('inbounds.emptyTitle')} text={t('inbounds.emptyText')} action={actions} />
        </Card>
      ) : (
        <div className="mt-1 grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {inbounds.data.map((ib) => (
            <Card key={ib.id} className={cn('min-w-0', !ib.enabled && 'opacity-70')}>
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="truncate font-semibold">{ib.remark}</div>
                  <div className="mt-1.5 flex flex-wrap gap-1.5">
                    <Badge tone="accent">{kindLabel(ib.protocol, ib.transport)}</Badge>
                    <Badge tone="muted">{t('inbounds.portLabel', { network: ib.network.toUpperCase(), port: ib.port })}</Badge>
                    {ib.obfs && <Badge tone="muted">salamander</Badge>}
                    {ib.protocol === 'vmess' && <Badge tone="warning">{t('inbounds.weak')}</Badge>}
                    {ib.testKit && <Badge tone="muted">{t('inbounds.testBadge')}</Badge>}
                  </div>
                </div>
                <Switch checked={ib.enabled} label={t('inbounds.enabled')} disabled={toggle.isPending} onChange={() => toggle.mutate(ib)} />
              </div>
              <InboundFacts ib={ib} />
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

      <Dialog open={removingKit} onClose={() => setRemovingKit(false)} title={t('inbounds.testKitRemove')} description={t('inbounds.testKitRemoveText')}>
        <div className="flex justify-end gap-2">
          <Button variant="secondary" onClick={() => setRemovingKit(false)}>
            {t('common.cancel')}
          </Button>
          <Button variant="danger" disabled={deleteKit.isPending} onClick={() => deleteKit.mutate()}>
            {t('common.delete')}
          </Button>
        </div>
      </Dialog>
    </>
  )
}

/** The camouflage site field with "find the best": the server checks
 *  candidate sites from its own network and ranks them. */
function TargetField(props: {
  label: string
  hint: string
  error?: string
  value: string
  onChange: (v: string) => void
  suggestions: string[]
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const scan = useMutation({ mutationFn: () => api.inbounds.scanTargets() })
  return (
    <div className="space-y-2">
      <Field label={props.label} hint={props.hint} error={props.error}>
        {(id) => (
          <div className="flex gap-2">
            <Input id={id} list="reality-targets" value={props.value} onChange={(e) => props.onChange(e.target.value)} required />
            <datalist id="reality-targets">
              {props.suggestions.map((s) => (
                <option key={s} value={s} />
              ))}
            </datalist>
            <Button
              type="button"
              variant="secondary"
              className="shrink-0"
              disabled={scan.isPending}
              onClick={() => {
                setOpen(true)
                scan.mutate()
              }}
            >
              <Sparkles className="h-4 w-4" />
              {t('inbounds.finder.button')}
            </Button>
          </div>
        )}
      </Field>
      {open && (
        <div className="rounded-xl border bg-card p-3">
          <div className="flex items-start justify-between gap-3">
            <div>
              <div className="text-sm font-medium">{t('inbounds.finder.title')}</div>
              <div className="text-xs text-muted-foreground">{t('inbounds.finder.hint')}</div>
            </div>
            <Button type="button" variant="ghost" size="sm" onClick={() => setOpen(false)}>
              {t('inbounds.finder.close')}
            </Button>
          </div>
          {scan.isPending ? (
            <p className="mt-3 flex items-center gap-2 text-sm text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" />
              {t('inbounds.finder.scanning')}
            </p>
          ) : scan.isError ? (
            <p className="mt-3 text-sm text-destructive">{(scan.error as Error).message}</p>
          ) : (
            <ul className="mt-2 max-h-64 divide-y overflow-y-auto">
              {scan.data?.map((r) => (
                <li key={r.host} className="flex items-center gap-3 py-2 text-sm">
                  <span className="min-w-0 flex-1 truncate">{r.host}</span>
                  {r.good ? (
                    <>
                      <span className="text-xs tabular-nums text-muted-foreground">{r.rttMs} ms</span>
                      <Badge tone="success">{t('inbounds.finder.fits')}</Badge>
                      <Button
                        type="button"
                        size="sm"
                        variant={props.value === r.host ? 'primary' : 'secondary'}
                        onClick={() => {
                          props.onChange(r.host)
                          setOpen(false)
                        }}
                      >
                        {t('inbounds.finder.use')}
                      </Button>
                    </>
                  ) : (
                    <span className="truncate text-xs text-muted-foreground" title={r.error}>
                      {t('inbounds.finder.noFit')}
                      {r.error ? ` · ${r.error}` : ''}
                    </span>
                  )}
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  )
}

function InboundFacts({ ib }: { ib: Inbound }) {
  const { t } = useTranslation()
  const nodes = useQuery({ queryKey: ['nodes'], queryFn: api.nodes.list, staleTime: 30_000 })
  const nodeName = (id: number) => nodes.data?.find((n) => n.id === id)?.name ?? '#' + id
  const facts: [string, string][] = []
  if (realityProtocols.includes(ib.protocol)) facts.push([t('inbounds.target'), ib.serverName])
  if (ib.protocol === 'hysteria2') facts.push([t('inbounds.masquerade'), ib.masquerade?.replace(/^https:\/\/|\/$/g, '') ?? ''])
  if (certProtocols.includes(ib.protocol)) facts.push([t('inbounds.hySni'), ib.serverName])
  if (ib.protocol === 'shadowtls') facts.push([t('inbounds.handshake'), ib.target?.replace(/:443$/, '') ?? ''])
  if (ib.address) facts.push([t('inbounds.address'), ib.address])
  if (ib.nodeId) facts.push([t('inbounds.server'), nodeName(ib.nodeId)])
  facts.push([t('inbounds.appsLabel'), ib.apps.map((a) => t(`inbounds.apps.${a}Short`)).join(', ')])
  const selfSigned = (ib.protocol === 'hysteria2' || certProtocols.includes(ib.protocol)) && !ib.domainCert
  return (
    <>
      <dl className="mt-4 space-y-1.5 text-sm">
        {facts.map(([k, v]) => (
          <div key={k} className="flex min-w-0 justify-between gap-4">
            <dt className="shrink-0 text-muted-foreground">{k}</dt>
            <dd className="min-w-0 truncate text-right" title={v}>
              {v}
            </dd>
          </div>
        ))}
      </dl>
      {selfSigned && <p className="mt-3 rounded-xl bg-warning-soft px-3 py-2 text-xs text-warning">{t('inbounds.selfSignedWarn')}</p>}
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
    nodeId: ib.nodeId ?? 0,
    target: ib.target ?? '',
    serverName: ib.serverName,
    fingerprint: ib.fingerprint ?? 'chrome',
    xhttpPath: ib.xhttpPath ?? '',
    xhttpMode: ib.xhttpMode ?? 'auto',
    grpcServiceName: ib.grpcServiceName ?? '',
    wsPath: ib.wsPath ?? '',
    obfs: ib.obfs,
    masquerade: ib.masquerade ?? '',
    upMbps: ib.upMbps ?? 0,
    downMbps: ib.downMbps ?? 0,
    hopPorts: ib.hopPorts ?? '',
    hopInterval: ib.hopInterval ?? 0,
    enabled: ib.enabled,
    expert: ib.expert ?? {},
  }
}

const emptyInput: InboundInput = {
  remark: '',
  protocol: 'vless',
  transport: 'xhttp',
  port: 0,
  address: '',
  nodeId: 0,
  target: 'www.microsoft.com',
  serverName: '',
  fingerprint: 'chrome',
  xhttpPath: '',
  xhttpMode: 'auto',
  grpcServiceName: '',
  wsPath: '',
  obfs: false,
  masquerade: 'https://www.bing.com/',
  upMbps: 0,
  downMbps: 0,
  hopPorts: '',
  hopInterval: 0,
  enabled: true,
  expert: {},
}

function InboundDialog({ inbound, onClose, onSaved }: { inbound: Inbound | null; onClose: () => void; onSaved: () => void }) {
  const { t } = useTranslation()
  const kindLabel = useKindLabel()
  const [form, setForm] = useState<InboundInput>(inbound ? toInput(inbound) : emptyInput)
  const p = form.protocol
  const spec = protocolSpecs[p]
  const suggest = useQuery({ queryKey: ['inbounds', 'suggest', p], queryFn: () => api.inbounds.suggest(p), enabled: !inbound, staleTime: 0 })
  const domainInfo = useQuery({ queryKey: ['domain'], queryFn: api.domain.get, staleTime: 60_000 })
  const nodes = useQuery({ queryKey: ['nodes'], queryFn: api.nodes.list, staleTime: 30_000 })
  // A trusted certificate is offered for TLS inbounds once the domain has one.
  const domainName = domainInfo.data?.cert && !domainInfo.data.cert.staging ? domainInfo.data.cert.domain : ''
  const [advanced, setAdvanced] = useState(false)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const set = <K extends keyof InboundInput>(k: K, v: InboundInput[K]) => setForm((f) => ({ ...f, [k]: v }))
  const setProtocol = (np: Protocol) =>
    setForm((f) => ({
      ...f,
      protocol: np,
      transport: protocolSpecs[np].transports[0],
      port: 0,
      serverName: '',
      target: 'www.microsoft.com',
    }))

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
    save.mutate({ ...form, remark: form.remark.trim() || kindLabel(p, form.transport), port })
  }

  const num = (v: string) => Number(v.replace(/\D/g, '')) || 0
  const portHint = spec.network === 'udp' ? 'inbounds.udpPortHint' : spec.network === 'tcp+udp' ? 'inbounds.bothPortHint' : 'inbounds.portHint'

  return (
    <Dialog open onClose={onClose} title={inbound ? t('inbounds.editTitle', { name: inbound.remark }) : t('inbounds.createTitle')}>
      <form onSubmit={submit} className="max-h-[75vh] space-y-4 overflow-y-auto pr-1">
        {!inbound && (
          <div className="space-y-2">
            <div className="text-sm font-medium">{t('inbounds.protocol')}</div>
            <div className="grid gap-2 sm:grid-cols-2">
              {protocolOrder.map((proto) => (
                <button
                  key={proto}
                  type="button"
                  onClick={() => setProtocol(proto)}
                  className={cn(
                    'cursor-pointer rounded-xl border p-3 text-left transition',
                    p === proto ? 'border-primary bg-accent/60 ring-2 ring-primary/20' : 'hover:bg-muted',
                  )}
                >
                  <div className="flex flex-wrap items-center gap-1.5 text-sm font-medium">
                    {t(`inbounds.protocols.${proto}.title`)}
                    {proto === 'vless' && <Badge tone="success">{t('inbounds.recommended')}</Badge>}
                    {protocolSpecs[proto].network !== 'tcp' && <Badge tone="muted">{protocolSpecs[proto].network.toUpperCase()}</Badge>}
                    {proto === 'vmess' && <Badge tone="warning">{t('inbounds.weak')}</Badge>}
                  </div>
                  <div className="mt-0.5 text-xs text-muted-foreground">{t(`inbounds.protocols.${proto}.text`)}</div>
                  <div className="mt-1.5 text-[11px] font-medium text-accent-foreground">
                    {t('connect.worksIn', { apps: protocolSpecs[proto].apps.map((a) => t(`inbounds.apps.${a}Short`)).join(', ') })}
                  </div>
                </button>
              ))}
            </div>
          </div>
        )}

        {!inbound && spec.transports.length > 1 && (
          <Field label={t('inbounds.transport')} hint={t(`inbounds.transportHints.${form.transport}`, { defaultValue: '' })} error={errors.transport}>
            {(id) => (
              <Select id={id} value={form.transport} onChange={(e) => set('transport', e.target.value as Transport)}>
                {spec.transports.map((tr) => (
                  <option key={tr} value={tr}>
                    {t(`inbounds.transportNames.${tr}`)}
                  </option>
                ))}
              </Select>
            )}
          </Field>
        )}

        {(nodes.data?.length ?? 0) > 0 && (
          <Field label={t('inbounds.server')} hint={t('inbounds.serverHint')} error={errors.nodeId}>
            {(id) => (
              <Select id={id} value={form.nodeId} onChange={(e) => setForm((f) => ({ ...f, nodeId: Number(e.target.value), hopPorts: '' }))}>
                <option value={0}>{t('servers.thisServer')}</option>
                {nodes.data?.map((n) => (
                  <option key={n.id} value={n.id}>
                    {n.name} ({n.host})
                  </option>
                ))}
              </Select>
            )}
          </Field>
        )}

        <div className="grid gap-4 sm:grid-cols-[1fr_8rem]">
          <Field label={t('inbounds.remark')} error={errors.remark}>
            {(id) => (
              <Input id={id} value={form.remark} onChange={(e) => set('remark', e.target.value)} placeholder={kindLabel(p, form.transport)} maxLength={64} />
            )}
          </Field>
          <Field label={t('inbounds.port')} error={errors.port}>
            {(id) => <Input id={id} inputMode="numeric" value={String(port)} onChange={(e) => set('port', num(e.target.value))} />}
          </Field>
        </div>
        {!errors.port && <p className="-mt-2 text-xs text-muted-foreground">{t(portHint)}</p>}

        {(realityProtocols.includes(p) || p === 'shadowtls') && (
          <TargetField
            label={t(p === 'shadowtls' ? 'inbounds.handshake' : 'inbounds.target')}
            hint={t(p === 'shadowtls' ? 'inbounds.handshakeHint' : 'inbounds.targetHint')}
            error={errors.target}
            value={form.target}
            onChange={(v) => set('target', v)}
            suggestions={suggest.data?.targets ?? []}
          />
        )}
        {p === 'hysteria2' && (
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

            {(p === 'hysteria2' || certProtocols.includes(p)) && (
              <Field label={t('inbounds.hySni')} hint={t('inbounds.hySniHint')} error={errors.serverName}>
                {(id) => <Input id={id} value={form.serverName} onChange={(e) => set('serverName', e.target.value)} placeholder="www.bing.com" />}
              </Field>
            )}
            {(p === 'hysteria2' || certProtocols.includes(p)) && domainName && (
              <button
                type="button"
                onClick={() => set('serverName', domainName)}
                aria-pressed={form.serverName === domainName}
                className={'-mt-2 rounded-full border px-3 py-1 text-xs transition ' + (form.serverName === domainName ? 'border-primary bg-accent text-accent-foreground' : 'hover:bg-card')}
              >
                {form.serverName === domainName ? '✓ ' : ''}
                {t('inbounds.useDomainCert', { domain: domainName })}
              </button>
            )}

            {p === 'hysteria2' && (
              <>
                <div className="grid gap-4 sm:grid-cols-2">
                  <Field label={t('inbounds.upMbps')} error={errors.upMbps}>
                    {(id) => <Input id={id} inputMode="numeric" value={String(form.upMbps)} onChange={(e) => set('upMbps', num(e.target.value))} />}
                  </Field>
                  <Field label={t('inbounds.downMbps')} error={errors.downMbps}>
                    {(id) => <Input id={id} inputMode="numeric" value={String(form.downMbps)} onChange={(e) => set('downMbps', num(e.target.value))} />}
                  </Field>
                </div>
                <p className="-mt-2 text-xs text-muted-foreground">{t('inbounds.bandwidthHint')}</p>
                {form.nodeId === 0 && (
                <div className="grid gap-4 sm:grid-cols-2">
                  <Field label={t('inbounds.hopPorts')} hint={t('inbounds.hopPortsHint')} error={errors.hopPorts}>
                    {(id) => <Input id={id} value={form.hopPorts} onChange={(e) => set('hopPorts', e.target.value)} placeholder="20000-30000" />}
                  </Field>
                  <Field label={t('inbounds.hopInterval')} error={errors.hopInterval}>
                    {(id) => (
                      <Input id={id} inputMode="numeric" value={form.hopInterval ? String(form.hopInterval) : ''} onChange={(e) => set('hopInterval', num(e.target.value))} placeholder="30" disabled={!form.hopPorts} />
                    )}
                  </Field>
                </div>
                )}
                {form.hopPorts && <p className="-mt-2 text-xs text-muted-foreground">{t('inbounds.hopIntervalNote')}</p>}
              </>
            )}

            {realityProtocols.includes(p) && (
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
              </>
            )}

            {p === 'vmess' && form.transport === 'ws' && (
              <Field label="WebSocket path" hint={t('inbounds.xhttpPathHint')} error={errors.wsPath}>
                {(id) => <Input id={id} value={form.wsPath} onChange={(e) => set('wsPath', e.target.value)} placeholder="/…" />}
              </Field>
            )}

            <p className="text-xs text-muted-foreground">{t('inbounds.keysNote')}</p>
          </div>
        )}

        {spec.core === 'xray' && (
          <ExpertSection protocol={p} transport={form.transport} value={form.expert} onChange={(v) => set('expert', v)} errors={errors} />
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
