import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ArrowDown,
  ArrowUp,
  Cloud,
  Database,
  Globe,
  ListFilter,
  Pencil,
  Plus,
  RefreshCw,
  ShieldCheck,
  History,
  Smartphone,
  Waypoints,
  Trash2,
} from 'lucide-react'
import {
  ApiError,
  api,
  type CoreSupport,
  type Outbound,
  type RoutingRule,
  type RoutingSettings,
  type Trace,
  type User,
} from '@/lib/api'
import { cn, formatBytes } from '@/lib/utils'
import { Badge, Button, Card, Dialog, Field, Input, PageHeader, Select, Switch, Tabs, useSavedTab } from '@/components/ui'

const outbounds: Outbound[] = ['direct', 'block', 'warp', 'ipv4']
// Labels for outbounds, including the admin's own exits ("out:<id>").
function useOutLabel(custom: { id: string; name: string }[]) {
  const { t } = useTranslation()
  return (o: string) => (o.startsWith('out:') ? (custom.find((c) => `out:${c.id}` === o)?.name ?? o) : t(`routing.outbound.${o}`))
}
const customChoices = (custom: { id: string }[]) => custom.filter((c) => c.id).map((c) => `out:${c.id}` as Outbound)

const outboundTone: Record<string, 'muted' | 'danger' | 'accent' | 'warning'> = {
  direct: 'muted',
  block: 'danger',
  warp: 'accent',
  ipv4: 'warning',
}

function Section({ icon, title, hint, actions, children }: { icon: ReactNode; title: string; hint?: string; actions?: ReactNode; children: ReactNode }) {
  return (
    <Card>
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="flex items-center gap-2.5">
            <span className="text-accent-foreground">{icon}</span>
            <h2 className="font-semibold">{title}</h2>
          </div>
          {hint && <p className="mt-1 text-sm text-muted-foreground">{hint}</p>}
        </div>
        {actions}
      </div>
      <div className="mt-4">{children}</div>
    </Card>
  )
}

function ToggleRow({ title, hint, checked, onChange, disabled }: { title: string; hint: string; checked: boolean; onChange: (v: boolean) => void; disabled?: boolean }) {
  return (
    <div className="flex items-start justify-between gap-4 py-2.5">
      <div>
        <div className="text-sm font-medium">{title}</div>
        <div className="text-xs text-muted-foreground">{hint}</div>
      </div>
      <Switch checked={checked} onChange={onChange} label={title} disabled={disabled} />
    </div>
  )
}

const routingTabs = ['basic', 'rules', 'exits', 'dns', 'history'] as const

export function RoutingPage() {
  const { t } = useTranslation()
  const [tab, choose] = useSavedTab('routing.tab', routingTabs)
  const qc = useQueryClient()
  const routing = useQuery({ queryKey: ['routing'], queryFn: api.routing.get })
  const users = useQuery({ queryKey: ['users'], queryFn: api.users.list })
  const [draft, setDraft] = useState<RoutingSettings | null>(null)
  const [error, setError] = useState('')

  // The draft follows the server until the admin changes something.
  const base = routing.data
  const s = draft ?? base
  const dirty = draft !== null && JSON.stringify(draft) !== JSON.stringify(base)
  const edit = (fn: (d: RoutingSettings) => void) => {
    const next = structuredClone(s!)
    fn(next)
    setDraft(next)
  }
  const accept = (data: RoutingSettings) => {
    qc.setQueryData(['routing'], data)
    // Keep unsaved edits; WARP itself is whatever the server now has.
    setDraft((d) => (d ? { ...d, warp: data.warp } : null))
  }

  const save = useMutation({
    mutationFn: () => api.routing.save(s!),
    onSuccess: (data) => {
      qc.setQueryData(['routing'], data)
      setDraft(null)
      qc.invalidateQueries({ queryKey: ['routing', 'history'] })
      setError('')
    },
    onError: (err) => setError(err instanceof ApiError && err.field ? `${err.field}: ${err.message}` : t('common.error')),
  })

  if (routing.isPending) return <p className="text-sm text-muted-foreground">{t('common.loading')}</p>
  if (routing.isError || !s) return <p className="text-sm text-destructive">{t('common.error')}</p>

  return (
    <>
      <PageHeader title={t('routing.title')} subtitle={t('routing.subtitle')} />
      <div className="mx-auto max-w-3xl">
        <Tabs tabs={routingTabs} value={tab} onChange={choose} label={(v) => t(`routing.tab_${v}`)} />
        <div className="space-y-4">
          {tab === 'basic' && (
            <>
              <Section icon={<ShieldCheck className="h-5 w-5" />} title={t('routing.protection')} hint={t('routing.protectionHint')}>
                <div className="divide-y">
                  {(['blockPrivate', 'blockSmtp', 'blockBittorrent', 'blockAds', 'googleIpv4'] as const).map((k) => (
                    <ToggleRow
                      key={k}
                      title={t(`routing.presets.${k}`)}
                      hint={t(`routing.presets.${k}Hint`)}
                      checked={s.presets[k]}
                      onChange={(v) => edit((d) => (d.presets[k] = v))}
                    />
                  ))}
                </div>
              </Section>
              <CountriesCard s={s} edit={edit} />
              <ClientCard s={s} edit={edit} />
            </>
          )}
          {tab === 'rules' && (
            <>
              <RulesCard s={s} edit={edit} users={users.data ?? []} />
              <GeoCard s={s} edit={edit} />
            </>
          )}
          {tab === 'exits' && (
            <>
              <WarpCard s={s} edit={edit} onServer={accept} />
              <OutboundsCard s={s} edit={edit} />
            </>
          )}
          {tab === 'dns' && <DnsCard s={s} edit={edit} />}
          {tab === 'history' && <HistoryCard onServer={accept} />}
        </div>
      </div>

      {(dirty || error) && (
        <div className="sticky bottom-4 z-20 mt-4 flex flex-wrap items-center justify-between gap-3 rounded-2xl border bg-card px-4 py-3 shadow-lg">
          <div className="text-sm">
            {error ? <span className="text-destructive">{error}</span> : <span className="text-muted-foreground">{t('routing.unsaved')}</span>}
          </div>
          <div className="flex gap-2">
            <Button
              variant="secondary"
              onClick={() => {
                setDraft(null)
                setError('')
              }}
            >
              {t('common.cancel')}
            </Button>
            <Button disabled={save.isPending || !dirty} onClick={() => save.mutate()}>
              {t('routing.apply')}
            </Button>
          </div>
        </div>
      )}
    </>
  )
}

