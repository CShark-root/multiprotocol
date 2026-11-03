import { useMemo, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Pencil, Plus, Search, Smartphone, Trash2, UserPlus } from 'lucide-react'
import { ApiError, api, type ResetPeriod, type User, type UserInput, type UserStatus } from '@/lib/api'
import { GiB, formatBytes, formatDate, fromDateInput, toDateInput } from '@/lib/utils'
import { Badge, Button, Card, Dialog, EmptyState, Field, Input, PageHeader, Select, Switch } from '@/components/ui'
import { ShareRow } from '@/components/share'

const statusTone: Record<UserStatus, 'success' | 'muted' | 'warning' | 'danger'> = {
  active: 'success',
  disabled: 'muted',
  expired: 'warning',
  limited: 'danger',
}

const resetPeriods: ResetPeriod[] = ['none', 'day', 'week', 'month']

function toInput(u: User): UserInput {
  return { name: u.name, note: u.note, enabled: u.enabled, trafficLimit: u.trafficLimit, resetPeriod: u.resetPeriod, expireAt: u.expireAt }
}

export function UsersPage() {
  const { t, i18n } = useTranslation()
  const qc = useQueryClient()
  const users = useQuery({ queryKey: ['users'], queryFn: api.users.list })
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
                          <div className="font-medium">{u.name}</div>
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
          <ShareRow label={t('connect.subscription')} hint={t('connect.subscriptionHint')} text={links.data.subscriptionUrl} />
          <div>
            <div className="text-sm font-medium">{t('connect.links')}</div>
            <p className="mb-2 text-xs text-muted-foreground">{t('connect.linksHint')}</p>
            {links.data.links.length === 0 ? (
              <p className="text-sm text-muted-foreground">{t('connect.noLinks')}</p>
            ) : (
              <div className="space-y-2">
                {links.data.links.map((l) => {
                  const kind = t(`inbounds.transports.${l.transport}.title`)
                  return <ShareRow key={l.inboundId} label={l.remark === kind ? kind : `${l.remark} · ${kind}`} text={l.url} />
                })}
              </div>
            )}
          </div>
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
    save.mutate({ name, note, enabled, resetPeriod, trafficLimit: Math.round(gb * GiB), expireAt: fromDateInput(expire) })
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
