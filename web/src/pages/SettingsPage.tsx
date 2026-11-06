import { useEffect, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Archive, Cpu, Check, Download, Globe, KeyRound, Monitor, RefreshCw, Send, ShieldCheck, ShieldOff } from 'lucide-react'
import { ApiError, api, basePath, coreVersions, extras, type PanelSettings, type SubSettings } from '@/lib/api'
import { Badge, Button, Card, Dialog, Field, Input, PageHeader, Select, Switch, Tabs, useSavedTab, Section } from '@/components/ui'
import { CopyButton, QrImage } from '@/components/share'

const tabs = ['access', 'security', 'maintenance'] as const
type Tab = (typeof tabs)[number]

export function SettingsPage() {
  const { t } = useTranslation()
  const [tab, choose] = useSavedTab<Tab>('settings.tab', tabs)
  return (
    <>
      <PageHeader title={t('settings.title')} subtitle={t('settings.subtitle')} />
      <div className="mx-auto max-w-3xl">
        <Tabs tabs={tabs} value={tab} onChange={choose} label={(v) => t(`settings.tab_${v}`)} />
        <div className="space-y-4">
          {tab === 'access' && (
            <>
              <AddressCard />
              <DomainCard />
              <SubscriptionCard />
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
              <CoresAutoCard />
              <CoreVersionsCard />
              <BackupCard />
              <TelegramCard />
            </>
          )}
        </div>
      </div>
    </>
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
  const [startedAt, setStartedAt] = useState(0)
  const [withCores, setWithCores] = useState(false)
  const [doneTo, setDoneTo] = useState<string | null>(null)
  const [result, setResult] = useState<'ok' | 'failed' | null>(null)
  const [, tick] = useState(0)
  // Shown once after the page reloads into the new version.
  const [justUpdated] = useState(() => {
    try {
      const v = sessionStorage.getItem('mp.updated')
      sessionStorage.removeItem('mp.updated')
      return v
    } catch {
      return null
    }
  })
  const start = useMutation({
    mutationFn: (cores: boolean) => api.update.start(cores),
    onMutate: (cores) => {
      setStartedAt(Date.now())
      setWithCores(cores)
    },
    onSuccess: () => {
      setResult(null)
      setStarted(q.data?.current ?? '')
      q.refetch()
    },
  })

  const d = q.data
  const log = d?.log ?? []
  const last = log.length ? log[log.length - 1] : ''
  const finished = /== .*(done|failed)$/.test(last)
  const busy = started !== null || start.isPending
  useEffect(() => {
    if (!busy) return
    const id = setInterval(() => tick((n) => n + 1), 1000)
    return () => clearInterval(id)
  }, [busy])
  useEffect(() => {
    if (started === null || !d || d.pending) return
    if (d.current !== started) {
      // The new version answered: say so, then load its pages.
      setDoneTo(d.current)
      try {
        sessionStorage.setItem('mp.updated', `${started} → ${d.current}`)
      } catch {
        /* the banner after reload is a nicety */
      }
      const id = setTimeout(() => window.location.reload(), 2500)
      return () => clearTimeout(id)
    }
    if (finished && Date.now() - startedAt > 5000) {
      // Same version: a reinstall, cores only, or a failure.
      setResult(last.endsWith('done') ? 'ok' : 'failed')
      setStarted(null)
    }
  }, [d, started, finished, startedAt, last])

  if (!d)
    return (
      <Section icon={<Download className="h-4 w-4" />} title={t('update.title')}>
        <p className="text-sm text-muted-foreground">{t('update.checking')}</p>
      </Section>
    )
  const latest = d.latest?.tag
  const newer = Boolean(latest && latest !== d.current)
  // Stages from the "==> step" lines of this run's log.
  // Until the updater picks the request up, the log is the previous run's.
  const runLog = busy && (start.isPending || d.pending) ? [] : log
  const seen = new Set(runLog.filter((l) => l.startsWith('==> ')).map((l) => l.slice(4).trim()))
  const panelDown = busy && (q.isError || q.failureCount > 0)
  const steps = ['queued', 'download', 'verify', 'install', ...(withCores ? ['cores'] : []), 'restart', 'back'] as const
  const hit = (s: string) =>
    s === 'queued' ? started !== null && !d.pending : s === 'back' ? doneTo !== null : seen.has(s) || (s === 'restart' && panelDown)
  // A later stage implies the earlier ones (an older installer marks fewer).
  const lastHit = steps.reduce((m, s, i) => (hit(s) ? i : m), -1)
  const reached = (s: string) => steps.indexOf(s as (typeof steps)[number]) <= lastHit
  const current = lastHit + 1
  const failedRun = started !== null && finished && last.endsWith('failed')
  const elapsed = startedAt ? Math.round((Date.now() - startedAt) / 1000) : 0
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

        {result && !busy && (
          <p className={'rounded-xl px-3.5 py-2.5 text-sm ' + (result === 'ok' ? 'bg-success-soft text-success' : 'bg-destructive/10 text-destructive')}>
            {result === 'ok' ? t('update.resultOk') : t('update.failedRun')}
          </p>
        )}
        {justUpdated && !busy && (
          <div className="flex items-center gap-2 rounded-xl bg-success-soft px-3.5 py-2.5 text-sm text-success">
            <Check className="h-4 w-4" />
            {t('update.justUpdated', { versions: justUpdated })}
          </div>
        )}
        {busy ? (
          <div className="space-y-3 rounded-xl border p-3.5">
            <div className="flex items-center justify-between gap-2 text-sm font-medium">
              <span>{doneTo ? t('update.doneTo', { version: doneTo }) : failedRun ? t('update.failedRun') : t('update.running')}</span>
              <span className="tabular-nums text-xs text-muted-foreground">{elapsed} c</span>
            </div>
            <ol className="space-y-1.5">
              {steps.map((s, i) => {
                const ok = reached(s)
                const now = i === current && !failedRun
                return (
                  <li key={s} className={'flex items-center gap-2 text-sm ' + (ok || now ? '' : 'text-muted-foreground')}>
                    {ok ? (
                      <Check className="h-4 w-4 text-success" />
                    ) : now ? (
                      <RefreshCw className="h-4 w-4 animate-spin text-primary" />
                    ) : (
                      <span className="mx-1.5 h-1.5 w-1.5 rounded-full bg-muted-foreground/40" />
                    )}
                    {t(`update.step.${s}`)}
                  </li>
                )
              })}
            </ol>
            {panelDown && !doneTo && <p className="text-xs text-muted-foreground">{t('update.restarting')}</p>}
            {runLog.length > 0 && (
              <pre className="max-h-40 overflow-auto rounded-lg bg-muted p-2.5 text-[11px] text-foreground">{runLog.filter((l) => !l.startsWith('==> ')).slice(-12).join('\n')}</pre>
            )}
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

function TelegramCard() {
  const { t, i18n } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['telegram'], queryFn: api.telegram.get })
  const [token, setToken] = useState('')
  const [form, setForm] = useState<{ chatId: number; chatName: string; events: string[]; dailyBackup: boolean; control: boolean; backupPassword: string } | null>(null)
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null)
  const d = q.data
  useEffect(() => {
    if (d && !form) setForm({ chatId: d.chatId, chatName: d.chatName, events: d.events, dailyBackup: d.dailyBackup, control: d.control, backupPassword: '' })
  }, [d, form])
  const fail = (e: unknown) => setMsg({ ok: false, text: e instanceof ApiError ? e.message : t('common.error') })
  const save = useMutation({
    mutationFn: () => api.telegram.save({ token, ...form! }),
    onSuccess: (data) => {
      qc.setQueryData(['telegram'], data)
      setToken('')
      setForm({ ...form!, backupPassword: '' })
      setMsg({ ok: true, text: t('telegram.saved') })
    },
    onError: fail,
  })
  const find = useMutation({
    mutationFn: () => api.telegram.findChat(token),
    onSuccess: (c) => {
      setForm({ ...form!, chatId: c.id, chatName: c.name })
      setMsg({ ok: true, text: t('telegram.found', { name: c.name }) })
    },
    onError: fail,
  })
  const test = useMutation({ mutationFn: api.telegram.test, onSuccess: () => setMsg({ ok: true, text: t('telegram.testSent') }), onError: fail })
  const backupNow = useMutation({
    mutationFn: api.telegram.backup,
    onSuccess: () => {
      setMsg({ ok: true, text: t('telegram.backupSent') })
      qc.invalidateQueries({ queryKey: ['telegram'] })
    },
    onError: fail,
  })
  if (!d || !form) return null
  const ready = d.hasToken && d.chatId !== 0
  const toggle = (e: string) => setForm({ ...form, events: form.events.includes(e) ? form.events.filter((x) => x !== e) : [...form.events, e] })

  return (
    <Section icon={<Send className="h-4 w-4" />} title={t('telegram.title')} hint={t('telegram.hint')}>
      <div className="space-y-4">
        {!d.hasToken && (
          <ol className="list-decimal space-y-1 rounded-xl bg-muted/60 p-3.5 pl-8 text-sm text-muted-foreground">
            <li>{t('telegram.step1')}</li>
            <li>{t('telegram.step2')}</li>
            <li>{t('telegram.step3')}</li>
          </ol>
        )}
        <div className="grid gap-3 sm:grid-cols-[1fr_auto] sm:items-end">
          <Field label={t('telegram.token')} hint={d.hasToken ? t('domain.tokenSaved') : undefined}>
            {(id) => <Input id={id} type="password" autoComplete="off" value={token} onChange={(e) => setToken(e.target.value)} placeholder={d.hasToken ? '••••••••' : '123456789:AAE…'} />}
          </Field>
          <Button variant="secondary" disabled={find.isPending || (!token && !d.hasToken)} onClick={() => find.mutate()}>
            {t('telegram.findChat')}
          </Button>
        </div>
        <p className="text-sm">
          {t('telegram.chat')}: {form.chatId ? <span className="font-medium">{form.chatName || form.chatId}</span> : <span className="text-muted-foreground">{t('telegram.noChat')}</span>}
        </p>

        <div>
          <div className="text-sm font-medium">{t('telegram.events')}</div>
          <div className="mt-2 grid gap-1.5 sm:grid-cols-2">
            {d.allEvents.map((e) => (
              <label key={e} className="flex items-center gap-2 text-sm">
                <input type="checkbox" checked={form.events.includes(e)} onChange={() => toggle(e)} />
                {t(`telegram.ev.${e.replace('.', '_')}`)}
              </label>
            ))}
          </div>
        </div>

        <label className="flex items-start gap-2.5 rounded-xl bg-muted/60 p-3.5 text-sm">
          <input type="checkbox" className="mt-1" checked={form.control} onChange={(e) => setForm({ ...form, control: e.target.checked })} />
          <span>
            {t('telegram.control')}
            <span className="block text-xs text-muted-foreground">{t('telegram.controlHint')}</span>
          </span>
        </label>

        <div className="rounded-xl bg-muted/60 p-3.5">
          <label className="flex items-start gap-2.5 text-sm">
            <input type="checkbox" className="mt-1" checked={form.dailyBackup} onChange={(e) => setForm({ ...form, dailyBackup: e.target.checked })} />
            <span>
              {t('telegram.dailyBackup')}
              <span className="block text-xs text-muted-foreground">{t('telegram.dailyBackupHint')}</span>
            </span>
          </label>
          {form.dailyBackup && (
            <div className="mt-3">
              <Field label={t('telegram.backupPassword')} hint={d.hasBackupPassword ? t('telegram.backupPasswordSaved') : t('telegram.backupPasswordHint')}>
                {(id) => <Input id={id} type="password" autoComplete="new-password" value={form.backupPassword} onChange={(e) => setForm({ ...form, backupPassword: e.target.value })} />}
              </Field>
            </div>
          )}
          {d.lastBackup && !d.lastBackup.startsWith('0001') && (
            <p className="mt-2 text-xs text-muted-foreground">
              {t('telegram.lastBackup', { time: new Date(d.lastBackup).toLocaleString(i18n.language, { dateStyle: 'medium', timeStyle: 'short' }) })}
            </p>
          )}
          {d.lastError && <p className="mt-1 text-xs text-destructive">{d.lastError}</p>}
        </div>

        {msg && <p className={'text-sm ' + (msg.ok ? 'text-success' : 'text-destructive')}>{msg.text}</p>}
        <div className="flex flex-wrap gap-2">
          <Button disabled={save.isPending} onClick={() => save.mutate()}>
            {t('common.save')}
          </Button>
          <Button variant="secondary" disabled={!ready || test.isPending} onClick={() => test.mutate()}>
            {t('telegram.test')}
          </Button>
          <Button variant="ghost" disabled={!ready || !d.hasBackupPassword || backupNow.isPending} onClick={() => backupNow.mutate()}>
            {t('telegram.backupNow')}
          </Button>
        </div>
      </div>
    </Section>
  )
}

// CoresAutoCard — ночная проверка и установка новых версий ядер.
function CoresAutoCard() {
  const { t, i18n } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['cores-auto'], queryFn: api.update.coresAuto, refetchInterval: 30_000 })
  const save = useMutation({
    mutationFn: (v: { enabled: boolean; hour: number }) => api.update.saveCoresAuto(v.enabled, v.hour),
    onSuccess: (d) => qc.setQueryData(['cores-auto'], d),
  })
  const s = q.data?.settings
  const last = s?.lastCheck && !s.lastCheck.startsWith('0001') ? new Date(s.lastCheck).toLocaleString(i18n.language) : ''
  const clock = q.data?.clock
  // Смещения от UTC в минутах: сервера и браузера.
  const browserOffset = -new Date().getTimezoneOffset()
  const sameZone = !clock || clock.offset === browserOffset
  const utc = (m: number) => 'UTC' + (m >= 0 ? '+' : '−') + Math.floor(Math.abs(m) / 60) + (Math.abs(m) % 60 ? ':' + String(Math.abs(m) % 60).padStart(2, '0') : '')
  // Час сервера h — сколько это на часах у админа.
  const localHour = (h: number) => {
    const m = (((h * 60 - (clock?.offset ?? 0) + browserOffset) % 1440) + 1440) % 1440
    return String(Math.floor(m / 60)).padStart(2, '0') + ':' + String(m % 60).padStart(2, '0')
  }
  return (
    <Section icon={<RefreshCw className="h-4 w-4" />} title={t('update.autoTitle')} hint={t('update.autoHint')}>
      {!q.data ? (
        <p className="text-sm text-muted-foreground">{t('common.loading')}</p>
      ) : !q.data.supported || !s ? (
        <p className="text-sm text-muted-foreground">{t('update.autoUnsupported')}</p>
      ) : (
        <div className="space-y-3">
          <div className="flex items-center justify-between gap-3">
            <span className="text-sm">{t('update.autoOn')}</span>
            <Switch label={t('update.autoOn')} checked={s.enabled} disabled={save.isPending} onChange={(v) => save.mutate({ enabled: v, hour: s.hour })} />
          </div>
          {s.enabled && (
            <Field label={t('update.autoHour')}>
              {(id) => (
                <Select id={id} value={s.hour} disabled={save.isPending} onChange={(e) => save.mutate({ enabled: true, hour: Number(e.target.value) })}>
                {Array.from({ length: 24 }, (_, h) => (
                  <option key={h} value={h}>
                    {String(h).padStart(2, '0')}:00
                    {!sameZone && ' ' + t('update.autoYours', { time: localHour(h) })}
                  </option>
                ))}
                </Select>
              )}
            </Field>
          )}
          {clock && (
            <p className="text-sm">
              {t('update.autoClock', { time: clock.time, zone: utc(clock.offset) })}
              {!sameZone && <span className="text-muted-foreground"> · {t('update.autoBrowser', { zone: utc(browserOffset) })}</span>}
            </p>
          )}
          <p className="text-xs text-muted-foreground">
            {last ? t('update.autoLast', { time: last }) : t('update.autoNever')}
            {s.lastError ? ` — ${s.lastError}` : ''}
          </p>
        </div>
      )}
    </Section>
  )
}

