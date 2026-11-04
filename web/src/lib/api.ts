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

export type CoreHealth = { name: string; installed: boolean; running: boolean; version: string; message?: string }

export type Protocol = 'vless' | 'trojan' | 'shadowsocks' | 'vmess' | 'hysteria2' | 'tuic' | 'anytls' | 'shadowtls'
export type Transport = 'xhttp' | 'tcp' | 'grpc' | 'ws' | 'quic'

/** What the wizard offers per protocol; mirrors the server's protocol registry. */
export const protocolSpecs: Record<Protocol, { transports: Transport[]; core: 'xray' | 'hysteria' | 'singbox'; network: string }> = {
  vless: { transports: ['xhttp', 'tcp', 'grpc'], core: 'xray', network: 'tcp' },
  trojan: { transports: ['tcp', 'xhttp', 'grpc'], core: 'xray', network: 'tcp' },
  shadowsocks: { transports: ['tcp'], core: 'xray', network: 'tcp+udp' },
  vmess: { transports: ['ws', 'tcp'], core: 'xray', network: 'tcp' },
  hysteria2: { transports: ['quic'], core: 'hysteria', network: 'udp' },
  tuic: { transports: ['quic'], core: 'singbox', network: 'udp' },
  anytls: { transports: ['tcp'], core: 'singbox', network: 'tcp' },
  shadowtls: { transports: ['tcp'], core: 'singbox', network: 'tcp' },
}
export const protocolOrder = Object.keys(protocolSpecs) as Protocol[]

export type Inbound = {
  id: number
  remark: string
  protocol: Protocol
  transport: Transport
  security: string
  network: string // tcp | udp | tcp+udp
  core: 'xray' | 'hysteria' | 'singbox'
  testKit: boolean
  wsPath?: string
  port: number
  address: string
  enabled: boolean
  serverName: string
  target?: string
  fingerprint?: string
  publicKey?: string
  shortId?: string
  xhttpPath?: string
  xhttpMode?: string
  grpcServiceName?: string
  obfs: boolean
  masquerade?: string
  upMbps?: number
  downMbps?: number
  certPin?: string
  createdAt: string
}

export type InboundInput = {
  remark: string
  protocol: Protocol
  transport: Transport
  port: number
  address: string
  target: string
  serverName: string
  fingerprint: string
  xhttpPath: string
  xhttpMode: string
  grpcServiceName: string
  wsPath: string
  obfs: boolean
  masquerade: string
  upMbps: number
  downMbps: number
  enabled: boolean
}

export type InboundSuggest = {
  port: number
  targets: string[]
  masquerades: string[]
  fingerprints: string[]
  xhttpModes: string[]
  publicHost: string
}

export type UserLinks = {
  subscriptionUrl: string
  singboxUrl: string
  links: { inboundId: number; remark: string; protocol: Protocol; transport: Transport; url: string }[]
}

export type SystemInfo = {
  version: string
  publicHost: string
  sync: { lastSync: string | null; lastError: string } | null
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
    links: (id: number) => request<UserLinks>('GET', `users/${id}/links`),
  },
  inbounds: {
    list: () => request<Inbound[]>('GET', 'inbounds'),
    suggest: (protocol: Protocol) => request<InboundSuggest>('GET', `inbounds/suggest?protocol=${protocol}`),
    createTestKit: () => request<{ inbounds: number }>('POST', 'inbounds/test-kit', {}),
    deleteTestKit: () => request<{ inbounds: number }>('DELETE', 'inbounds/test-kit'),
    create: (input: InboundInput) => request<Inbound>('POST', 'inbounds', input),
    update: (id: number, input: InboundInput) => request<Inbound>('PUT', `inbounds/${id}`, input),
    remove: (id: number) => request<void>('DELETE', `inbounds/${id}`),
  },
}
