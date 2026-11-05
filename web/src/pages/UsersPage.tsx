import { useMemo, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CalendarPlus, Pencil, Plus, RotateCcw, Search, Smartphone, Trash2, UserPlus } from 'lucide-react'
import { ApiError, api, type OnlineEntry, type ResetPeriod, type User, type UserInput, type UserStatus } from '@/lib/api'
import { GiB, formatBytes, formatDate, fromDateInput, toDateInput } from '@/lib/utils'
import { Badge, Button, Card, Dialog, EmptyState, Field, Input, PageHeader, Select, Switch } from '@/components/ui'
import { ShareRow } from '@/components/share'
import { TrafficChart } from '@/components/TrafficChart'
import { useKindLabel } from './InboundsPage'

const statusTone: Record<UserStatus, 'success' | 'muted' | 'warning' | 'danger'> = {
  active: 'success',
  disabled: 'muted',
  expired: 'warning',
  limited: 'danger',
}

const resetPeriods: ResetPeriod[] = ['none', 'day', 'week', 'month']

function toInput(u: User): UserInput {
  return { name: u.name, note: u.note, enabled: u.enabled, trafficLimit: u.trafficLimit, resetPeriod: u.resetPeriod, expireAt: u.expireAt, amneziaKey: u.amneziaKey, subHidden: u.subHidden }
}

