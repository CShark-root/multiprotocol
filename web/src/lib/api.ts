// The server injects <base href="/<secret>/">; the dev server uses /dev/.
export const basePath = (document.querySelector('base')?.getAttribute('href') ?? import.meta.env.BASE_URL).replace(
  /\/?$/,
  '/',
)

export class ApiError extends Error {
  status: number
  code: string
  field?: string

  constructor(status: number, code: string, message: string, field?: string) {
    super(message)
    this.status = status
    this.code = code
    this.field = field
  }
}

let unauthorizedHandler: (() => void) | null = null

/** Called when an authenticated request comes back 401 (session expired). */
export function onUnauthorized(fn: () => void) {
  unauthorizedHandler = fn
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(basePath + 'api/' + path, {
    method,
    // Always JSON: the server rejects other content types on writes (CSRF guard).
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
    credentials: 'same-origin',
  })
  if (res.status === 204) return undefined as T
  const data = await res.json().catch(() => null)
  if (!res.ok) {
    const err = data?.error
    if (res.status === 401 && !path.startsWith('auth/')) unauthorizedHandler?.()
    throw new ApiError(res.status, err?.code ?? `http_${res.status}`, err?.message ?? res.statusText, err?.field)
  }
  return data as T
}

export type Admin = { id: number; username: string }
export type AuthState = { setupRequired: boolean; authenticated: boolean; admin?: Admin }

export type UserStatus = 'active' | 'disabled' | 'expired' | 'limited'
export type ResetPeriod = 'none' | 'day' | 'week' | 'month'

export type User = {
  id: number
  name: string
  note: string
  enabled: boolean
  status: UserStatus
  trafficLimit: number
  trafficUsed: number
  resetPeriod: ResetPeriod
  expireAt: string | null
  subToken: string
  createdAt: string
  updatedAt: string
}

export type UserInput = {
  name: string
  note: string
  enabled: boolean
  trafficLimit: number
  resetPeriod: ResetPeriod
  expireAt: string | null
}

export type CoreHealth = { protocol: string; running: boolean; version: string; message?: string }

export type SystemInfo = {
  version: string
  localAgent: {
    hostname: string
    os: string
    arch: string
    agentUptimeSeconds: number
    system: {
      cpus: number
      uptimeSeconds: number
      memTotal: number
      memAvailable: number
      load1: number
      load5: number
      load15: number
    }
    cores: CoreHealth[]
  } | null
}

export const api = {
  authState: () => request<AuthState>('GET', 'auth/state'),
  setup: (username: string, password: string) =>
    request<{ admin: Admin }>('POST', 'auth/setup', { username, password }),
  login: (username: string, password: string) =>
    request<{ admin: Admin }>('POST', 'auth/login', { username, password }),
  logout: () => request<void>('POST', 'auth/logout'),
  system: () => request<SystemInfo>('GET', 'system'),
  users: {
    list: () => request<User[]>('GET', 'users'),
    create: (input: UserInput) => request<User>('POST', 'users', input),
    update: (id: number, input: UserInput) => request<User>('PUT', `users/${id}`, input),
    remove: (id: number) => request<void>('DELETE', `users/${id}`),
  },
}