// Сколько ждать после перевыпуска WARP, пока Xray перейдёт на новое устройство.
const settleMs = 8000

type CardProps = { s: RoutingSettings; edit: (fn: (d: RoutingSettings) => void) => void }

function TraceLine({ label, tr }: { label: string; tr: Trace }) {
  return (
    <div className="flex flex-wrap items-baseline justify-between gap-x-3 text-sm">
      <span className="text-muted-foreground">{label}</span>
      <span className="font-mono text-xs">
        {tr.ip} · {tr.country} · {tr.latencyMs} ms
      </span>
    </div>
  )
}

function WarpCard({ s, edit, onServer }: CardProps & { onServer: (d: RoutingSettings) => void }) {
  const { t } = useTranslation()
  const [license, setLicense] = useState('')
  const [showLicense, setShowLicense] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const fail = (err: unknown) => (err instanceof ApiError ? err.message : t('common.error'))
  // Перевыпуск: регистрация у Cloudflare, затем несколько секунд, пока Xray
  // переходит на новое устройство. Всё это время кнопка заливается цветом,
  // а «Проверить» недоступна: проверка раньше времени ходит в удалённое устройство.
  const [settling, setSettling] = useState(false)
  const [grow, setGrow] = useState(false)
  const register = useMutation({
    mutationFn: api.routing.registerWarp,
    onMutate: () => {
      // Заливка стартует с нуля: ширина меняется кадром позже, чтобы сработал переход.
      setGrow(false)
      requestAnimationFrame(() => requestAnimationFrame(() => setGrow(true)))
    },
    onSuccess: (d) => {
      onServer(d)
      setSettling(true)
      setTimeout(() => {
        setSettling(false)
        check.mutate()
      }, settleMs)
    },
  })
  const setKey = useMutation({ mutationFn: () => api.routing.warpLicense(license), onSuccess: (d) => (onServer(d), setLicense(''), setShowLicense(false)) })
  const remove = useMutation({ mutationFn: api.routing.deleteWarp, onSuccess: (d) => (onServer(d), setConfirmDelete(false)) })
  const check = useMutation({ mutationFn: api.routing.checkWarp })
  const reissuing = register.isPending || settling
  const w = s.warp

  return (
    <Section
      icon={<Cloud className="h-5 w-5" />}
      title="Cloudflare WARP"
      hint={t('routing.warpHint')}
      actions={w.registered ? <Badge tone={w.warpPlus ? 'accent' : 'success'}>{w.warpPlus ? 'WARP+' : t('routing.warpFree')}</Badge> : undefined}
    >
      {!w.registered ? (
        <div className="space-y-3">
          <p className="text-sm text-muted-foreground">{t('routing.warpIntro')}</p>
          <Button onClick={() => register.mutate()} disabled={register.isPending}>
            <Cloud className="h-4 w-4" />
            {register.isPending ? t('routing.warpRegistering') : t('routing.warpRegister')}
          </Button>
          {register.isError && <p className="text-sm text-destructive">{fail(register.error)}</p>}
        </div>
      ) : (
        <div className="space-y-4">
          <div className="grid gap-2 sm:grid-cols-3">
            {(['off', 'rules', 'all'] as const).map((m) => (
              <button
                key={m}
                type="button"
                onClick={() => edit((d) => (d.warp.mode = m))}
                className={
                  'rounded-xl border p-3 text-left transition ' + (w.mode === m ? 'border-primary bg-accent' : 'hover:bg-muted/60')
                }
              >
                <div className="text-sm font-medium">{t(`routing.warpMode.${m}`)}</div>
                <div className="mt-0.5 text-xs text-muted-foreground">{t(`routing.warpMode.${m}Hint`)}</div>
              </button>
            ))}
          </div>

          <div>
            <div className="text-sm font-medium">{t('routing.warpServices')}</div>
            <p className="text-xs text-muted-foreground">{t('routing.warpServicesHint')}</p>
            <div className="mt-2 flex flex-wrap gap-2">
              {s.services.map((sv) => {
                const on = s.presets.warpServices.includes(sv)
                return (
                  <button
                    key={sv}
                    type="button"
                    aria-pressed={on}
                    disabled={w.mode === 'off'}
                    onClick={() =>
                      edit((d) => {
                        d.presets.warpServices = on ? d.presets.warpServices.filter((x) => x !== sv) : [...d.presets.warpServices, sv]
                      })
                    }
                    className={
                      'rounded-full border px-3 py-1 text-sm transition disabled:opacity-50 ' +
                      (on ? 'border-primary bg-accent text-accent-foreground' : 'hover:bg-muted/60')
                    }
                  >
                    {t(`routing.services.${sv}`)}
                  </button>
                )
              })}
            </div>
          </div>

          <div className="rounded-xl bg-muted/60 p-3.5 text-xs text-muted-foreground">
            {t('routing.warpAddress')}: <span className="font-mono">{w.address4}</span>
            {w.address6 && <span className="font-mono"> · {w.address6}</span>}
          </div>

          {check.data && (
            <div className="space-y-1.5 rounded-xl border p-3.5">
              <TraceLine label={t('routing.checkDirect')} tr={check.data.direct} />
              {check.data.warp ? (
                <TraceLine label={t('routing.checkWarp')} tr={check.data.warp} />
              ) : (
                <p className="text-sm text-destructive">{t('routing.checkFailed', { error: check.data.error ?? '' })}</p>
              )}
            </div>
          )}

          {showLicense && (
            <div className="flex gap-2">
              <Input value={license} onChange={(e) => setLicense(e.target.value)} placeholder="xxxxxxxx-xxxxxxxx-xxxxxxxx" />
              <Button onClick={() => setKey.mutate()} disabled={setKey.isPending || license.trim().length < 8}>
                {t('common.save')}
              </Button>
            </div>
          )}
          {setKey.isError && <p className="text-sm text-destructive">{fail(setKey.error)}</p>}

          {confirmDelete ? (
            <div className="rounded-xl bg-destructive-soft p-3.5 text-sm">
              <p>{t('routing.warpDeleteConfirm')}</p>
              <div className="mt-3 flex justify-end gap-2">
                <Button variant="secondary" size="sm" onClick={() => setConfirmDelete(false)}>
                  {t('common.cancel')}
                </Button>
                <Button variant="danger" size="sm" disabled={remove.isPending} onClick={() => remove.mutate()}>
                  {t('common.delete')}
                </Button>
              </div>
            </div>
          ) : (
            <div className="flex flex-wrap gap-2">
              <Button variant="secondary" size="sm" disabled={check.isPending || w.mode === 'off' || reissuing} onClick={() => check.mutate()}>
                {check.isPending ? t('routing.checking') : t('routing.check')}
              </Button>
              {!showLicense && (
                <Button variant="ghost" size="sm" disabled={reissuing} onClick={() => setShowLicense(true)}>
                  {t('routing.warpPlusKey')}
                </Button>
              )}
              <Button
                variant="ghost"
                size="sm"
                className={cn('relative overflow-hidden', reissuing && 'disabled:opacity-100')}
                disabled={reissuing}
                onClick={() => register.mutate()}
                title={t('routing.warpReissueHint')}
              >
                {reissuing && (
                  <span
                    aria-hidden
                    className="absolute inset-y-0 left-0 rounded-xl bg-primary/20 transition-[width] ease-out"
                    style={{ width: settling ? '100%' : grow ? '70%' : '0%', transitionDuration: settling ? settleMs + 'ms' : '3000ms' }}
                  />
                )}
                <RefreshCw className={cn('relative h-4 w-4', reissuing && 'animate-spin')} />
                <span className="relative">{register.isPending ? t('routing.warpRegistering') : settling ? t('routing.warpApplying') : t('routing.warpReissue')}</span>
              </Button>
              <Button variant="ghost" size="sm" disabled={reissuing} onClick={() => setConfirmDelete(true)}>
                <Trash2 className="h-4 w-4" />
                {t('common.delete')}
              </Button>
            </div>
          )}
        </div>
      )}
    </Section>
  )
}

