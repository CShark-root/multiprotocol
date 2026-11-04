import { useEffect, useState, type FormEvent, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Archive, Download, Globe, KeyRound, Monitor, RefreshCw, ShieldCheck, ShieldOff } from 'lucide-react'
import { ApiError, api, basePath, type PanelSettings } from '@/lib/api'
import { Badge, Button, Card, Dialog, Field, Input, PageHeader, Select } from '@/components/ui'
import { CopyButton, QrImage } from '@/components/share'
import { cn } from '@/lib/utils'

const tabs = ['access', 'security', 'maintenance'] as const
type Tab = (typeof tabs)[number]

function savedTab(): Tab {
  try {
    const v = localStorage.getItem('settings.tab')
    return tabs.includes(v as Tab) ? (v as Tab) : 'access'
  } catch {
    return 'access'
  }
}

export function SettingsPage() {
  const { t } = useTranslation()
  const [tab, setTab] = useState<Tab>(savedTab)
  const choose = (v: Tab) => {
    setTab(v)
    try {
      localStorage.setItem('settings.tab', v)
    } catch {
      // private mode: the tab is just not remembered
    }
  }
  return (
    <>
      <PageHeader title={t('settings.title')} subtitle={t('settings.subtitle')} />
      <div className="mx-auto max-w-3xl">
        <div role="tablist" className="mb-5 flex gap-1 rounded-2xl border bg-card p-1">
          {tabs.map((v) => (
            <button
              key={v}
              role="tab"
              type="button"
              aria-selected={tab === v}
              onClick={() => choose(v)}
              className={cn(
                'flex-1 rounded-xl px-3 py-2 text-sm transition',
                tab === v ? 'bg-accent font-medium text-accent-foreground' : 'text-muted-foreground hover:text-foreground',
              )}
            >
              {t(`settings.tab_${v}`)}
            </button>
          ))}
        </div>
        <div className="space-y-4">
          {tab === 'access' && (
            <>
              <AddressCard />
              <DomainCard />
            </>
          )}
          {tab === 'security' && (
            <>
              <TotpCard />
              <PasswordCard />
              <SessionsCard />
            </>
          )}
          {tab === 'maintenance' && (
            <>
              <UpdateCard />
              <BackupCard />
            </>
          )}
        </div>
      </div>
    </>
  )
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
        {actions && <div className="shrink-0">{actions}</div>}
      </div>
      <div className="mt-4">{children}</div>
    </Card>
  )
}