export function UsersPage() {
  const { t, i18n } = useTranslation()
  const qc = useQueryClient()
  const users = useQuery({ queryKey: ['users'], queryFn: api.users.list })
  const online = useQuery({ queryKey: ['online'], queryFn: api.online, refetchInterval: 10_000 })
  const onlineBy = useMemo(() => new Map((online.data ?? []).map((o) => [o.userId, o])), [online.data])
  const [details, setDetails] = useState<User | null>(null)
  const [query, setQuery] = useState('')
  const [editing, setEditing] = useState<User | 'new' | null>(null)
  const [deleting, setDeleting] = useState<User | null>(null)
  const [connecting, setConnecting] = useState<User | null>(null)

  const invalidate = () => qc.invalidateQueries({ queryKey: ['users'] })
  const toggle = useMutation({
    mutationFn: (u: User) => api.users.update(u.id, { ...toInput(u), enabled: !u.enabled }),
    onSuccess: invalidate,
  })
  const remove = useMutation({
    mutationFn: (u: User) => api.users.remove(u.id),
    onSuccess: () => {
      setDeleting(null)
      invalidate()
    },
  })

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    const list = users.data ?? []
    return q ? list.filter((u) => u.name.toLowerCase().includes(q) || u.note.toLowerCase().includes(q)) : list
  }, [users.data, query])

  const lng = i18n.language
  const addButton = (
    <Button onClick={() => setEditing('new')}>
      <Plus className="h-4 w-4" />
      {t('users.add')}
    </Button>
  )

  return (
    <>
      <PageHeader title={t('users.title')} subtitle={t('users.subtitle')} actions={users.data?.length ? addButton : undefined} />

      <Card className="p-0">
        {users.isPending ? (
          <p className="p-6 text-sm text-muted-foreground">{t('common.loading')}</p>
        ) : users.isError ? (
          <div className="p-6 text-sm">
            <p className="text-destructive">{t('common.error')}</p>
            <Button variant="secondary" size="sm" className="mt-3" onClick={() => users.refetch()}>
              {t('common.retry')}
            </Button>
          </div>
        ) : users.data.length === 0 ? (
          <EmptyState icon={<UserPlus className="h-6 w-6" />} title={t('users.emptyTitle')} text={t('users.emptyText')} action={addButton} />
        ) : (
          <>
            <div className="border-b p-4">
              <div className="relative max-w-sm">
                <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                <Input value={query} onChange={(e) => setQuery(e.target.value)} placeholder={t('users.search')} className="pl-9" />
              </div>
            </div>
            {filtered.length === 0 ? (
              <p className="p-6 text-sm text-muted-foreground">{t('users.nothingFound')}</p>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="text-left text-xs uppercase tracking-wide text-muted-foreground">
                      <th className="px-5 py-3 font-medium">{t('users.colName')}</th>
                      <th className="px-5 py-3 font-medium">{t('users.colStatus')}</th>
                      <th className="px-5 py-3 font-medium">{t('users.colTraffic')}</th>
                      <th className="px-5 py-3 font-medium">{t('users.colExpire')}</th>
                      <th className="px-5 py-3 text-right font-medium">{t('users.colActions')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {filtered.map((u) => (
                      <tr key={u.id} className="border-t transition hover:bg-muted/50">
                        <td className="px-5 py-3.5">
                          <button type="button" className="flex items-center gap-2 text-left font-medium hover:underline underline-offset-4" onClick={() => setDetails(u)}>
                            <OnlineDot entry={onlineBy.get(u.id)} />
                            {u.name}
                          </button>
                          {u.note && <div className="text-xs text-muted-foreground">{u.note}</div>}
                        </td>
                        <td className="px-5 py-3.5">
                          <Badge tone={statusTone[u.status]}>{t(`users.status.${u.status}`)}</Badge>
                        </td>
                        <td className="px-5 py-3.5 whitespace-nowrap">
                          {formatBytes(u.trafficUsed, lng)}
                          <span className="text-muted-foreground"> / {u.trafficLimit ? formatBytes(u.trafficLimit, lng) : t('common.unlimited')}</span>
                        </td>
                        <td className="px-5 py-3.5 whitespace-nowrap text-muted-foreground">
                          {u.expireAt ? formatDate(u.expireAt, lng) : t('common.never')}
                        </td>
                        <td className="px-5 py-3.5">
                          <div className="flex items-center justify-end gap-1">
                            <Switch
                              checked={u.enabled}
                              label={u.enabled ? t('users.disable') : t('users.enable')}
                              disabled={toggle.isPending}
                              onChange={() => toggle.mutate(u)}
                            />
                            <Button variant="ghost" size="icon" aria-label={t('users.connect')} title={t('users.connect')} onClick={() => setConnecting(u)}>
                              <Smartphone className="h-4 w-4" />
                            </Button>
                            <Button variant="ghost" size="icon" aria-label={t('common.edit')} title={t('common.edit')} onClick={() => setEditing(u)}>
                              <Pencil className="h-4 w-4" />
                            </Button>
                            <Button variant="ghost" size="icon" aria-label={t('common.delete')} title={t('common.delete')} onClick={() => setDeleting(u)}>
                              <Trash2 className="h-4 w-4" />
                            </Button>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </>
        )}
      </Card>

      {editing && <UserDialog user={editing === 'new' ? null : editing} onClose={() => setEditing(null)} onSaved={invalidate} />}
      {connecting && <ConnectDialog user={connecting} onClose={() => setConnecting(null)} />}
      {details && (
        <UserDetails
          user={users.data?.find((u) => u.id === details.id) ?? details}
          online={onlineBy.get(details.id)}
          onClose={() => setDetails(null)}
          onEdit={() => {
            setEditing(users.data?.find((u) => u.id === details.id) ?? details)
            setDetails(null)
          }}
        />
      )}

      <Dialog
        open={deleting !== null}
        onClose={() => setDeleting(null)}
        title={t('users.deleteTitle', { name: deleting?.name ?? '' })}
        description={t('users.deleteText')}
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

function ConnectDialog({ user, onClose }: { user: User; onClose: () => void }) {
  const { t } = useTranslation()
  const kindLabel = useKindLabel()
  const links = useQuery({ queryKey: ['users', user.id, 'links'], queryFn: () => api.users.links(user.id) })
  return (
    <Dialog open onClose={onClose} title={t('connect.title', { name: user.name })}>
      {links.isPending ? (
        <p className="text-sm text-muted-foreground">{t('common.loading')}</p>
      ) : links.isError ? (
        <p className="text-sm text-destructive">{t('common.error')}</p>
      ) : (
        <div className="max-h-[70vh] space-y-4 overflow-y-auto pr-1">
          {user.status !== 'active' && (
            <p className="rounded-xl bg-warning-soft px-3.5 py-2.5 text-sm text-warning">{t('connect.disabledUser')}</p>
          )}
          {(() => {
            // Which methods each subscription carries, named the way the admin named them.
            const names = (family: 'xray' | 'singbox') =>
              links.data.links
                .filter((l) => l.inSub && l.apps.includes(family) && (family === 'singbox' || l.url))
                .map((l) => l.remark)
                .join(', ') || '—'
            return (
              <div className="space-y-2">
                <div className="text-sm font-medium">{t('connect.subscriptions')}</div>
                <p className="-mt-1 text-xs text-muted-foreground">{t('connect.subscriptionsHint')}</p>
                {links.data.overLan && <p className="rounded-xl bg-accent px-3.5 py-2.5 text-xs text-accent-foreground">{t('connect.overLan')}</p>}
                <ShareRow label={t('connect.subXray')} hint={t('connect.subXrayHint', { list: names('xray') })} text={links.data.subscriptionUrl} />
                <ShareRow label={t('connect.subSingbox')} hint={t('connect.subSingboxHint', { list: names('singbox') })} text={links.data.singboxUrl} />
              </div>
            )
          })()}
          <details className="border-t pt-3">
            <summary className="cursor-pointer text-sm font-medium">{t('connect.links')}</summary>
            <p className="mb-2 mt-1 text-xs text-muted-foreground">{t('connect.linksHint')}</p>
            {links.data.links.length === 0 ? (
              <p className="text-sm text-muted-foreground">{t('connect.noLinks')}</p>
            ) : (
              <div className="space-y-2">
                {links.data.links.map((l) => {
                  // The remark defaults to the kind; show the kind only when the
                  // admin named the inbound something else entirely.
                  const kind = kindLabel(l.protocol, l.transport)
                  const named = l.remark.toLowerCase().includes(t(`inbounds.protocols.${l.protocol}.title`).split(' ')[0].toLowerCase())
                  const label = named ? l.remark : `${l.remark} · ${kind}`
                  const works =
                    t('connect.worksIn', { apps: l.apps.map((a) => t(`inbounds.apps.${a}`)).join('; ') }) +
                    (l.inSub ? '' : ` · ${t('connect.notInSub')}`)
                  return l.url ? (
                    <ShareRow key={l.inboundId} label={label} hint={works} text={l.url} />
                  ) : (
                    <div key={l.inboundId} className="rounded-xl border border-dashed p-3.5">
                      <div className="text-sm font-medium">{label}</div>
                      <p className="mt-1 text-xs text-muted-foreground">{t('connect.onlySingbox')}</p>
                    </div>
                  )
                })}
              </div>
            )}
          </details>
          {user.amneziaKey && (
            <div>
              <div className="text-sm font-medium">Amnezia</div>
              <p className="mb-2 text-xs text-muted-foreground">{t('connect.amneziaHint')}</p>
              <ShareRow label={t('connect.amneziaKey')} text={user.amneziaKey} />
            </div>
          )}
          <div className="flex justify-end">
            <Button variant="secondary" onClick={onClose}>
              OK
            </Button>
          </div>
        </div>
      )}
    </Dialog>
  )
}

function UserDialog({ user, onClose, onSaved }: { user: User | null; onClose: () => void; onSaved: () => void }) {
  const { t } = useTranslation()
  const [name, setName] = useState(user?.name ?? '')
  const [note, setNote] = useState(user?.note ?? '')
  const [limitGb, setLimitGb] = useState(user ? String(+(user.trafficLimit / GiB).toFixed(2)) : '0')
  const [resetPeriod, setResetPeriod] = useState<ResetPeriod>(user?.resetPeriod ?? 'none')
  const [expire, setExpire] = useState(toDateInput(user?.expireAt ?? null))
  const [enabled, setEnabled] = useState(user?.enabled ?? true)
  const [amneziaKey, setAmneziaKey] = useState(user?.amneziaKey ?? '')
  const [subHidden, setSubHidden] = useState<number[]>(user?.subHidden ?? [])
  const inbounds = useQuery({ queryKey: ['inbounds'], queryFn: api.inbounds.list })
  const kindLabel = useKindLabel()
  const [errors, setErrors] = useState<Record<string, string>>({})

  const save = useMutation({
    mutationFn: (input: UserInput) => (user ? api.users.update(user.id, input) : api.users.create(input)),
    onSuccess: () => {
      onSaved()
      onClose()
    },
    onError: (err) => {
      if (err instanceof ApiError && err.status === 409) setErrors({ name: t('users.nameTaken') })
      else if (err instanceof ApiError && err.field) setErrors({ [err.field]: err.message })
      else setErrors({ form: t('common.error') })
    },
  })

  function submit(e: FormEvent) {
    e.preventDefault()
    setErrors({})
    const gb = Math.max(0, Number(limitGb.replace(',', '.')) || 0)
    save.mutate({ name, note, enabled, resetPeriod, trafficLimit: Math.round(gb * GiB), expireAt: fromDateInput(expire), amneziaKey, subHidden })
  }

  return (
    <Dialog open onClose={onClose} title={user ? t('users.editTitle', { name: user.name }) : t('users.createTitle')}>
      <form onSubmit={submit} className="space-y-4">
        <Field label={t('users.name')} hint={t('users.nameHint')} error={errors.name}>
          {(id) => <Input id={id} value={name} onChange={(e) => setName(e.target.value)} maxLength={64} autoFocus required />}
        </Field>
        <Field label={t('users.note')} error={errors.note}>
          {(id) => <Input id={id} value={note} onChange={(e) => setNote(e.target.value)} placeholder={t('users.notePlaceholder')} maxLength={500} />}
        </Field>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={t('users.trafficLimit')} hint={t('users.trafficLimitHint')} error={errors.trafficLimit}>
            {(id) => <Input id={id} inputMode="decimal" value={limitGb} onChange={(e) => setLimitGb(e.target.value)} />}
          </Field>
          <Field label={t('users.resetPeriod')} error={errors.resetPeriod}>
            {(id) => (
              <Select id={id} value={resetPeriod} onChange={(e) => setResetPeriod(e.target.value as ResetPeriod)}>
                {resetPeriods.map((p) => (
                  <option key={p} value={p}>
                    {t(`users.reset.${p}`)}
                  </option>
                ))}
              </Select>
            )}
          </Field>
        </div>
        <Field label={t('users.expireAt')} hint={t('users.expireAtHint')} error={errors.expireAt}>
          {(id) => <Input id={id} type="date" value={expire} onChange={(e) => setExpire(e.target.value)} />}
        </Field>
        {inbounds.data && inbounds.data.length > 0 && (
          <div>
            <div className="text-sm font-medium">{t('users.inSub')}</div>
            <p className="mb-2 text-xs text-muted-foreground">{t('users.inSubHint')}</p>
            <div className="divide-y rounded-xl border">
              {inbounds.data.map((ib) => (
                <div key={ib.id} className="flex items-center justify-between gap-3 px-3.5 py-2.5">
                  <div className="min-w-0">
                    <div className="truncate text-sm">{ib.remark}</div>
                    <div className="truncate text-xs text-muted-foreground">
                      {kindLabel(ib.protocol, ib.transport)}
                      {!ib.enabled && ` · ${t('users.inboundOff')}`}
                    </div>
                  </div>
                  <Switch
                    checked={!subHidden.includes(ib.id)}
                    onChange={(on) => setSubHidden((h) => (on ? h.filter((x) => x !== ib.id) : [...h, ib.id]))}
                    label={ib.remark}
                  />
                </div>
              ))}
            </div>
          </div>
        )}
        <Field label={t('users.amneziaKey')} hint={t('users.amneziaKeyHint')} error={errors.amneziaKey}>
          {(id) => (
            <textarea
              id={id}
              rows={2}
              value={amneziaKey}
              onChange={(e) => setAmneziaKey(e.target.value)}
              placeholder="vpn://…"
              spellCheck={false}
              className="w-full rounded-xl border border-input bg-card px-3.5 py-2.5 font-mono text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring"
            />
          )}
        </Field>
        <div className="flex items-start justify-between gap-4 rounded-xl bg-muted/60 p-3.5">
          <div>
            <div className="text-sm font-medium">{t('users.enabled')}</div>
            <div className="text-xs text-muted-foreground">{t('users.enabledHint')}</div>
          </div>
          <Switch checked={enabled} onChange={setEnabled} label={t('users.enabled')} />
        </div>
        {errors.form && <p className="text-sm text-destructive">{errors.form}</p>}
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

function OnlineDot({ entry }: { entry?: OnlineEntry }) {
  const { t } = useTranslation()
  const label = entry ? t('users.onlineNow', { ips: entry.ips.join(', ') || '—' }) : t('users.offline')
  return (
    <span
      role="img"
      aria-label={label}
      title={label}
      className={entry ? 'h-2 w-2 shrink-0 rounded-full bg-success' : 'h-2 w-2 shrink-0 rounded-full border border-muted-foreground/50'}
    />
  )
}

function UserDetails({ user, online, onClose, onEdit }: { user: User; online?: OnlineEntry; onClose: () => void; onEdit: () => void }) {
  const { t, i18n } = useTranslation()
  const lng = i18n.language
  const qc = useQueryClient()
  const traffic = useQuery({ queryKey: ['traffic', 30, user.id], queryFn: () => api.users.traffic(user.id, 30), refetchInterval: 60_000 })
  const [confirmReset, setConfirmReset] = useState(false)
  const done = () => {
    qc.invalidateQueries({ queryKey: ['users'] })
    qc.invalidateQueries({ queryKey: ['traffic'] })
  }
  const extend = useMutation({ mutationFn: () => api.users.extend(user.id, 30), onSuccess: done })
  const reset = useMutation({
    mutationFn: () => api.users.resetTraffic(user.id),
    onSuccess: () => {
      setConfirmReset(false)
      done()
    },
  })
  const expired = user.expireAt !== null && new Date(user.expireAt) < new Date()

  return (
    <Dialog open onClose={onClose} title={user.name} description={user.note || undefined}>
      <div className="max-h-[75vh] space-y-5 overflow-y-auto pr-1">
        <div className="grid grid-cols-2 gap-3 text-sm sm:grid-cols-3">
          <div className="rounded-xl bg-muted/60 p-3">
            <div className="text-xs text-muted-foreground">{t('users.colStatus')}</div>
            <div className="mt-1">
              <Badge tone={statusTone[user.status]}>{t(`users.status.${user.status}`)}</Badge>
            </div>
          </div>
          <div className="rounded-xl bg-muted/60 p-3">
            <div className="text-xs text-muted-foreground">{t('users.colTraffic')}</div>
            <div className="mt-1 font-medium whitespace-nowrap">
              {formatBytes(user.trafficUsed, lng)}
              <span className="font-normal text-muted-foreground"> / {user.trafficLimit ? formatBytes(user.trafficLimit, lng) : '∞'}</span>
            </div>
            {user.resetPeriod !== 'none' && <div className="text-xs text-muted-foreground">{t(`users.reset.${user.resetPeriod}`)}</div>}
          </div>
          <div className="col-span-2 rounded-xl bg-muted/60 p-3 sm:col-span-1">
            <div className="text-xs text-muted-foreground">{t('users.colExpire')}</div>
            <div className={expired ? 'mt-1 font-medium text-warning' : 'mt-1 font-medium'}>
              {user.expireAt ? formatDate(user.expireAt, lng) : t('common.never')}
            </div>
          </div>
        </div>

        <div className="rounded-xl border p-3.5 text-sm">
          <div className="flex items-center gap-2 font-medium">
            <OnlineDot entry={online} />
            {online ? t('users.online') : t('users.offline')}
          </div>
          <p className="mt-1 text-xs text-muted-foreground">
            {online ? t('users.onlineIps', { ips: online.ips.join(', ') || '—' }) : t('users.offlineHint')}
          </p>
        </div>

        <SubFetches userId={user.id} />

        <div>
          <div className="text-sm font-medium">{t('users.traffic30')}</div>
          <div className="mt-2">{traffic.data ? <TrafficChart data={traffic.data} height={150} /> : <div className="h-[150px]" />}</div>
        </div>

        {confirmReset ? (
          <div className="rounded-xl bg-warning-soft p-3.5 text-sm">
            <p>{t('users.resetConfirm', { used: formatBytes(user.trafficUsed, lng) })}</p>
            <div className="mt-3 flex justify-end gap-2">
              <Button variant="secondary" size="sm" onClick={() => setConfirmReset(false)}>
                {t('common.cancel')}
              </Button>
              <Button variant="danger" size="sm" disabled={reset.isPending} onClick={() => reset.mutate()}>
                {t('users.resetTraffic')}
              </Button>
            </div>
          </div>
        ) : (
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex flex-wrap gap-2">
              <Button
                variant="secondary"
                size="sm"
                disabled={!user.expireAt || extend.isPending}
                title={user.expireAt ? undefined : t('users.extendNoExpiry')}
                onClick={() => extend.mutate()}
              >
                <CalendarPlus className="h-4 w-4" />
                {t('users.extend30')}
              </Button>
              <Button variant="secondary" size="sm" disabled={user.trafficUsed === 0} onClick={() => setConfirmReset(true)}>
                <RotateCcw className="h-4 w-4" />
                {t('users.resetTraffic')}
              </Button>
            </div>
            <div className="flex gap-2">
              <Button variant="ghost" size="sm" onClick={onEdit}>
                <Pencil className="h-4 w-4" />
                {t('common.edit')}
              </Button>
              <Button variant="secondary" size="sm" onClick={onClose}>
                OK
              </Button>
            </div>
          </div>
        )}
        {(extend.isError || reset.isError) && <p className="text-sm text-destructive">{t('common.error')}</p>}
      </div>
    </Dialog>
  )
}

// appName — название приложения из User-Agent: «v2RayTun/5.10», «Hiddify/2.5».
function appName(ua: string) {
  if (!ua) return '?'
  return ua.split(/[\s;(]/)[0].slice(0, 40)
}

// SubFetches — когда и каким приложением пользователь обновлял подписку.
function SubFetches({ userId }: { userId: number }) {
  const { t, i18n } = useTranslation()
  const q = useQuery({ queryKey: ['sub-fetches', userId], queryFn: () => api.users.fetches(userId) })
  const list = q.data ?? []
  return (
    <div className="rounded-xl border p-3.5 text-sm">
      <div className="font-medium">{t('users.fetches')}</div>
      {list.length === 0 ? (
        <p className="mt-1 text-xs text-muted-foreground">{q.isPending ? t('common.loading') : t('users.fetchesNone')}</p>
      ) : (
        <ul className="mt-1.5 space-y-1 text-xs">
          {list.slice(0, 5).map((f, i) => (
            <li key={i} className="flex flex-wrap justify-between gap-x-3 text-muted-foreground">
              <span>
                <span className="font-medium text-foreground">{appName(f.app)}</span> · {f.ip}
              </span>
              <span className="tabular-nums" title={f.app}>
                {new Date(f.at).toLocaleString(i18n.language)}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