// SubscriptionCard — что подписка сообщает приложению: имя профиля,
// как часто обновляться, объявление и ссылка на поддержку.
function SubscriptionCard() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['sub-settings'], queryFn: extras.subSettings })
  const [form, setForm] = useState<SubSettings | null>(null)
  const s = form ?? q.data
  const save = useMutation({
    mutationFn: () => extras.saveSubSettings(s!),
    onSuccess: (d) => {
      qc.setQueryData(['sub-settings'], d)
      setForm(null)
    },
  })
  const err = save.error instanceof ApiError ? (save.error.field ? `${save.error.field}: ${save.error.message}` : save.error.message) : save.error ? t('common.error') : ''
  const set = <K extends keyof SubSettings>(k: K, v: SubSettings[K]) => setForm({ ...s!, [k]: v })
  return (
    <Section icon={<Send className="h-4 w-4" />} title={t('subSettings.title')} hint={t('subSettings.hint')}>
      {!s ? (
        <p className="text-sm text-muted-foreground">{t('common.loading')}</p>
      ) : (
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault()
            save.mutate()
          }}
        >
          <div className="grid gap-4 sm:grid-cols-[1fr_9rem]">
            <Field label={t('subSettings.name')} hint={t('subSettings.nameHint')}>
              {(id) => <Input id={id} value={s.title} maxLength={60} placeholder="{name}" onChange={(e) => set('title', e.target.value)} />}
            </Field>
            <Field label={t('subSettings.interval')}>
              {(id) => (
                <Select id={id} value={s.interval} onChange={(e) => set('interval', Number(e.target.value))}>
                  {[1, 3, 6, 12, 24, 48].map((h) => (
                    <option key={h} value={h}>
                      {t('subSettings.hours', { count: h })}
                    </option>
                  ))}
                </Select>
              )}
            </Field>
          </div>
          <Field label={t('subSettings.announce')} hint={t('subSettings.announceHint')}>
            {(id) => <Input id={id} value={s.announce} maxLength={200} onChange={(e) => set('announce', e.target.value)} />}
          </Field>
          <Field label={t('subSettings.support')} hint={t('subSettings.supportHint')}>
            {(id) => <Input id={id} value={s.supportUrl} maxLength={200} placeholder="https://t.me/…" onChange={(e) => set('supportUrl', e.target.value)} />}
          </Field>
          {err && <p className="text-sm text-destructive">{err}</p>}
          {form && (
            <div className="flex justify-end gap-2">
              <Button type="button" variant="secondary" onClick={() => setForm(null)}>
                {t('common.cancel')}
              </Button>
              <Button type="submit" disabled={save.isPending}>
                {t('common.save')}
              </Button>
            </div>
          )}
        </form>
      )}
    </Section>
  )
}