function SupportMarks({ support }: { support?: RoutingRule['support'] }) {
  const { t } = useTranslation()
  if (!support) return null
  const mark = (v: CoreSupport) => (v === 'full' ? '✓' : v === 'partial' ? '≈' : '—')
  const tip = (core: string, v: CoreSupport) => `${core}: ${t(`routing.support.${v}`)}`
  return (
    <span className="text-[11px] text-muted-foreground" title={[tip('Xray', support.xray), tip('Hysteria', support.hysteria), tip('sing-box', support.singbox)].join('\n')}>
      Xray {mark(support.xray)} · Hy2 {mark(support.hysteria)} · sing-box {mark(support.singbox)}
    </span>
  )
}

function RulesCard({ s, edit, users }: CardProps & { users: User[] }) {
  const { t } = useTranslation()
  const outLabel = useOutLabel(s.outbounds)
  const [editing, setEditing] = useState<{ index: number; rule: RoutingRule } | null>(null)
  const move = (i: number, by: number) =>
    edit((d) => {
      const [r] = d.rules.splice(i, 1)
      d.rules.splice(i + by, 0, r)
    })
  const blank: RoutingRule = { id: '', name: '', enabled: true, match: [], ports: '', network: '', protocols: [], users: [], outbound: 'direct' }

  return (
    <Section
      icon={<ListFilter className="h-5 w-5" />}
      title={t('routing.rules')}
      hint={t('routing.rulesHint')}
      actions={
        <Button size="sm" variant="secondary" onClick={() => setEditing({ index: -1, rule: blank })}>
          <Plus className="h-4 w-4" />
          {t('routing.addRule')}
        </Button>
      }
    >
      {s.rules.length === 0 ? (
        <p className="rounded-xl border border-dashed p-4 text-sm text-muted-foreground">{t('routing.noRules')}</p>
      ) : (
        <ul className="divide-y rounded-xl border">
          {s.rules.map((r, i) => (
            <li key={r.id || i} className="flex items-center gap-3 p-3">
              <Switch checked={r.enabled} label={r.enabled ? t('routing.ruleOn') : t('routing.ruleOff')} onChange={(v) => edit((d) => (d.rules[i].enabled = v))} />
              <div className={'min-w-0 flex-1 ' + (r.enabled ? '' : 'opacity-60')}>
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-sm font-medium">{r.name || t('routing.untitled')}</span>
                  <Badge tone={outboundTone[r.outbound] ?? 'accent'}>{outLabel(r.outbound)}</Badge>
                </div>
                <div className="mt-0.5 truncate text-xs text-muted-foreground">{ruleSummary(r, users, t)}</div>
                <SupportMarks support={r.support} />
              </div>
              <div className="flex shrink-0 items-center">
                <Button variant="ghost" size="icon" aria-label={t('routing.up')} disabled={i === 0} onClick={() => move(i, -1)}>
                  <ArrowUp className="h-4 w-4" />
                </Button>
                <Button variant="ghost" size="icon" aria-label={t('routing.down')} disabled={i === s.rules.length - 1} onClick={() => move(i, 1)}>
                  <ArrowDown className="h-4 w-4" />
                </Button>
                <Button variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => setEditing({ index: i, rule: r })}>
                  <Pencil className="h-4 w-4" />
                </Button>
                <Button variant="ghost" size="icon" aria-label={t('common.delete')} onClick={() => edit((d) => d.rules.splice(i, 1))}>
                  <Trash2 className="h-4 w-4" />
                </Button>
              </div>
            </li>
          ))}
        </ul>
      )}
      <p className="mt-3 text-xs text-muted-foreground">{t('routing.orderHint')}</p>
      {editing && (
        <RuleDialog
          rule={editing.rule}
          users={users}
          custom={s.outbounds}
          warpOff={s.warp.mode === 'off' || !s.warp.registered}
          onClose={() => setEditing(null)}
          onSave={(r) => {
            edit((d) => {
              // Support marks are recomputed by the server on save.
              const { support: _s, ...clean } = r
              if (editing.index < 0) d.rules.push(clean)
              else d.rules[editing.index] = clean
            })
            setEditing(null)
          }}
        />
      )}
    </Section>
  )
}

