import { useEffect, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown, Wrench } from 'lucide-react'
import type { ExpertInput, Protocol, Transport, XHTTPExpert } from '@/lib/api'
import { cn } from '@/lib/utils'
import { Field, Input, Select, Switch } from './ui'

// Fields of the main and "advanced" parts of the wizard, not of this section.
const mainFields = [
  'form', 'remark', 'port', 'target', 'masquerade', 'transport', 'protocol', 'address', 'serverName',
  'fingerprint', 'xhttpPath', 'xhttpMode', 'grpcServiceName', 'wsPath', 'upMbps', 'downMbps',
]

const defaultSniffing ={ enabled: true, destOverride: ['http', 'tls', 'quic'], metadataOnly: false, routeOnly: true }
const defaultReality = { xver: 0, maxTimeDiff: 0, minClientVer: '', maxClientVer: '', spiderX: '', shortIds: [] as string[], mldsa65: false }

function Group({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="space-y-3 rounded-xl border bg-card p-4">
      <div className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{title}</div>
      {children}
    </div>
  )
}

function Toggle({ label, hint, checked, onChange }: { label: string; hint?: string; checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <div className="flex items-start justify-between gap-4">
      <div>
        <div className="text-sm">{label}</div>
        {hint && <div className="text-xs text-muted-foreground">{hint}</div>}
      </div>
      <Switch checked={checked} onChange={onChange} label={label} />
    </div>
  )
}

/** Fine-tuning for Xray inbounds, mirroring what 3x-ui exposes. Every empty
 *  field means "Xray default"; the server validates everything. */