/** Asks for the admin password right before a sensitive action. */
function PasswordPrompt({
  open,
  title,
  text,
  confirm,
  danger,
  busy,
  error,
  onConfirm,
  onClose,
}: {
  open: boolean
  title: string
  text?: string
  confirm: string
  danger?: boolean
  busy?: boolean
  error?: string
  onConfirm: (password: string) => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [password, setPassword] = useState('')
  useEffect(() => {
    if (!open) setPassword('')
  }, [open])
  return (
    <Dialog open={open} onClose={onClose} title={title} description={text}>
      <form
        className="space-y-4"
        onSubmit={(e: FormEvent) => {
          e.preventDefault()
          onConfirm(password)
        }}
      >
        <Field label={t('settings.currentPassword')} error={error}>
          {(id) => <Input id={id} type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" autoFocus required />}
        </Field>
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" variant={danger ? 'danger' : 'primary'} disabled={busy || !password}>
            {confirm}
          </Button>
        </div>
      </form>
    </Dialog>
  )
}

const randomPath = () => {
  const abc = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789'
  const b = crypto.getRandomValues(new Uint8Array(20))
  return Array.from(b, (x) => abc[x % abc.length]).join('')
}

function fieldError(err: unknown, field: string) {
  return err instanceof ApiError && err.field === field ? err.message : undefined
}

function AddressCard() {
  const { t } = useTranslation()
  const q = useQuery({ queryKey: ['settings', 'panel'], queryFn: api.settings.panel })
  const [form, setForm] = useState<PanelSettings | null>(null)
  const [asking, setAsking] = useState(false)
  const [newUrl, setNewUrl] = useState('')
  const [restarting, setRestarting] = useState(false)
  useEffect(() => {
    if (q.data && !form) setForm(q.data.settings)
  }, [q.data, form])

  const save = useMutation({
    mutationFn: (password: string) => api.settings.savePanel({ ...form!, password }),
    // A port or address problem is shown at its field: close the prompt.
    onError: (e) => e instanceof ApiError && ['port', 'publicHost'].includes(e.field ?? '') && setAsking(false),
    onSuccess: (r) => {
      setNewUrl(r.url)
      setAsking(false)
      q.refetch()
    },
  })
  const restart = useMutation({ mutationFn: api.settings.restart, onSuccess: () => setRestarting(true) })

  if (!form || !q.data) return <Card>{t('common.loading')}</Card>
  const set = <K extends keyof PanelSettings>(k: K, v: PanelSettings[K]) => setForm({ ...form, [k]: v })
  const err = (f: string) => fieldError(save.error, f)
  const generalError = save.error instanceof ApiError && !['port', 'password', 'publicHost'].includes(save.error.field ?? '') ? save.error.message : ''

  return (
    <Section icon={<Monitor className="h-4 w-4" />} title={t('settings.address')} hint={t('settings.addressHint')}>
      <form
        className="space-y-4"
        onSubmit={(e: FormEvent) => {
          e.preventDefault()
          save.reset()
          setAsking(true)
        }}
      >
        <Field label={t('settings.publicHost')} hint={t('settings.publicHostHint')} error={err('publicHost')}>
          {(id) => <Input id={id} value={form.publicHost} onChange={(e) => set('publicHost', e.target.value.trim())} />}
        </Field>
        <div className="grid gap-4 sm:grid-cols-[8rem_1fr]">
          <Field label={t('settings.port')} error={err('port')}>
            {(id) => <Input id={id} inputMode="numeric" value={String(form.port)} onChange={(e) => set('port', Number(e.target.value.replace(/\D/g, '')) || 0)} />}
          </Field>
          <Field label={t('settings.decoy')}>
            {(id) => (
              <Select id={id} value={form.decoy} onChange={(e) => set('decoy', e.target.value as PanelSettings['decoy'])}>
                <option value="404">{t('settings.decoy404')}</option>
                <option value="drop">{t('settings.decoyDrop')}</option>
              </Select>
            )}
          </Field>
        </div>
        {(['basePath', 'subPath'] as const).map((k) => (
          <Field key={k} label={t(`settings.${k}`)} hint={t(`settings.${k}Hint`)}>
            {(id) => (
              <div className="flex gap-2">
                <Input id={id} value={form[k]} onChange={(e) => set(k, e.target.value.trim())} className="font-mono" />
                <Button type="button" variant="secondary" className="shrink-0" onClick={() => set(k, randomPath())} title={t('settings.regenerate')}>
                  <RefreshCw className="h-4 w-4" />
                </Button>
              </div>
            )}
          </Field>
        ))}
        {!asking && generalError && <p className="text-sm text-destructive">{generalError}</p>}
        <Button type="submit" disabled={save.isPending}>
          {t('settings.save')}
        </Button>
      </form>

      <PasswordPrompt
        open={asking}
        title={t('settings.confirmTitle')}
        text={t('settings.confirmPasswordHint')}
        confirm={t('settings.save')}
        busy={save.isPending}
        error={err('password') ? t('settings.wrongPassword') : generalError || undefined}
        onConfirm={(pw) => save.mutate(pw)}
        onClose={() => setAsking(false)}
      />

      {(newUrl || q.data.pending) && (
        <div className="mt-4 space-y-3 rounded-xl bg-accent p-4 text-sm text-accent-foreground">
          <p>{newUrl ? t('settings.saved') : t('settings.pending')}</p>
          {newUrl && (
            <div className="flex items-center gap-2">
              <code className="min-w-0 flex-1 truncate rounded-lg bg-card px-2.5 py-1.5 text-xs text-foreground">{newUrl}</code>
              <CopyButton text={newUrl} />
            </div>
          )}
          {restarting ? (
            <p className="font-medium">
              {t('settings.restarting')}{' '}
              {newUrl && (
                <a href={newUrl} className="underline">
                  {newUrl}
                </a>
              )}
            </p>
          ) : q.data.restartSupported ? (
            <Button variant="primary" size="sm" disabled={restart.isPending} onClick={() => restart.mutate()}>
              <RefreshCw className="h-4 w-4" />
              {t('settings.restart')}
            </Button>
          ) : (
            <p className="text-xs">{t('settings.restartManual')}</p>
          )}
        </div>
      )}
    </Section>
  )
}

function PasswordCard() {
  const { t } = useTranslation()
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const change = useMutation({
    mutationFn: () => api.account.changePassword(current, next),
    onSuccess: () => {
      setCurrent('')
      setNext('')
    },
  })
  return (
    <Section icon={<KeyRound className="h-4 w-4" />} title={t('settings.password')} hint={t('settings.passwordHint')}>
      <form
        className="space-y-3"
        onSubmit={(e: FormEvent) => {
          e.preventDefault()
          change.mutate()
        }}
      >
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label={t('settings.currentPassword')} error={change.error instanceof ApiError && change.error.code === 'wrong_password' ? t('settings.wrongPassword') : undefined}>
            {(id) => <Input id={id} type="password" value={current} onChange={(e) => setCurrent(e.target.value)} autoComplete="current-password" required />}
          </Field>
          <Field label={t('settings.newPassword')} hint={t('auth.passwordHint')} error={fieldError(change.error, 'password')}>
            {(id) => <Input id={id} type="password" value={next} onChange={(e) => setNext(e.target.value)} autoComplete="new-password" minLength={10} required />}
          </Field>
        </div>
        {change.isSuccess && <p className="text-sm text-success">{t('settings.passwordChanged')}</p>}
        <Button type="submit" variant="secondary" disabled={change.isPending}>
          {t('settings.changePassword')}
        </Button>
      </form>
    </Section>
  )
}

function TotpCard() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const account = useQuery({ queryKey: ['account'], queryFn: api.account.get })
  const [setup, setSetup] = useState<{ secret: string; uri: string } | null>(null)
  const [disabling, setDisabling] = useState(false)
  const [code, setCode] = useState('')
  const [password, setPassword] = useState('')
  const refresh = () => qc.invalidateQueries({ queryKey: ['account'] })
  const begin = useMutation({ mutationFn: api.account.totpSetup, onSuccess: (s) => setSetup(s) })
  const enable = useMutation({
    mutationFn: () => api.account.totpEnable(setup!.secret, code),
    onSuccess: () => {
      setSetup(null)
      setCode('')
      refresh()
    },
  })
  const disable = useMutation({
    mutationFn: () => api.account.totpDisable(password, code),
    onSuccess: () => {
      setDisabling(false)
      setPassword('')
      setCode('')
      refresh()
    },
  })
  const on = account.data?.totpEnabled
  const codeInput = (id: string) => (
    <Input
      id={id}
      value={code}
      onChange={(e) => setCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
      inputMode="numeric"
      autoComplete="one-time-code"
      className="text-center font-mono tracking-[0.3em]"
      required
    />
  )
  const errText = (e: unknown) =>
    e instanceof ApiError ? (e.code === 'invalid_totp' ? t('auth.invalidCode') : e.code === 'wrong_password' ? t('settings.wrongPassword') : e.message) : ''

  return (
    <Section icon={on ? <ShieldCheck className="h-4 w-4" /> : <ShieldOff className="h-4 w-4" />} title={t('settings.totp')}>
      <div className="flex items-center gap-2 text-sm">
        <Badge tone={on ? 'success' : 'warning'}>{on ? 'ON' : 'OFF'}</Badge>
        <span className="text-muted-foreground">{on ? t('settings.totpOn') : t('settings.totpOff')}</span>
      </div>

      {!on && !setup && (
        <Button className="mt-4" onClick={() => begin.mutate()} disabled={begin.isPending}>
          {t('settings.totpEnable')}
        </Button>
      )}
      {!on && setup && (
        <form
          className="mt-4 space-y-3"
          onSubmit={(e: FormEvent) => {
            e.preventDefault()
            enable.mutate()
          }}
        >
          <p className="text-sm text-muted-foreground">{t('settings.totpScan')}</p>
          <div className="flex justify-center">
            <QrImage text={setup.uri} size={180} />
          </div>
          <details className="text-xs text-muted-foreground">
            <summary className="cursor-pointer">{t('settings.totpManual')}</summary>
            <div className="mt-2 flex items-center gap-2">
              <code className="min-w-0 flex-1 break-all rounded-lg bg-muted px-2 py-1">{setup.secret}</code>
              <CopyButton text={setup.secret} />
            </div>
          </details>
          <Field label={t('settings.code')} error={errText(enable.error) || undefined}>
            {codeInput}
          </Field>
          <Button type="submit" disabled={enable.isPending || code.length !== 6}>
            {t('settings.totpConfirm')}
          </Button>
        </form>
      )}
      {on && (
        <Button className="mt-4" variant="secondary" size="sm" onClick={() => setDisabling(true)}>
          {t('settings.totpDisable')}
        </Button>
      )}
      <Dialog open={disabling} onClose={() => setDisabling(false)} title={t('settings.totpDisableTitle')} description={t('settings.totpDisableHint')}>
        <form
          className="space-y-4"
          onSubmit={(e: FormEvent) => {
            e.preventDefault()
            disable.mutate()
          }}
        >
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label={t('settings.currentPassword')}>
              {(id) => <Input id={id} type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" autoFocus required />}
            </Field>
            <Field label={t('settings.code')}>{codeInput}</Field>
          </div>
          {disable.error && <p className="text-sm text-destructive">{errText(disable.error)}</p>}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="secondary" onClick={() => setDisabling(false)}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" variant="danger" disabled={disable.isPending || !password || code.length !== 6}>
              {t('settings.totpDisable')}
            </Button>
          </div>
        </form>
      </Dialog>
    </Section>
  )
}

function SessionsCard() {
  const { t, i18n } = useTranslation()
  const sessions = useQuery({ queryKey: ['account', 'sessions'], queryFn: api.account.sessions })
  const revoke = useMutation({ mutationFn: api.account.revokeSession, onSuccess: () => sessions.refetch() })
  const others = useMutation({ mutationFn: api.account.revokeOthers, onSuccess: () => sessions.refetch() })
  const shortUA = (ua: string) => ua.match(/(Firefox|Edg|Chrome|Safari|Opera)\/[\d.]+/)?.[0]?.replace('Edg', 'Edge') ?? (ua.slice(0, 40) || '—')

  return (
    <Section icon={<Monitor className="h-4 w-4" />} title={t('settings.sessions')} hint={t('settings.sessionsHint')}>
      <ul className="divide-y text-sm">
        {(sessions.data ?? []).map((s) => (
          <li key={s.id} className="flex items-center gap-3 py-2.5">
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2">
                <span className="truncate">{shortUA(s.userAgent)}</span>
                {s.current && <Badge tone="success">{t('settings.thisDevice')}</Badge>}
              </div>
              <div className="text-xs text-muted-foreground">
                {s.ip} · {new Date(s.createdAt).toLocaleString(i18n.language)} · {t('settings.until', { time: new Date(s.expiresAt).toLocaleTimeString(i18n.language) })}
              </div>
            </div>
            {!s.current && (
              <Button variant="ghost" size="sm" onClick={() => revoke.mutate(s.id)}>
                {t('settings.revoke')}
              </Button>
            )}
          </li>
        ))}
      </ul>
      {(sessions.data?.length ?? 0) > 1 && (
        <Button className="mt-3" variant="secondary" size="sm" onClick={() => others.mutate()} disabled={others.isPending}>
          {t('settings.revokeOthers')}
        </Button>
      )}
    </Section>
  )
}

function DomainCard() {
  const { t, i18n } = useTranslation()
  const qc = useQueryClient()
  const lng = i18n.language
  const [issuing, setIssuing] = useState(false)
  const q = useQuery({
    queryKey: ['domain'],
    queryFn: api.domain.get,
    // While a certificate is being issued, follow its steps.
    refetchInterval: (query) => (issuing || query.state.data?.status.progress ? 2000 : false),
  })
  const d = q.data
  const [form, setForm] = useState<{ name: string; token: string; ipMode: 'auto' | 'custom'; ip: string; staging: boolean } | null>(null)
  const [error, setError] = useState<Record<string, string>>({})
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [startedAt, setStartedAt] = useState(0)
  useEffect(() => {
    if (d && !form) setForm({ name: d.name, token: '', ipMode: d.ipMode || 'auto', ip: d.ip, staging: d.staging })
  }, [d, form])
  useEffect(() => {
    // Issuance is over once a newer attempt is recorded and no step is running.
    if (issuing && d && !d.status.progress && new Date(d.status.lastAttempt).getTime() >= startedAt - 5000) setIssuing(false)
  }, [d, issuing, startedAt])

  const save = useMutation({
    mutationFn: () => api.domain.save(form!),
    onSuccess: (data) => {
      qc.setQueryData(['domain'], data)
      setForm({ ...form!, token: '' })
      setError({})
    },
    onError: (err) => setError(err instanceof ApiError && err.field ? { [err.field]: err.message } : { form: t('common.error') }),
  })
  const issue = useMutation({
    mutationFn: api.domain.issue,
    onSuccess: () => {
      setStartedAt(Date.now())
      setIssuing(true)
    },
  })
  const remove = useMutation({
    mutationFn: api.domain.remove,
    onSuccess: (data) => {
      qc.setQueryData(['domain'], data)
      setForm({ name: '', token: '', ipMode: 'auto', ip: '', staging: false })
      setConfirmDelete(false)
    },
  })

  if (!d || !form) return null
  const saved = d.name !== '' && d.hasToken
  const dirty = form.name !== d.name || form.token !== '' || form.ipMode !== d.ipMode || form.ip !== d.ip || form.staging !== d.staging
  const progress = d.status.progress
  const busy = issuing || Boolean(progress)
  const days = d.cert ? Math.floor((new Date(d.cert.notAfter).getTime() - Date.now()) / 86_400_000) : 0
  const fmt = (iso: string) => new Date(iso).toLocaleString(lng, { dateStyle: 'medium', timeStyle: 'short' })

  return (
    <Section icon={<Globe className="h-4 w-4" />} title={t('domain.title')} hint={t('domain.hint')}>
      <div className="space-y-4">
        {!saved && (
          <ol className="list-decimal space-y-1 rounded-xl bg-muted/60 p-3.5 pl-8 text-sm text-muted-foreground">
            <li>{t('domain.step1')}</li>
            <li>{t('domain.step2')}</li>
            <li>{t('domain.step3')}</li>
          </ol>
        )}
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={t('domain.name')} error={error.name}>
            {(id) => (
              <div className="flex items-center gap-2">
                <Input id={id} value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="myvpn" autoComplete="off" />
                <span className="shrink-0 text-sm text-muted-foreground">.duckdns.org</span>
              </div>
            )}
          </Field>
          <Field label={t('domain.token')} hint={d.hasToken ? t('domain.tokenSaved') : undefined} error={error.token}>
            {(id) => (
              <Input
                id={id}
                type="password"
                autoComplete="off"
                value={form.token}
                onChange={(e) => setForm({ ...form, token: e.target.value })}
                placeholder={d.hasToken ? '••••••••' : 'xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx'}
              />
            )}
          </Field>
        </div>
        {d.ipMode === 'custom' && d.ip && <p className="text-xs text-muted-foreground">{t('domain.ipCustomNote', { ip: d.ip })}</p>}
        {error.form && <p className="text-sm text-destructive">{error.form}</p>}
        {(dirty || !saved) && (
          <Button onClick={() => save.mutate()} disabled={save.isPending || !form.name}>
            {t('common.save')}
          </Button>
        )}

        {saved && (
          <div className="space-y-3 border-t pt-4">
            <div className="flex flex-wrap items-center gap-2 text-sm">
              <span className="font-mono">{d.domain}</span>
              {d.status.ipError ? (
                <Badge tone="danger">{t('domain.dnsFailed')}</Badge>
              ) : d.status.ipUpdatedAt ? (
                <Badge tone="success">{t('domain.dnsOk', { ip: d.ipMode === 'custom' ? d.ip : t('domain.ipAuto') })}</Badge>
              ) : null}
            </div>
            {d.status.ipError && <p className="text-xs text-destructive">{d.status.ipError}</p>}

            {d.cert ? (
              <div className="rounded-xl bg-muted/60 p-3.5 text-sm">
                <div className="flex flex-wrap items-center gap-2">
                  <ShieldCheck className="h-4 w-4 text-success" />
                  <span className="font-medium">{t('domain.certFor', { domain: d.cert.domain })}</span>
                  {d.cert.staging && <Badge tone="warning">{t('domain.stagingBadge')}</Badge>}
                </div>
                <div className="mt-1 text-xs text-muted-foreground">
                  {t('domain.certUntil', { date: new Date(d.cert.notAfter).toLocaleDateString(lng, { dateStyle: 'long' }), days })} · {d.cert.issuer}
                </div>
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">{t('domain.noCert')}</p>
            )}

            {busy ? (
              <div className="flex items-center gap-2 rounded-xl bg-accent px-3.5 py-2.5 text-sm text-accent-foreground">
                <RefreshCw className="h-4 w-4 animate-spin" />
                {t(`domain.progress_${progress || 'account'}`)}
              </div>
            ) : (
              <div className="space-y-2">
                <Button variant={d.cert ? 'secondary' : 'primary'} size="sm" disabled={issue.isPending || dirty} onClick={() => issue.mutate()}>
                  <ShieldCheck className="h-4 w-4" />
                  {d.cert ? t('domain.reissue') : t('domain.issue')}
                </Button>
                <p className="text-xs text-muted-foreground">
                  {t('domain.terms')}{' '}
                  <a href={d.terms} target="_blank" rel="noreferrer" className="underline underline-offset-2">
                    Let&apos;s Encrypt
                  </a>
                </p>
              </div>
            )}
            {!busy && d.status.lastError && (
              <p className="text-xs text-destructive">{t('domain.lastError', { error: d.status.lastError, time: fmt(d.status.lastAttempt) })}</p>
            )}

            {d.cert && (
              <div className="space-y-1.5 rounded-xl border p-3.5 text-xs text-muted-foreground">
                <div className="text-sm font-medium text-foreground">{t('domain.usesTitle')}</div>
                <div>✓ {t('domain.usePanel', { domain: d.cert.domain })}</div>
                <div>✓ {t('domain.useInbounds', { domain: d.cert.domain })}</div>
                <div>{d.publicHost === d.cert.domain ? '✓ ' + t('domain.useLinksOn') : '→ ' + t('domain.useLinksOff', { domain: d.cert.domain })}</div>
              </div>
            )}

            {confirmDelete ? (
              <div className="flex flex-wrap items-center justify-between gap-2 rounded-xl bg-destructive-soft p-3 text-sm">
                <span>{t('domain.deleteConfirm')}</span>
                <div className="flex gap-2">
                  <Button variant="secondary" size="sm" onClick={() => setConfirmDelete(false)}>
                    {t('common.cancel')}
                  </Button>
                  <Button variant="danger" size="sm" disabled={remove.isPending} onClick={() => remove.mutate()}>
                    {t('common.delete')}
                  </Button>
                </div>
              </div>
            ) : (
              <Button variant="ghost" size="sm" onClick={() => setConfirmDelete(true)}>
                {t('domain.remove')}
              </Button>
            )}
          </div>
        )}
      </div>
    </Section>
  )
}

function BackupCard() {
  const { t } = useTranslation()
  const [asking, setAsking] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [done, setDone] = useState(false)

  async function download(password: string) {
    setBusy(true)
    setError('')
    try {
      const res = await fetch(basePath + 'api/system/backup', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password }),
      })
      if (!res.ok) {
        setError(res.status === 403 ? t('settings.wrongPassword') : t('common.error'))
        return
      }
      const name = /filename="([^"]+)"/.exec(res.headers.get('Content-Disposition') ?? '')?.[1] ?? 'multiprotocol-backup.tar.gz'
      const url = URL.createObjectURL(await res.blob())
      const a = document.createElement('a')
      a.href = url
      a.download = name
      a.click()
      setTimeout(() => URL.revokeObjectURL(url), 1000)
      setAsking(false)
      setDone(true)
    } catch {
      setError(t('common.error'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Section icon={<Archive className="h-4 w-4" />} title={t('backup.title')} hint={t('backup.hint')}>
      <div className="space-y-3">
        <Button
          variant="secondary"
          onClick={() => {
            setError('')
            setDone(false)
            setAsking(true)
          }}
        >
          <Archive className="h-4 w-4" />
          {t('backup.download')}
        </Button>
        {done && <p className="text-xs text-muted-foreground">{t('backup.done')}</p>}
        <p className="text-xs text-muted-foreground">{t('backup.restoreHint')}</p>
      </div>
      <PasswordPrompt
        open={asking}
        title={t('backup.title')}
        text={t('backup.confirm')}
        confirm={t('backup.download')}
        busy={busy}
        error={error || undefined}
        onConfirm={download}
        onClose={() => setAsking(false)}
      />
    </Section>
  )
}

function UpdateCard() {
  const { t, i18n } = useTranslation()
  const lng = i18n.language
  const [started, setStarted] = useState<string | null>(null) // version when an update began
  const q = useQuery({
    queryKey: ['update'],
    queryFn: () => api.update.status(false),
    // The panel restarts during an update: keep asking until it answers.
    refetchInterval: started !== null ? 3000 : false,
    retry: started !== null ? true : 1,
  })
  const system = useQuery({ queryKey: ['system'], queryFn: api.system })
  const qc = useQueryClient()
  const check = useMutation({
    mutationFn: () => api.update.status(true),
    onSuccess: (d) => qc.setQueryData(['update'], d),
  })
  const start = useMutation({
    mutationFn: (cores: boolean) => api.update.start(cores),
    onSuccess: () => setStarted(q.data?.current ?? ''),
  })

  const d = q.data
  const log = d?.log ?? []
  const last = log.length ? log[log.length - 1] : ''
  const finished = /== .*(done|failed)$/.test(last)
  useEffect(() => {
    if (started === null || !d || d.pending) return
    if (d.current !== started) window.location.reload() // the new version answered
    else if (finished) setStarted(null) // same version (reinstall, cores only, or a failure)
  }, [d, started, finished])

  if (!d)
    return (
      <Section icon={<Download className="h-4 w-4" />} title={t('update.title')}>
        <p className="text-sm text-muted-foreground">{t('update.checking')}</p>
      </Section>
    )
  const latest = d.latest?.tag
  const newer = Boolean(latest && latest !== d.current)
  const busy = started !== null
  // "== 2026-11-05T10:00:00+03:00 update started (cores: 0)" … "== … done"
  const lastAt = /^== (\S+)/.exec(log[0] ?? '')?.[1]
  const lastOk = last.endsWith('done')
  const cores = system.data?.localAgent?.cores ?? []

  return (
    <Section
      icon={<Download className="h-4 w-4" />}
      title={t('update.title')}
      actions={
        <Button variant="ghost" size="sm" disabled={check.isPending || busy} onClick={() => check.mutate()}>
          <RefreshCw className={'h-4 w-4 ' + (check.isPending ? 'animate-spin' : '')} />
          {t('update.check')}
        </Button>
      }
    >
      <div className="space-y-4">
        <div className="rounded-xl bg-muted/60 p-3.5">
          <div>
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-mono text-base font-semibold">{d.current}</span>
              {newer ? (
                <Badge tone="accent">{t('update.available', { version: latest })}</Badge>
              ) : latest ? (
                <Badge tone="success">{t('update.upToDate')}</Badge>
              ) : null}
            </div>
            <div className="mt-0.5 text-xs text-muted-foreground">
              {d.error ? t('update.checkFailed', { error: d.error }) : t('update.source', { repo: d.repo })}
            </div>
          </div>
        </div>

        {busy ? (
          <div className="flex items-center gap-2 rounded-xl bg-accent px-3.5 py-2.5 text-sm text-accent-foreground">
            <RefreshCw className="h-4 w-4 animate-spin" />
            {t('update.running')}
          </div>
        ) : (
          newer && (
            <div className="flex flex-wrap items-center gap-3">
              <Button disabled={!d.supported || start.isPending} onClick={() => start.mutate(false)}>
                <Download className="h-4 w-4" />
                {t('update.updateTo', { version: latest })}
              </Button>
              <a href={d.latest!.url} target="_blank" rel="noreferrer" className="text-sm text-muted-foreground underline-offset-4 hover:text-foreground hover:underline">
                {t('update.whatsNew')}
              </a>
            </div>
          )
        )}

        <div className="flex flex-wrap items-center justify-between gap-3 border-t pt-4">
          <div className="text-sm">
            <div className="font-medium">{t('update.cores')}</div>
            <div className="text-xs text-muted-foreground">
              {cores.length ? cores.map((c) => `${c.name} ${c.version || '—'}`).join(' · ') : '—'}
            </div>
          </div>
          <Button variant="secondary" size="sm" disabled={!d.supported || busy || start.isPending} onClick={() => start.mutate(true)}>
            {t('update.updateCores')}
          </Button>
        </div>

        {!d.supported && <p className="text-xs text-muted-foreground">{t('update.unsupported')}</p>}
        {start.isError && <p className="text-xs text-destructive">{t('common.error')}</p>}

        <details className="text-xs text-muted-foreground">
          <summary className="cursor-pointer select-none">
            {lastAt
              ? t('update.lastRun', {
                  time: new Date(lastAt).toLocaleString(lng, { dateStyle: 'medium', timeStyle: 'short' }),
                  result: finished ? (lastOk ? t('update.ok') : t('update.failed')) : t('update.inProgress'),
                })
              : t('update.noRuns')}
          </summary>
          <div className="mt-2 space-y-2">
            {log.length > 0 && <pre className="max-h-48 overflow-auto rounded-xl bg-muted p-3 text-[11px] text-foreground">{log.join('\n')}</pre>}
            <Button variant="ghost" size="sm" disabled={!d.supported || busy} onClick={() => start.mutate(false)}>
              {t('update.reinstall')}
            </Button>
          </div>
        </details>
      </div>
    </Section>
  )
}