function ruleSummary(r: RoutingRule, users: User[], t: (k: string, o?: Record<string, unknown>) => string): string {
  const parts: string[] = []
  if (r.match.length) parts.push(r.match.slice(0, 3).join(', ') + (r.match.length > 3 ? ` +${r.match.length - 3}` : ''))
  if (r.ports) parts.push(t('routing.portsShort', { ports: r.ports }))
  if (r.network) parts.push(r.network.toUpperCase())
  if (r.protocols.length) parts.push(r.protocols.join(', '))
  if (r.users.length) parts.push(r.users.map((id) => users.find((u) => u.id === id)?.name ?? `#${id}`).join(', '))
  return parts.join(' · ')
}

function RuleDialog({
  rule,
  users,
  custom,
  warpOff,
  onClose,
  onSave,
}: {
  rule: RoutingRule
  users: User[]
  custom: { id: string; name: string }[]
  warpOff: boolean
  onClose: () => void
  onSave: (r: RoutingRule) => void
}) {
  const { t } = useTranslation()
  const outLabel = useOutLabel(custom)
  const [r, setR] = useState(rule)
  const [text, setText] = useState(rule.match.join('\n'))
  const [more, setMore] = useState(Boolean(rule.ports || rule.network || rule.protocols.length || rule.users.length))
  const lastLine = text.split('\n').at(-1)?.trim() ?? ''
  const [q, setQ] = useState('')
  useEffect(() => {
    const id = setTimeout(() => setQ(lastLine.startsWith('geo') ? lastLine : ''), 200)
    return () => clearTimeout(id)
  }, [lastLine])
  const codes = useQuery({ queryKey: ['geoCodes', q], queryFn: () => api.routing.geoCodes(q), enabled: q.length >= 4 })
  const match = useMemo(() => text.split('\n').map((x) => x.trim()).filter(Boolean), [text])
  const empty = match.length === 0 && !r.ports && !r.network && r.protocols.length === 0 && r.users.length === 0

  const pick = (code: string) => {
    const lines = text.split('\n')
    lines[lines.length - 1] = code
    setText(lines.join('\n') + '\n')
  }

  return (
    <Dialog open onClose={onClose} title={rule.id ? t('routing.editRule') : t('routing.newRule')}>
      <form
        className="max-h-[75vh] space-y-4 overflow-y-auto pr-1"
        onSubmit={(e) => {
          e.preventDefault()
          onSave({ ...r, match })
        }}
      >
        <Field label={t('routing.ruleName')}>
          {(id) => <Input id={id} value={r.name} maxLength={80} onChange={(e) => setR({ ...r, name: e.target.value })} placeholder={t('routing.ruleNamePlaceholder')} />}
        </Field>
        <Field label={t('routing.ruleMatch')} hint={t('routing.ruleMatchHint')}>
          {(id) => (
            <textarea
              id={id}
              rows={5}
              value={text}
              onChange={(e) => setText(e.target.value)}
              placeholder={'geosite:openai\nexample.com\ngeoip:ru\n203.0.113.0/24'}
              className="w-full rounded-xl border border-input bg-card px-3.5 py-2.5 font-mono text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
            />
          )}
        </Field>
        {codes.data && codes.data.length > 0 && q && (
          <div className="-mt-2 flex flex-wrap gap-1.5">
            {codes.data.slice(0, 16).map((c) => (
              <button key={c} type="button" onClick={() => pick(c)} className="rounded-full border px-2.5 py-0.5 font-mono text-xs hover:bg-muted/60">
                {c}
              </button>
            ))}
          </div>
        )}
        <Field label={t('routing.ruleOutbound')}>
          {(id) => (
            <Select id={id} value={r.outbound} onChange={(e) => setR({ ...r, outbound: e.target.value as Outbound })}>
              {[...outbounds, ...customChoices(custom)].map((o) => (
                <option key={o} value={o}>
                  {outLabel(o)}
                  {o === 'warp' && warpOff ? ` (${t('routing.warpOffNote')})` : ''}
                </option>
              ))}
            </Select>
          )}
        </Field>

        {!more ? (
          <button type="button" className="text-sm text-muted-foreground underline-offset-4 hover:text-foreground hover:underline" onClick={() => setMore(true)}>
            {t('routing.moreConditions')}
          </button>
        ) : (
          <div className="space-y-4 rounded-xl bg-muted/60 p-3.5">
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label={t('routing.rulePorts')} hint={t('routing.rulePortsHint')}>
                {(id) => <Input id={id} value={r.ports} onChange={(e) => setR({ ...r, ports: e.target.value })} placeholder="443, 8000-8100" />}
              </Field>
              <Field label={t('routing.ruleNetwork')}>
                {(id) => (
                  <Select id={id} value={r.network} onChange={(e) => setR({ ...r, network: e.target.value as RoutingRule['network'] })}>
                    <option value="">{t('routing.networkAny')}</option>
                    <option value="tcp">TCP</option>
                    <option value="udp">UDP</option>
                  </Select>
                )}
              </Field>
            </div>
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={r.protocols.includes('bittorrent')}
                onChange={(e) => setR({ ...r, protocols: e.target.checked ? ['bittorrent'] : [] })}
              />
              {t('routing.onlyBittorrent')}
            </label>
            {users.length > 0 && (
              <div>
                <div className="text-sm font-medium">{t('routing.ruleUsers')}</div>
                <p className="text-xs text-muted-foreground">{t('routing.ruleUsersHint')}</p>
                <div className="mt-2 flex flex-wrap gap-2">
                  {users.map((u) => {
                    const on = r.users.includes(u.id)
                    return (
                      <button
                        key={u.id}
                        type="button"
                        aria-pressed={on}
                        onClick={() => setR({ ...r, users: on ? r.users.filter((x) => x !== u.id) : [...r.users, u.id] })}
                        className={'rounded-full border px-3 py-1 text-sm ' + (on ? 'border-primary bg-accent text-accent-foreground' : 'hover:bg-card')}
                      >
                        {u.name}
                      </button>
                    )
                  })}
                </div>
              </div>
            )}
          </div>
        )}
        {empty && <p className="text-xs text-warning">{t('routing.ruleEmpty')}</p>}
        <div className="flex justify-end gap-2 pt-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" disabled={empty}>
            {t('routing.ruleDone')}
          </Button>
        </div>
      </form>
    </Dialog>
  )
}