export function ExpertSection({
  protocol,
  transport,
  value,
  onChange,
  errors,
}: {
  protocol: Protocol
  transport: Transport
  value: ExpertInput
  onChange: (v: ExpertInput) => void
  errors: Record<string, string>
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  // Open by itself when the server rejected one of its fields.
  const failed = Object.keys(errors).some((k) => !mainFields.includes(k))
  useEffect(() => {
    if (failed) setOpen(true)
  }, [failed])
  const reality = protocol === 'vless' || protocol === 'trojan'
  const r = { ...defaultReality, ...value.reality }
  const x: XHTTPExpert = value.xhttp ?? {}
  const sn = value.sniffing ?? defaultSniffing
  const so = value.sockopt ?? {}
  const setR = (p: Partial<typeof r>) => onChange({ ...value, reality: { ...r, ...p } })
  const setX = (p: Partial<XHTTPExpert>) => onChange({ ...value, xhttp: { ...x, ...p } })
  const setXmux = (p: Partial<NonNullable<XHTTPExpert['xmux']>>) => setX({ xmux: { ...x.xmux, ...p } })
  const setSn = (p: Partial<typeof sn>) => onChange({ ...value, sniffing: { ...sn, ...p } })
  const setSo = (p: Partial<typeof so>) => onChange({ ...value, sockopt: { ...so, ...p } })
  const e = (k: string) => errors[k]

  const headersText = Object.entries(x.headers ?? {})
    .map(([k, v]) => `${k}: ${v}`)
    .join('\n')
  const parseHeaders = (text: string) => {
    const h: Record<string, string> = {}
    for (const line of text.split('\n')) {
      const i = line.indexOf(':')
      if (i > 0) h[line.slice(0, i).trim()] = line.slice(i + 1).trim()
    }
    return h
  }

  const text = (label: string, k: keyof XHTTPExpert, placeholder: string, hint?: string) => (
    <Field label={label} hint={hint} error={e(k)}>
      {(id) => <Input id={id} value={(x[k] as string) ?? ''} placeholder={placeholder} onChange={(ev) => setX({ [k]: ev.target.value })} />}
    </Field>
  )
  const choice = (label: string, k: keyof XHTTPExpert, options: string[], hint?: string) => (
    <Field label={label} hint={hint} error={e(k)}>
      {(id) => (
        <Select id={id} value={(x[k] as string) ?? ''} onChange={(ev) => setX({ [k]: ev.target.value })}>
          <option value="">{t('inbounds.expert.default')}</option>
          {options.map((o) => (
            <option key={o} value={o}>
              {o}
            </option>
          ))}
        </Select>
      )}
    </Field>
  )
  const mux = (label: string, k: keyof NonNullable<XHTTPExpert['xmux']>, placeholder: string) => (
    <Field label={label} error={e(k)}>
      {(id) => (
        <Input
          id={id}
          value={String(x.xmux?.[k] ?? '')}
          placeholder={placeholder}
          onChange={(ev) => setXmux({ [k]: k === 'hKeepAlivePeriod' ? Number(ev.target.value.replace(/\D/g, '')) || 0 : ev.target.value })}
        />
      )}
    </Field>
  )

  return (
    <div>
      <button
        type="button"
        onClick={() => setOpen(!open)}
        className="flex cursor-pointer items-center gap-1.5 text-sm font-medium text-muted-foreground hover:text-foreground"
      >
        <ChevronDown className={cn('h-4 w-4 transition', open && 'rotate-180')} />
        <Wrench className="h-3.5 w-3.5" />
        {t('inbounds.expert.title')}
      </button>
      {open && (
        <div className="mt-3 space-y-3">
          <p className="text-xs text-muted-foreground">{t('inbounds.expert.intro')}</p>

          {reality && (
            <Group title={t('inbounds.expert.reality')}>
              <Toggle label={t('inbounds.expert.mldsa65')} hint={t('inbounds.expert.mldsa65Hint')} checked={r.mldsa65} onChange={(v) => setR({ mldsa65: v })} />
              <div className="grid gap-3 sm:grid-cols-2">
                <Field label={t('inbounds.expert.xver')} hint={t('inbounds.expert.xverHint')} error={e('xver')}>
                  {(id) => (
                    <Select id={id} value={String(r.xver)} onChange={(ev) => setR({ xver: Number(ev.target.value) })}>
                      {[0, 1, 2].map((v) => (
                        <option key={v} value={v}>
                          {v}
                        </option>
                      ))}
                    </Select>
                  )}
                </Field>
                <Field label={t('inbounds.expert.maxTimeDiff')} hint={t('inbounds.expert.maxTimeDiffHint')} error={e('maxTimeDiff')}>
                  {(id) => (
                    <Input id={id} inputMode="numeric" value={r.maxTimeDiff ? String(r.maxTimeDiff) : ''} placeholder="0" onChange={(ev) => setR({ maxTimeDiff: Number(ev.target.value.replace(/\D/g, '')) || 0 })} />
                  )}
                </Field>
                <Field label={t('inbounds.expert.minClientVer')} hint={t('inbounds.expert.clientVerHint')} error={e('minClientVer')}>
                  {(id) => <Input id={id} value={r.minClientVer} placeholder="—" onChange={(ev) => setR({ minClientVer: ev.target.value.trim() })} />}
                </Field>
                <Field label={t('inbounds.expert.maxClientVer')} error={e('maxClientVer')}>
                  {(id) => <Input id={id} value={r.maxClientVer} placeholder="—" onChange={(ev) => setR({ maxClientVer: ev.target.value.trim() })} />}
                </Field>
              </div>
              <Field label={t('inbounds.expert.shortIds')} hint={t('inbounds.expert.shortIdsHint')} error={e('shortIds')}>
                {(id) => (
                  <Input
                    id={id}
                    value={r.shortIds.join(', ')}
                    onChange={(ev) => {
                      const v = ev.target.value
                      setR({ shortIds: v.trim() === '' ? [] : v.split(',').map((s) => s.trim()) })
                    }}
                    className="font-mono"
                  />
                )}
              </Field>
              <Field label={t('inbounds.expert.spiderX')} hint={t('inbounds.expert.spiderXHint')} error={e('spiderX')}>
                {(id) => <Input id={id} value={r.spiderX} placeholder="/" onChange={(ev) => setR({ spiderX: ev.target.value.trim() })} />}
              </Field>
            </Group>
          )}

          {transport === 'xhttp' && (
            <>
              <Group title={t('inbounds.expert.xhttp')}>
                {text(t('inbounds.expert.host'), 'host', '—', t('inbounds.expert.hostHint'))}
                <Field label={t('inbounds.expert.headers')} hint={t('inbounds.expert.headersHint')} error={e('xhttpHeaders')}>
                  {(id) => (
                    <textarea
                      id={id}
                      rows={2}
                      defaultValue={headersText}
                      onBlur={(ev) => setX({ headers: parseHeaders(ev.target.value) })}
                      placeholder="X-Forwarded-For: 1.2.3.4"
                      className="w-full rounded-xl border border-input bg-card px-3 py-2 font-mono text-sm focus:border-ring focus:outline-none focus:ring-2 focus:ring-ring/25"
                    />
                  )}
                </Field>
                <div className="grid gap-3 sm:grid-cols-2">
                  {text(t('inbounds.expert.paddingBytes'), 'paddingBytes', '100-1000', t('inbounds.expert.paddingBytesHint'))}
                  {choice(t('inbounds.expert.paddingPlacement'), 'paddingPlacement', ['queryInHeader', 'header', 'cookie', 'query'])}
                  {choice(t('inbounds.expert.paddingMethod'), 'paddingMethod', ['repeat-x', 'tokenish'])}
                  {choice(t('inbounds.expert.uplinkHttpMethod'), 'uplinkHttpMethod', ['POST', 'PUT', 'PATCH', 'GET'], t('inbounds.expert.uplinkHttpMethodHint'))}
                  {choice(t('inbounds.expert.sessionPlacement'), 'sessionPlacement', ['path', 'cookie', 'header', 'query'])}
                  {choice(t('inbounds.expert.seqPlacement'), 'seqPlacement', ['path', 'cookie', 'header', 'query'])}
                  {choice(t('inbounds.expert.uplinkDataPlacement'), 'uplinkDataPlacement', ['auto', 'body', 'cookie', 'header'], t('inbounds.expert.uplinkDataPlacementHint'))}
                  {text(t('inbounds.expert.maxEachPostBytes'), 'maxEachPostBytes', '1000000', t('inbounds.expert.rangeHint'))}
                  {text(t('inbounds.expert.minPostsIntervalMs'), 'minPostsIntervalMs', '30', t('inbounds.expert.rangeHint'))}
                  <Field label={t('inbounds.expert.maxBufferedPosts')} error={e('maxBufferedPosts')}>
                    {(id) => (
                      <Input id={id} inputMode="numeric" value={x.maxBufferedPosts ? String(x.maxBufferedPosts) : ''} placeholder="30" onChange={(ev) => setX({ maxBufferedPosts: Number(ev.target.value.replace(/\D/g, '')) || 0 })} />
                    )}
                  </Field>
                  {text(t('inbounds.expert.streamUpServerSecs'), 'streamUpServerSecs', '20-80', t('inbounds.expert.rangeHint'))}
                </div>
                <Toggle label={t('inbounds.expert.paddingObfsMode')} hint={t('inbounds.expert.paddingObfsModeHint')} checked={!!x.paddingObfsMode} onChange={(v) => setX({ paddingObfsMode: v })} />
                <Toggle label={t('inbounds.expert.noSseHeader')} hint={t('inbounds.expert.noSseHeaderHint')} checked={!!x.noSseHeader} onChange={(v) => setX({ noSseHeader: v })} />
                <Toggle label={t('inbounds.expert.noGrpcHeader')} hint={t('inbounds.expert.noGrpcHeaderHint')} checked={!!x.noGrpcHeader} onChange={(v) => setX({ noGrpcHeader: v })} />
              </Group>
              <Group title={t('inbounds.expert.xmux')}>
                <div className="grid gap-3 sm:grid-cols-2">
                  {mux(t('inbounds.expert.maxConcurrency'), 'maxConcurrency', '16-32')}
                  {mux(t('inbounds.expert.maxConnections'), 'maxConnections', '0')}
                  {mux(t('inbounds.expert.cMaxReuseTimes'), 'cMaxReuseTimes', '0')}
                  {mux(t('inbounds.expert.hMaxRequestTimes'), 'hMaxRequestTimes', '600-900')}
                  {mux(t('inbounds.expert.hMaxReusableSecs'), 'hMaxReusableSecs', '1800-3000')}
                  {mux(t('inbounds.expert.hKeepAlivePeriod'), 'hKeepAlivePeriod', '0')}
                </div>
              </Group>
            </>
          )}

          <Group title={t('inbounds.expert.sniffing')}>
            <Toggle label={t('inbounds.expert.sniffEnabled')} hint={t('inbounds.expert.sniffEnabledHint')} checked={sn.enabled} onChange={(v) => setSn({ enabled: v })} />
            <div className="flex flex-wrap gap-4 text-sm">
              {['http', 'tls', 'quic'].map((p) => (
                <label key={p} className="flex cursor-pointer items-center gap-2">
                  <input
                    type="checkbox"
                    className="h-4 w-4 accent-[var(--primary)]"
                    checked={sn.destOverride.includes(p)}
                    onChange={(ev) => setSn({ destOverride: ev.target.checked ? [...sn.destOverride, p] : sn.destOverride.filter((d) => d !== p) })}
                  />
                  {p.toUpperCase()}
                </label>
              ))}
            </div>
            <Toggle label={t('inbounds.expert.metadataOnly')} checked={sn.metadataOnly} onChange={(v) => setSn({ metadataOnly: v })} />
            <Toggle label={t('inbounds.expert.routeOnly')} hint={t('inbounds.expert.routeOnlyHint')} checked={sn.routeOnly} onChange={(v) => setSn({ routeOnly: v })} />
          </Group>

          <Group title={t('inbounds.expert.sockopt')}>
            <Field label={t('inbounds.expert.tcpCongestion')} error={e('tcpCongestion')}>
              {(id) => (
                <Select id={id} value={so.tcpCongestion ?? ''} onChange={(ev) => setSo({ tcpCongestion: ev.target.value })}>
                  <option value="">{t('inbounds.expert.default')}</option>
                  {['bbr', 'cubic', 'reno'].map((o) => (
                    <option key={o} value={o}>
                      {o}
                    </option>
                  ))}
                </Select>
              )}
            </Field>
            <Toggle label={t('inbounds.expert.tcpFastOpen')} checked={!!so.tcpFastOpen} onChange={(v) => setSo({ tcpFastOpen: v })} />
            <Toggle label={t('inbounds.expert.tcpMptcp')} checked={!!so.tcpMptcp} onChange={(v) => setSo({ tcpMptcp: v })} />
            <Toggle label={t('inbounds.expert.acceptProxyProtocol')} hint={t('inbounds.expert.acceptProxyProtocolHint')} checked={!!so.acceptProxyProtocol} onChange={(v) => setSo({ acceptProxyProtocol: v })} />
          </Group>
        </div>
      )}
    </div>
  )
}
