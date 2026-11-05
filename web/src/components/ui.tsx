import { useEffect, useId, useRef, useState, type ButtonHTMLAttributes, type InputHTMLAttributes, type ReactNode, type SelectHTMLAttributes } from 'react'
import { cn } from '@/lib/utils'

type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger'

const buttonVariants: Record<ButtonVariant, string> = {
  primary: 'bg-primary text-primary-foreground hover:opacity-90 shadow-sm',
  secondary: 'bg-card text-foreground border border-input hover:bg-muted',
  ghost: 'text-muted-foreground hover:bg-muted hover:text-foreground',
  danger: 'bg-destructive text-primary-foreground hover:opacity-90 shadow-sm',
}

export function Button({
  variant = 'primary',
  size = 'md',
  className,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: ButtonVariant; size?: 'sm' | 'md' | 'icon' }) {
  return (
    <button
      className={cn(
        'inline-flex items-center justify-center gap-2 rounded-xl font-medium transition disabled:pointer-events-none disabled:opacity-50 cursor-pointer',
        size === 'md' && 'h-10 px-4 text-sm',
        size === 'sm' && 'h-8 px-3 text-sm',
        size === 'icon' && 'h-9 w-9',
        buttonVariants[variant],
        className,
      )}
      {...props}
    />
  )
}

export function Card({ className, children }: { className?: string; children: ReactNode }) {
  return <div className={cn('rounded-2xl border bg-card p-5', className)}>{children}</div>
}

export function Input({ className, ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      className={cn(
        'h-10 w-full rounded-xl border border-input bg-card px-3 text-sm placeholder:text-muted-foreground/70 transition focus:border-ring focus:outline-none focus:ring-2 focus:ring-ring/25',
        className,
      )}
      {...props}
    />
  )
}

export function Select({ className, ...props }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      className={cn(
        'h-10 w-full rounded-xl border border-input bg-card px-3 text-sm transition focus:border-ring focus:outline-none focus:ring-2 focus:ring-ring/25',
        className,
      )}
      {...props}
    />
  )
}

/** Label + control + helper text + error, the building block of every form. */
export function Field({
  label,
  hint,
  error,
  children,
}: {
  label: string
  hint?: string
  error?: string
  children: (id: string) => ReactNode
}) {
  const id = useId()
  return (
    <div className="space-y-1.5">
      <label htmlFor={id} className="block text-sm font-medium">
        {label}
      </label>
      {children(id)}
      {error ? (
        <p className="text-xs text-destructive">{error}</p>
      ) : hint ? (
        <p className="text-xs text-muted-foreground">{hint}</p>
      ) : null}
    </div>
  )
}

export function Switch({
  checked,
  onChange,
  label,
  disabled,
}: {
  checked: boolean
  onChange: (v: boolean) => void
  label: string
  disabled?: boolean
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      title={label}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={cn(
        'relative inline-flex h-6 w-10 shrink-0 cursor-pointer items-center rounded-full transition disabled:opacity-50',
        checked ? 'bg-primary' : 'bg-input',
      )}
    >
      <span
        className={cn(
          'inline-block h-5 w-5 rounded-full bg-card shadow transition-transform',
          checked ? 'translate-x-[18px]' : 'translate-x-0.5',
        )}
      />
    </button>
  )
}

const badgeTones = {
  success: 'bg-success-soft text-success',
  muted: 'bg-muted text-muted-foreground',
  warning: 'bg-warning-soft text-warning',
  danger: 'bg-destructive-soft text-destructive',
  accent: 'bg-accent text-accent-foreground',
}

export function Badge({ tone, children }: { tone: keyof typeof badgeTones; children: ReactNode }) {
  return (
    <span className={cn('inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium', badgeTones[tone])}>
      {children}
    </span>
  )
}

/** Modal built on <dialog>: focus trapping, Esc and the top layer for free. */
export function Dialog({
  open,
  onClose,
  title,
  description,
  children,
}: {
  open: boolean
  onClose: () => void
  title: string
  description?: string
  children: ReactNode
}) {
  const ref = useRef<HTMLDialogElement>(null)
  useEffect(() => {
    const d = ref.current
    if (!d) return
    if (open && !d.open) d.showModal()
    if (!open && d.open) d.close()
  }, [open])
  return (
    <dialog
      ref={ref}
      onCancel={(e) => {
        e.preventDefault()
        onClose()
      }}
      onClick={(e) => {
        if (e.target === ref.current) onClose()
      }}
      className="m-auto w-[min(32rem,calc(100vw-2rem))] rounded-2xl border bg-card p-0 text-foreground shadow-xl"
    >
      {open && (
        <div className="p-6">
          <h2 className="text-lg font-semibold">{title}</h2>
          {description && <p className="mt-1 text-sm text-muted-foreground">{description}</p>}
          <div className="mt-5">{children}</div>
        </div>
      )}
    </dialog>
  )
}

export function PageHeader({ title, subtitle, actions }: { title: string; subtitle?: string; actions?: ReactNode }) {
  return (
    <div className="mb-6 flex flex-wrap items-end justify-between gap-4">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
        {subtitle && <p className="mt-1 text-sm text-muted-foreground">{subtitle}</p>}
      </div>
      {actions}
    </div>
  )
}

export function EmptyState({ icon, title, text, action }: { icon: ReactNode; title: string; text: string; action?: ReactNode }) {
  return (
    <div className="flex flex-col items-center px-6 py-14 text-center">
      <div className="mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-accent text-accent-foreground">{icon}</div>
      <h3 className="font-semibold">{title}</h3>
      <p className="mt-1 max-w-sm text-sm text-muted-foreground">{text}</p>
      {action && <div className="mt-5">{action}</div>}
    </div>
  )
}