const dnsUpstreams = ['cloudflare', 'google', 'quad9', 'adguard', 'system', 'custom']
const extraPresets = ['yandex', 'cloudflare', 'google', 'quad9', 'adguard']

function DnsCard({ s, edit }: CardProps) {
  const { t } = useTranslation()
  const [hostsText, setHostsText] = useState(() => (s.dns.hosts ?? []).map((h) => `${h.name} ${h.ip}`).join('\n'))
  const commitHosts = (text: string) =>
    edit((d) => {
      d.dns.hosts = text
        .split('\n')
        .map((l) => l.trim().split(/\s+/))
        .filter((p) => p[0])
        .map(([name, ip]) => ({ name, ip: ip ?? '' }))
    })
  const extra = s.dns.extra ?? []
  const addRu = () =>
    edit((d) => {
      d.dns.extra = [...(d.dns.extra ?? []), { server: 'yandex', domains: ['domain:ru', 'domain:xn--p1ai', 'geosite:category-ru'] }]
    })
  const flags = ['disableCache', 'disableFallback', 'disableFallbackIfMatch', 'parallel', 'systemHosts'] as const

  return (
    <Section icon={<Globe className="h-5 w-5" />} title={t('routing.dns')} hint={t('routing.dnsHint')}>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t('routing.dnsUpstream')}>
          {(id) => (
            <Select id={id} value={s.dns.upstream} onChange={(e) => edit((d) => (d.dns.upstream = e.target.value))}>
              {dnsUpstreams.map((u) => (
                <option key={u} value={u}>
                  {t(`routing.dnsUpstreams.${u}`)}
                </option>
              ))}
            </Select>
          )}
        </Field>
        <Field label={t('routing.dnsStrategy')}>
          {(id) => (
            <Select id={id} value={s.dns.strategy} onChange={(e) => edit((d) => (d.dns.strategy = e.target.value as RoutingSettings['dns']['strategy']))}>
              {(['prefer4', 'ipv4', 'both'] as const).map((v) => (
                <option key={v} value={v}>
                  {t(`routing.dnsStrategies.${v}`)}
                </option>
              ))}
            </Select>
          )}
        </Field>
      </div>
      {s.dns.upstream === 'custom' && (
        <div className="mt-4">
          <Field label={t('routing.dnsCustom')} hint={t('routing.dnsCustomHint')}>
            {(id) => <Input id={id} value={s.dns.custom} onChange={(e) => edit((d) => (d.dns.custom = e.target.value))} placeholder="https://dns.example/dns-query" />}
          </Field>
        </div>
      )}

      <div className="mt-5 border-t pt-4">
        <div className="flex flex-wrap items-start justify-between gap-2">
          <div>
            <div className="text-sm font-medium">{t('routing.dnsExtra')}</div>
            <p className="text-xs text-muted-foreground">{t('routing.dnsExtraHint')}</p>
          </div>
          <Button size="sm" variant="secondary" onClick={() => edit((d) => (d.dns.extra = [...(d.dns.extra ?? []), { server: 'yandex', domains: [] }]))}>
            <Plus className="h-4 w-4" />
            {t('routing.dnsExtraAdd')}
          </Button>
        </div>
        {extra.length === 0 ? (
          <button type="button" onClick={addRu} className="mt-3 w-full rounded-xl border border-dashed p-3 text-left text-sm text-muted-foreground hover:bg-muted/60">
            {t('routing.dnsExtraRu')}
          </button>
        ) : (
          <div className="mt-3 space-y-3">
            {extra.map((e, i) => {
              const preset = extraPresets.includes(e.server)
              return (
                <div key={i} className="rounded-xl border p-3">
                  <div className="flex flex-wrap gap-2">
                    <Select
                      value={preset ? e.server : 'custom'}
                      onChange={(ev) => edit((d) => (d.dns.extra[i].server = ev.target.value === 'custom' ? '' : ev.target.value))}
                      className="w-auto min-w-48"
                      aria-label={t('routing.dnsUpstream')}
                    >
                      {extraPresets.map((p) => (
                        <option key={p} value={p}>
                          {t(`routing.dnsUpstreams.${p}`)}
                        </option>
                      ))}
                      <option value="custom">{t('routing.dnsUpstreams.custom')}</option>
                    </Select>
                    {!preset && (
                      <Input
                        value={e.server}
                        onChange={(ev) => edit((d) => (d.dns.extra[i].server = ev.target.value))}
                        placeholder="https://dns.example/dns-query"
                        className="min-w-0 flex-1"
                      />
                    )}
                    <Button variant="ghost" size="icon" aria-label={t('common.delete')} onClick={() => edit((d) => d.dns.extra.splice(i, 1))}>
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </div>
                  <textarea
                    rows={3}
                    value={e.domains.join('\n')}
                    onChange={(ev) => edit((d) => (d.dns.extra[i].domains = ev.target.value.split('\n')))}
                    placeholder={'domain:ru\ngeosite:category-ru'}
                    aria-label={t('routing.dnsExtraDomains')}
                    className="mt-2 w-full rounded-xl border border-input bg-card px-3 py-2 font-mono text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  />
                </div>
              )
            })}
          </div>
        )}
        <p className="mt-2 text-xs text-muted-foreground">{t('routing.dnsHysteriaNote')}</p>
      </div>

      <details className="mt-4 border-t pt-4">
        <summary className="cursor-pointer text-sm font-medium">{t('routing.dnsExpert')}</summary>
        <div className="mt-3 divide-y">
          {flags.map((k) => (
            <ToggleRow key={k} title={t(`routing.dnsFlags.${k}`)} hint={t(`routing.dnsFlags.${k}Hint`)} checked={Boolean(s.dns[k])} onChange={(v) => edit((d) => (d.dns[k] = v))} />
          ))}
        </div>
        <div className="mt-3 space-y-4">
          <Field label={t('routing.dnsClientIp')} hint={t('routing.dnsClientIpHint')}>
            {(id) => <Input id={id} value={s.dns.clientIp ?? ''} onChange={(e) => edit((d) => (d.dns.clientIp = e.target.value))} placeholder="203.0.113.1" />}
          </Field>
          <Field label={t('routing.dnsHosts')} hint={t('routing.dnsHostsHint')}>
            {(id) => (
              <textarea
                id={id}
                rows={3}
                value={hostsText}
                onChange={(e) => setHostsText(e.target.value)}
                onBlur={() => commitHosts(hostsText)}
                placeholder="router.lan 192.168.1.1"
                className="w-full rounded-xl border border-input bg-card px-3 py-2 font-mono text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring"
              />
            )}
          </Field>
        </div>
      </details>
    </Section>
  )
}

