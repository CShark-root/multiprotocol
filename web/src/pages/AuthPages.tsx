import { useState, type FormEvent, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from '@tanstack/react-router'
import { useQueryClient } from '@tanstack/react-query'
import { Shield } from 'lucide-react'
import { ApiError, api, type Admin, type AuthState } from '@/lib/api'
import { Button, Card, Field, Input } from '@/components/ui'

function AuthLayout({ title, text, children }: { title: string; text: string; children: ReactNode }) {
  return (
    <div className="flex min-h-dvh items-center justify-center px-4 py-10">
      <div className="w-full max-w-sm">
        <div className="mb-6 flex flex-col items-center text-center">
          <div className="mb-4 flex h-12 w-12 items-center justify-center rounded-2xl bg-primary text-primary-foreground">
            <Shield className="h-5 w-5" />
          </div>
          <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
          <p className="mt-1.5 text-sm text-muted-foreground">{text}</p>
        </div>
        <Card className="p-6">{children}</Card>
      </div>
    </div>
  )
}

function useSignedIn() {
  const qc = useQueryClient()
  const navigate = useNavigate()
  return (admin: Admin) => {
    qc.setQueryData<AuthState>(['auth', 'state'], { setupRequired: false, authenticated: true, admin })
    navigate({ to: '/' })
  }
}

export function SetupPage() {
  const { t } = useTranslation()
  const signedIn = useSignedIn()
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('')
  const [repeat, setRepeat] = useState('')
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)

  async function submit(e: FormEvent) {
    e.preventDefault()
    if (password !== repeat) {
      setErrors({ repeat: t('auth.passwordMismatch') })
      return
    }
    setBusy(true)
    setErrors({})
    try {
      const { admin } = await api.setup(username, password)
      signedIn(admin)
    } catch (err) {
      const field = err instanceof ApiError && err.field ? err.field : 'form'
      setErrors({ [field]: err instanceof ApiError ? err.message : t('common.error') })
    } finally {
      setBusy(false)
    }
  }

  return (
    <AuthLayout title={t('auth.setupTitle')} text={t('auth.setupText')}>
      <form onSubmit={submit} className="space-y-4">
        <Field label={t('auth.username')} hint={t('auth.usernameHint')} error={errors.username}>
          {(id) => <Input id={id} value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" required />}
        </Field>
        <Field label={t('auth.password')} hint={t('auth.passwordHint')} error={errors.password}>
          {(id) => (
            <Input id={id} type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" minLength={10} required />
          )}
        </Field>
        <Field label={t('auth.passwordRepeat')} error={errors.repeat}>
          {(id) => <Input id={id} type="password" value={repeat} onChange={(e) => setRepeat(e.target.value)} autoComplete="new-password" required />}
        </Field>
        {errors.form && <p className="text-sm text-destructive">{errors.form}</p>}
        <Button type="submit" className="w-full" disabled={busy}>
          {t('auth.create')}
        </Button>
      </form>
    </AuthLayout>
  )
}

export function LoginPage() {
  const { t } = useTranslation()
  const signedIn = useSignedIn()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      const { admin } = await api.login(username, password)
      signedIn(admin)
    } catch (err) {
      if (err instanceof ApiError && err.status === 429) setError(t('auth.rateLimited'))
      else if (err instanceof ApiError && err.status === 401) setError(t('auth.invalid'))
      else setError(t('common.error'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <AuthLayout title={t('auth.loginTitle')} text={t('auth.loginText')}>
      <form onSubmit={submit} className="space-y-4">
        <Field label={t('auth.username')}>
          {(id) => <Input id={id} value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" autoFocus required />}
        </Field>
        <Field label={t('auth.password')}>
          {(id) => <Input id={id} type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" required />}
        </Field>
        {error && <p className="text-sm text-destructive">{error}</p>}
        <Button type="submit" className="w-full" disabled={busy}>
          {t('auth.signIn')}
        </Button>
      </form>
    </AuthLayout>
  )
}