// useSavedTab запоминает выбранную вкладку страницы в localStorage (если браузер разрешает).
export function useSavedTab<T extends string>(key: string, tabs: readonly T[]): [T, (v: T) => void] {
  const [tab, setTab] = useState<T>(() => {
    try {
      const v = localStorage.getItem(key)
      return tabs.includes(v as T) ? (v as T) : tabs[0]
    } catch {
      return tabs[0]
    }
  })
  const choose = (v: T) => {
    setTab(v)
    try {
      localStorage.setItem(key, v)
    } catch {
      // приватный режим: вкладка просто не запомнится
    }
  }
  return [tab, choose]
}

export function Tabs<T extends string>({ tabs, value, onChange, label }: { tabs: readonly T[]; value: T; onChange: (v: T) => void; label: (v: T) => ReactNode }) {
  return (
    <div role="tablist" className="mb-5 flex gap-1 overflow-x-auto rounded-2xl border bg-card p-1">
      {tabs.map((v) => (
        <button
          key={v}
          role="tab"
          type="button"
          aria-selected={value === v}
          onClick={() => onChange(v)}
          className={cn(
            'flex-1 whitespace-nowrap rounded-xl px-3 py-2 text-sm transition',
            value === v ? 'bg-accent font-medium text-accent-foreground' : 'text-muted-foreground hover:text-foreground',
          )}
        >
          {label(v)}
        </button>
      ))}
    </div>
  )
}

// Ring — кольцевая диаграмма: доли segments (value, color) от общего total.
export function Ring({
  segments,
  total,
  size = 112,
  stroke = 10,
  children,
}: {
  segments: { value: number; color: string }[]
  total: number
  size?: number
  stroke?: number
  children?: ReactNode
}) {
  const r = (size - stroke) / 2
  const len = 2 * Math.PI * r
  let offset = 0
  return (
    <div className="relative shrink-0" style={{ width: size, height: size }}>
      <svg width={size} height={size} className="-rotate-90" aria-hidden>
        <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke="var(--color-muted)" strokeWidth={stroke} />
        {total > 0 &&
          segments
            .filter((s) => s.value > 0)
            .map((s, i) => {
              const dash = (s.value / total) * len
              const el = (
                <circle
                  key={i}
                  cx={size / 2}
                  cy={size / 2}
                  r={r}
                  fill="none"
                  stroke={s.color}
                  strokeWidth={stroke}
                  strokeDasharray={`${dash} ${len - dash}`}
                  strokeDashoffset={-offset}
                />
              )
              offset += dash
              return el
            })}
      </svg>
      <div className="absolute inset-0 flex flex-col items-center justify-center text-center">{children}</div>
    </div>
  )
}

// UsageRing — маленькое колечко «сколько израсходовано» (0–1); null — без лимита.
export function UsageRing({ part, size = 26 }: { part: number | null; size?: number }) {
  const p = part === null ? 0 : Math.min(Math.max(part, 0), 1)
  const color = p >= 1 ? 'var(--color-destructive)' : p >= 0.8 ? 'var(--color-warning)' : 'var(--color-primary)'
  return <Ring segments={[{ value: p, color }]} total={1} size={size} stroke={3} />
}

// RowMenu — меню «⋯» в строке таблицы. Закрывается по щелчку вне и по Esc.
export function RowMenu({ label, items }: { label: string; items: { label: string; icon?: ReactNode; danger?: boolean; onClick: () => void }[] }) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const close = (e: MouseEvent | KeyboardEvent) => {
      if (e instanceof KeyboardEvent ? e.key === 'Escape' : !ref.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', close)
    document.addEventListener('keydown', close)
    return () => {
      document.removeEventListener('mousedown', close)
      document.removeEventListener('keydown', close)
    }
  }, [open])
  return (
    <div ref={ref} className="relative">
      <Button variant="ghost" size="icon" aria-label={label} title={label} aria-expanded={open} onClick={() => setOpen((v) => !v)}>
        <span aria-hidden className="text-lg leading-none">⋯</span>
      </Button>
      {open && (
        <div role="menu" className="absolute right-0 top-full z-20 mt-1 min-w-44 overflow-hidden rounded-xl border bg-card p-1 shadow-lg">
          {items.map((it) => (
            <button
              key={it.label}
              role="menuitem"
              type="button"
              onClick={() => {
                setOpen(false)
                it.onClick()
              }}
              className={cn('flex w-full cursor-pointer items-center gap-2.5 rounded-lg px-3 py-2 text-left text-sm hover:bg-muted', it.danger && 'text-destructive')}
            >
              {it.icon}
              {it.label}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

// Section — карточка с единой шапкой: значок в плашке, заголовок, пояснение,
// действия справа. Так выглядят карточки на всех страницах.
export function Section({ icon, title, hint, actions, children }: { icon: ReactNode; title: string; hint?: string; actions?: ReactNode; children: ReactNode }) {
  return (
    <Card>
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 items-start gap-3">
          <span className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-accent text-accent-foreground [&_svg]:h-4 [&_svg]:w-4">{icon}</span>
          <div className="min-w-0">
            <h2 className="font-semibold leading-8">{title}</h2>
            {hint && <p className="text-sm text-muted-foreground">{hint}</p>}
          </div>
        </div>
        {actions && <div className="shrink-0">{actions}</div>}
      </div>
      <div className="mt-4">{children}</div>
    </Card>
  )
}