function GeoCard({ s, edit }: CardProps) {
  const { t, i18n } = useTranslation()
  const qc = useQueryClient()
  const lng = i18n.language
  const update = useMutation({ mutationFn: api.routing.updateGeo, onSettled: () => qc.invalidateQueries({ queryKey: ['routing'] }) })
  const st = s.geoState
  const fmt = (iso: string) => (iso && !iso.startsWith('0001') ? new Date(iso).toLocaleString(lng, { dateStyle: 'medium', timeStyle: 'short' }) : '—')
  return (
    <Section icon={<Database className="h-5 w-5" />} title={t('routing.geo')} hint={t('routing.geoHint')}>
      <div className="space-y-4">
        <Field label={t('routing.geoSource')} hint={t(`routing.geoSources.${s.geo.source}Hint`)}>
          {(id) => (
            <Select id={id} value={s.geo.source} onChange={(e) => edit((d) => (d.geo.source = e.target.value as RoutingSettings['geo']['source']))}>
              {(['loyalsoldier', 'runetfreedom', 'custom'] as const).map((v) => (
                <option key={v} value={v}>
                  {t(`routing.geoSources.${v}`)}
                </option>
              ))}
            </Select>
          )}
        </Field>
        {s.geo.source === 'custom' && (
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="geoip.dat">{(id) => <Input id={id} value={s.geo.geoipUrl} onChange={(e) => edit((d) => (d.geo.geoipUrl = e.target.value))} placeholder="https://…/geoip.dat" />}</Field>
            <Field label="geosite.dat">{(id) => <Input id={id} value={s.geo.geositeUrl} onChange={(e) => edit((d) => (d.geo.geositeUrl = e.target.value))} placeholder="https://…/geosite.dat" />}</Field>
          </div>
        )}
        <ToggleRow title={t('routing.geoAuto')} hint={t('routing.geoAutoHint')} checked={s.geo.autoUpdate} onChange={(v) => edit((d) => (d.geo.autoUpdate = v))} />
        <div className="rounded-xl bg-muted/60 p-3.5 text-xs text-muted-foreground">
          <div>
            {t('routing.geoUpdated')}: {fmt(st.lastUpdate)} {st.lastSource && `· ${t(`routing.geoSources.${st.lastSource}`, { defaultValue: st.lastSource })}`}
          </div>
          <div>
            {st.files.map((f) => `${f.name} ${formatBytes(f.size, lng)}`).join(' · ') || t('routing.geoNone')}
          </div>
          {st.lastError && <div className="mt-1 text-destructive">{t('routing.geoError', { error: st.lastError })}</div>}
        </div>
        <div className="flex items-center gap-3">
          <Button variant="secondary" size="sm" disabled={update.isPending} onClick={() => update.mutate()}>
            <RefreshCw className={'h-4 w-4 ' + (update.isPending ? 'animate-spin' : '')} />
            {update.isPending ? t('routing.geoUpdating') : t('routing.geoUpdateNow')}
          </Button>
          {update.isSuccess && <span className="text-xs text-muted-foreground">{update.data.changed ? t('routing.geoChanged') : t('routing.geoSame')}</span>}
          {update.isError && <span className="text-xs text-destructive">{update.error instanceof ApiError ? update.error.message : t('common.error')}</span>}
        </div>
        <p className="text-xs text-muted-foreground">{t('routing.geoSaveFirst')}</p>
      </div>
    </Section>
  )
}

function flag(code: string) {
  if (code.length !== 2) return '🌐'
  return String.fromCodePoint(...[...code.toUpperCase()].map((c) => 0x1f1e6 + c.charCodeAt(0) - 65))
}

