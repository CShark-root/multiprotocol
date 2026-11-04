import { useEffect, useState, type FormEvent, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound, Monitor, RefreshCw, ShieldCheck, ShieldOff } from 'lucide-react'
import { ApiError, api, type PanelSettings } from '@/lib/api'
import { Badge, Button, Card, Field, Input, PageHeader, Select } from '@/components/ui'
import { CopyButton, QrImage } from '@/components/share'

export function SettingsPage() {
  const { t } = useTranslation()
  return (
    <>
      <PageHeader title={t('settings.title')} subtitle={t('settings.subtitle')} />
      <div className="grid gap-4 lg:grid-cols-2">
        <div className="space-y-4">
          <AddressCard />
        </div>
        <div className="space-y-4">
          <TotpCard />
          <PasswordCard />
          <SessionsCard />
        </div>
      </div>
    </>
  )
}

function Section({ icon, title, hint, children }: { icon: ReactNode; title: string; hint?: string; children: ReactNode }) {
  return (
    <Card>
      <div className="flex items-center gap-2.5">
        <span className="text-accent-foreground">{icon}</span>
        <h2 className="font-semibold">{title}</h2>
      </div>
      {hint && <p className="mt-1 text-sm text-muted-foreground">{hint}</p>}
      <div className="mt-4">{children}</div>
    </Card>
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
  const [password, setPassword] = useState('')
  const [newUrl, setNewUrl] = useState('')
  const [restarting, setRestarting] = useState(false)
  useEffect(() => {
    if (q.data && !form) setForm(q.data.settings)
  }, [q.data, form])

  const save = useMutation({
    mutationFn: () => api.settings.savePanel({ ...form!, password }),
    onSuccess: (r) => {
      setNewUrl(r.url)
      setPassword('')
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
          save.mutate()
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
        <Field label={t('settings.confirmPassword')} hint={t('settings.confirmPasswordHint')} error={err('password') && t('settings.wrongPassword')}>
          {(id) => <Input id={id} type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" required />}
        </Field>
        {generalError && <p className="text-sm text-destructive">{generalError}</p>}
        <Button type="submit" disabled={save.isPending}>
          {t('settings.save')}
        </Button>
      </form>

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
        <form
          className="mt-4 space-y-3"
          onSubmit={(e: FormEvent) => {
            e.preventDefault()
            disable.mutate()
          }}
        >
          <p className="text-xs text-muted-foreground">{t('settings.totpDisableHint')}</p>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label={t('settings.currentPassword')}>
              {(id) => <Input id={id} type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" required />}
            </Field>
            <Field label={t('settings.code')}>{codeInput}</Field>
          </div>
          {disable.error && <p className="text-sm text-destructive">{errText(disable.error)}</p>}
          <Button type="submit" variant="secondary" disabled={disable.isPending}>
            {t('settings.totpDisable')}
          </Button>
        </form>
      )}
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