const coreTitles: Record<string, string> = { xray: 'Xray', hysteria: 'Hysteria2', singbox: 'sing-box' }

// CoreVersionsCard — выбор версии каждого ядра, как в 3x-ui. Не последняя
// версия закрепляется: ночное автообновление её не трогает.
function CoreVersionsCard() {
  const { t, i18n } = useTranslation()
  const [waiting, setWaiting] = useState<{ core: string; version: string } | null>(null)
  const q = useQuery({ queryKey: ['core-versions'], queryFn: coreVersions.list, refetchInterval: waiting ? 5_000 : false, retry: true })
  const [picked, setPicked] = useState<Record<string, string>>({})
  const install = useMutation({
    mutationFn: (v: { core: string; version: string }) => coreVersions.install(v.core, v.version),
    onSuccess: (_, v) => setWaiting(v),
  })
  const cores = q.data?.cores ?? []
  // Ждём, пока ядро станет нужной версии (панель за это время перезапустится).
  const done = waiting && cores.some((c) => c.core === waiting.core && (waiting.version === 'latest' ? c.current === c.latest : c.current === waiting.version))
  useEffect(() => {
    if (!done) return
    const id = setTimeout(() => setWaiting(null), 4000)
    return () => clearTimeout(id)
  }, [done])
  const err = install.error instanceof ApiError ? install.error.message : install.error ? t('common.error') : ''
  const date = (s: string) => new Date(s).toLocaleDateString(i18n.language, { day: 'numeric', month: 'short', year: 'numeric' })

  return (
    <Section icon={<Cpu className="h-4 w-4" />} title={t('coreVersions.title')} hint={t('coreVersions.hint')}>
      {!q.data ? (
        <p className="text-sm text-muted-foreground">{t('common.loading')}</p>
      ) : !q.data.supported ? (
        <p className="text-sm text-muted-foreground">{t('update.autoUnsupported')}</p>
      ) : (
        <div className="space-y-3">
          {cores.map((c) => {
            const value = picked[c.core] ?? c.pinned ?? c.current
            const choice = c.versions.find((v) => v.version === value)
            const busy = waiting?.core === c.core && !done
            return (
              <div key={c.core} className="rounded-xl bg-muted/60 p-3.5">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-medium">{coreTitles[c.core] ?? c.core}</span>
                  <span className="font-mono text-sm">{c.current}</span>
                  {c.current === c.latest && <Badge tone="success">{t('coreVersions.latest')}</Badge>}
                  {c.pinned && <Badge tone="warning">{t('coreVersions.pinned')}</Badge>}
                </div>
                {c.error && <p className="mt-1 text-xs text-destructive">{c.error}</p>}
                {c.versions.length > 0 && (
                  <div className="mt-2.5 flex flex-wrap items-center gap-2">
                    <Select value={value} disabled={busy} onChange={(e) => setPicked({ ...picked, [c.core]: e.target.value })} className="w-auto min-w-56">
                      {!c.versions.some((v) => v.version === c.current) && <option value={c.current}>{c.current}</option>}
                      {c.versions.map((v) => (
                        <option key={v.version} value={v.version}>
                          {v.version} · {date(v.published)}
                          {v.recommended ? ` · ${t('coreVersions.recommended')}` : ''}
                          {v.version === c.current ? ` · ${t('coreVersions.installed')}` : ''}
                        </option>
                      ))}
                    </Select>
                    <Button
                      size="sm"
                      variant="secondary"
                      disabled={busy || install.isPending || value === c.current}
                      onClick={() => install.mutate({ core: c.core, version: value })}
                    >
                      {busy ? t('coreVersions.installing') : t('coreVersions.install')}
                    </Button>
                    {c.pinned && (
                      <Button size="sm" variant="ghost" disabled={busy || install.isPending} onClick={() => install.mutate({ core: c.core, version: 'latest' })}>
                        {t('coreVersions.toLatest')}
                      </Button>
                    )}
                  </div>
                )}
                {choice?.note && <p className="mt-2 text-xs text-muted-foreground">{t(`coreVersions.${choice.note}`)}</p>}
                {busy && <p className="mt-2 text-xs text-muted-foreground">{t('coreVersions.wait')}</p>}
                {done && waiting?.core === c.core && <p className="mt-2 text-xs text-success">{t('coreVersions.done', { version: c.current })}</p>}
              </div>
            )
          })}
          {err && <p className="text-sm text-destructive">{err}</p>}
          <p className="text-xs text-muted-foreground">{t('coreVersions.footer')}</p>
        </div>
      )}
    </Section>
  )
}