function CountriesCard({ s, edit }: CardProps) {
  const { t, i18n } = useTranslation()
  const outLabel = useOutLabel(s.outbounds)
  const countries = useQuery({ queryKey: ['geoCountries'], queryFn: api.routing.geoCountries, staleTime: 5 * 60_000 })
  const [adding, setAdding] = useState('')
  const names = useMemo(() => {
    try {
      return new Intl.DisplayNames([i18n.language], { type: 'region' })
    } catch {
      return null
    }
  }, [i18n.language])
  const label = (code: string) => (code === 'ru-blocked' ? t('routing.ruBlocked') : (names?.of(code.toUpperCase()) ?? code.toUpperCase()))
  const list = s.presets.countries
  const used = new Set(list.map((c) => c.code))
  const options = (countries.data?.countries ?? [])
    .filter((c) => !used.has(c.code))
    .map((c) => ({ ...c, name: label(c.code) }))
    .sort((a, b) => a.name.localeCompare(b.name, i18n.language))
  const hasSite = (code: string) => code === 'ru-blocked' || countries.data?.countries.find((c) => c.code === code)?.hasSite
  const warpOff = !s.warp.registered || s.warp.mode === 'off'
  const add = (code: string, outbound: Outbound = 'warp') => edit((d) => d.presets.countries.push({ code, outbound }))
  const choices: Outbound[] = ['direct', 'warp', 'block']

  return (
    <Section icon={<Globe className="h-5 w-5" />} title={t('routing.countries')} hint={t('routing.countriesHint')}>
      {list.length === 0 ? (
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-dashed p-3.5">
          <span className="text-sm text-muted-foreground">{t('routing.countriesEmpty')}</span>
          <Button size="sm" variant="secondary" onClick={() => add('ru')}>
            {flag('ru')} {t('routing.ruViaWarp')}
          </Button>
        </div>
      ) : (
        <ul className="divide-y rounded-xl border">
          {list.map((c, i) => (
            <li key={c.code} className="flex flex-wrap items-center gap-3 p-3">
              <span className="text-lg leading-none">{flag(c.code)}</span>
              <div className="min-w-0 flex-1">
                <div className="text-sm font-medium">{label(c.code)}</div>
                <div className="text-xs text-muted-foreground">{hasSite(c.code) ? t('routing.countrySitesAndIps') : t('routing.countryIpsOnly')}</div>
              </div>
              <div className="flex rounded-xl border p-0.5">
                {[...choices, ...customChoices(s.outbounds)].map((o) => (
                  <button
                    key={o}
                    type="button"
                    aria-pressed={c.outbound === o}
                    onClick={() => edit((d) => (d.presets.countries[i].outbound = o))}
                    className={'rounded-lg px-2.5 py-1 text-xs transition ' + (c.outbound === o ? 'bg-accent font-medium text-accent-foreground' : 'text-muted-foreground hover:text-foreground')}
                  >
                    {outLabel(o)}
                  </button>
                ))}
              </div>
              <Button variant="ghost" size="icon" aria-label={t('common.delete')} onClick={() => edit((d) => d.presets.countries.splice(i, 1))}>
                <Trash2 className="h-4 w-4" />
              </Button>
            </li>
          ))}
        </ul>
      )}
      {list.some((c) => c.outbound === 'warp') && warpOff && <p className="mt-3 rounded-xl bg-warning-soft px-3.5 py-2.5 text-xs text-warning">{t('routing.countriesWarpOff')}</p>}
      <div className="mt-3 flex flex-wrap gap-2">
        <Select
          value={adding}
          onChange={(e) => {
            if (e.target.value) add(e.target.value)
            setAdding('')
          }}
          className="max-w-xs"
          aria-label={t('routing.addCountry')}
        >
          <option value="">{t('routing.addCountry')}</option>
          {!used.has('ru-blocked') && (
            <option value="ru-blocked" disabled={!countries.data?.ruBlocked}>
              🚫 {t('routing.ruBlocked')}
              {countries.data?.ruBlocked ? '' : ` (${t('routing.needsRunetfreedom')})`}
            </option>
          )}
          {options.map((c) => (
            <option key={c.code} value={c.code}>
              {flag(c.code)} {c.name}
            </option>
          ))}
        </Select>
      </div>
      <p className="mt-2 text-xs text-muted-foreground">{t('routing.countriesOrder')}</p>
    </Section>
  )
}

function OutboundsCard({ s, edit }: CardProps) {
  const { t } = useTranslation()
  const [adding, setAdding] = useState(false)
  const [name, setName] = useState('')
  const [link, setLink] = useState('')
  // Выход: по ссылке или через наш сервер ("main" или id узла).
  const [server, setServer] = useState('')
  const nodes = useQuery({ queryKey: ['nodes'], queryFn: api.nodes.list, staleTime: 30_000 })
  const serverName = (id: string) => (id === 'main' ? t('servers.thisServer') : (nodes.data?.find((n) => String(n.id) === id)?.name ?? '#' + id))
  const [checks, setChecks] = useState<Record<string, { trace?: Trace; error?: string } | 'busy'>>({})
  const check = async (id: string) => {
    setChecks((c) => ({ ...c, [id]: 'busy' }))
    try {
      const r = await api.routing.checkOutbound(id)
      setChecks((c) => ({ ...c, [id]: r }))
    } catch {
      setChecks((c) => ({ ...c, [id]: { error: t('common.error') } }))
    }
  }
  const used = (id: string) =>
    s.rules.some((r) => r.outbound === `out:${id}`) || s.presets.countries.some((c) => c.outbound === `out:${id}`)

  return (
    <Section
      icon={<Waypoints className="h-5 w-5" />}
      title={t('routing.outs')}
      hint={t('routing.outsHint')}
      actions={
        !adding && (
          <Button size="sm" variant="secondary" onClick={() => setAdding(true)}>
            <Plus className="h-4 w-4" />
            {t('routing.outsAdd')}
          </Button>
        )
      }
    >
      {s.outbounds.length === 0 && !adding && <p className="rounded-xl border border-dashed p-4 text-sm text-muted-foreground">{t('routing.outsEmpty')}</p>}
      {s.outbounds.length > 0 && (
        <ul className="divide-y rounded-xl border">
          {s.outbounds.map((o, i) => {
            const st = o.id ? checks[o.id] : undefined
            return (
              <li key={o.id || `new-${i}`} className="p-3">
                <div className="flex flex-wrap items-center gap-3">
                  <div className="min-w-0 flex-1">
                    <div className="text-sm font-medium">{o.name}</div>
                    <div className="truncate font-mono text-xs text-muted-foreground">{o.server ? t('routing.outsViaServer', { name: serverName(o.server) }) : o.summary || t('routing.outsUnsaved')}</div>
                  </div>
                  {o.id && (
                    <Button variant="ghost" size="sm" disabled={st === 'busy'} onClick={() => check(o.id)}>
                      {st === 'busy' ? t('routing.checking') : t('routing.check')}
                    </Button>
                  )}
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label={t('common.delete')}
                    title={used(o.id) ? t('routing.outsInUse') : t('common.delete')}
                    disabled={Boolean(o.id) && used(o.id)}
                    onClick={() => edit((d) => d.outbounds.splice(i, 1))}
                  >
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </div>
                {st && st !== 'busy' && (
                  <p className={'mt-1.5 text-xs ' + (st.trace ? 'text-muted-foreground' : 'text-destructive')}>
                    {st.trace ? `${st.trace.ip} · ${st.trace.country} · ${st.trace.latencyMs} ms` : t('routing.checkFailedOut', { error: st.error ?? '' })}
                  </p>
                )}
              </li>
            )
          })}
        </ul>
      )}
      {adding && (
        <div className="mt-3 space-y-3 rounded-xl bg-muted/60 p-3.5">
          <Field label={t('routing.outsName')}>
            {(id) => <Input id={id} value={name} maxLength={40} onChange={(e) => setName(e.target.value)} placeholder={t('routing.outsNamePlaceholder')} />}
          </Field>
          {(nodes.data?.length ?? 0) > 0 && (
            <Field label={t('routing.outsKind')} hint={t('routing.outsKindHint')}>
              {(id) => (
                <Select id={id} value={server} onChange={(e) => setServer(e.target.value)}>
                  <option value="">{t('routing.outsByLink')}</option>
                  <option value="main">{t('routing.outsVia', { name: t('servers.thisServer') })}</option>
                  {nodes.data?.map((n) => (
                    <option key={n.id} value={String(n.id)}>
                      {t('routing.outsVia', { name: n.name })}
                    </option>
                  ))}
                </Select>
              )}
            </Field>
          )}
          {!server && (
          <Field label={t('routing.outsLink')} hint={t('routing.outsLinkHint')}>
            {(id) => (
              <textarea
                id={id}
                rows={3}
                value={link}
                onChange={(e) => setLink(e.target.value)}
                spellCheck={false}
                placeholder="vless://…  trojan://…  ss://…  socks5://user:pass@host:port"
                className="w-full rounded-xl border border-input bg-card px-3 py-2 font-mono text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring"
              />
            )}
          </Field>
          )}
          <div className="flex justify-end gap-2">
            <Button variant="secondary" size="sm" onClick={() => setAdding(false)}>
              {t('common.cancel')}
            </Button>
            <Button
              size="sm"
              disabled={!name.trim() || (!server && !link.trim())}
              onClick={() => {
                edit((d) => d.outbounds.push({ id: '', name: name.trim(), summary: '', link: server ? '' : link.trim(), server: server || undefined }))
                setName('')
                setLink('')
                setServer('')
                setAdding(false)
              }}
            >
              {t('routing.ruleDone')}
            </Button>
          </div>
        </div>
      )}
      {s.outbounds.length > 0 && <p className="mt-3 text-xs text-muted-foreground">{t('routing.outsUseHint')}</p>}
    </Section>
  )
}

function ClientCard({ s, edit }: CardProps) {
  const { t } = useTranslation()
  const modes = ['all', 'ru-direct', 'blocked-only'] as const
  return (
    <Section icon={<Smartphone className="h-5 w-5" />} title={t('routing.client')} hint={t('routing.clientHint')}>
      <div className="grid gap-2 sm:grid-cols-3">
        {modes.map((m) => (
          <button
            key={m}
            type="button"
            onClick={() => edit((d) => (d.client = m))}
            aria-pressed={(s.client || 'all') === m}
            className={'rounded-xl border p-3 text-left transition ' + ((s.client || 'all') === m ? 'border-primary bg-accent' : 'hover:bg-muted/60')}
          >
            <div className="text-sm font-medium">{t(`routing.clientModes.${m}`)}</div>
            <div className="mt-0.5 text-xs text-muted-foreground">{t(`routing.clientModes.${m}Hint`)}</div>
          </button>
        ))}
      </div>
      <p className="mt-3 text-xs text-muted-foreground">{t('routing.clientNote')}</p>
    </Section>
  )
}

function HistoryCard({ onServer }: { onServer: (d: RoutingSettings) => void }) {
  const { t, i18n } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['routing', 'history'], queryFn: api.routing.history })
  const [confirm, setConfirm] = useState<number | null>(null)
  const restore = useMutation({
    mutationFn: (i: number) => api.routing.rollback(i),
    onSuccess: (d) => {
      onServer(d)
      setConfirm(null)
      qc.invalidateQueries({ queryKey: ['routing', 'history'] })
    },
  })
  const list = q.data ?? []
  return (
    <Section icon={<History className="h-5 w-5" />} title={t('routing.history')} hint={t('routing.historyHint')}>
      {list.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t('routing.historyEmpty')}</p>
      ) : (
        <ul className="divide-y rounded-xl border">
          {list.slice(0, 8).map((v, i) => (
            <li key={v.at} className="flex flex-wrap items-center justify-between gap-2 p-3 text-sm">
              <div>
                <div>{new Date(v.at).toLocaleString(i18n.language, { dateStyle: 'medium', timeStyle: 'short' })}</div>
                <div className="text-xs text-muted-foreground">{t('routing.historyChanged', { list: v.changed.map((c) => t(`routing.sections.${c}`)).join(', ') })}</div>
              </div>
              {confirm === i ? (
                <div className="flex gap-2">
                  <Button size="sm" variant="secondary" onClick={() => setConfirm(null)}>
                    {t('common.cancel')}
                  </Button>
                  <Button size="sm" disabled={restore.isPending} onClick={() => restore.mutate(i)}>
                    {t('routing.historyRestore')}
                  </Button>
                </div>
              ) : (
                <Button size="sm" variant="ghost" onClick={() => setConfirm(i)}>
                  {t('routing.historyRestore')}
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
    </Section>
  )
}
