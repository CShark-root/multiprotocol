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
  amneziaKey: string
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
  amneziaKey: string
}

export type OnlineEntry = { userId: number; ips: string[]; connections: number }
export type DayTraffic = { day: string; bytes: number }

export type Outbound = 'direct' | 'block' | 'warp' | 'ipv4' | `out:${string}`
export type CustomOut = { id: string; name: string; summary: string; link?: string }
export type CoreSupport = 'full' | 'partial' | 'none'
export type RoutingRule = {
  id: string
  name: string
  enabled: boolean
  match: string[]
  ports: string
  network: '' | 'tcp' | 'udp'
  protocols: string[]
  users: number[]
  outbound: Outbound
  support?: { xray: CoreSupport; hysteria: CoreSupport; singbox: CoreSupport }
}
export type RoutingPresets = {
  blockPrivate: boolean
  blockSmtp: boolean
  blockBittorrent: boolean
  blockAds: boolean
  googleIpv4: boolean
  warpServices: string[]
  countries: { code: string; outbound: Outbound }[]
}
export type WarpMode = 'off' | 'rules' | 'all'
export type RoutingSettings = {
  presets: RoutingPresets
  rules: RoutingRule[]
  warp: { mode: WarpMode; registered: boolean; warpPlus: boolean; address4?: string; address6?: string; endpoint?: string; registeredAt?: string }
  dns: {
    upstream: string
    custom: string
    strategy: 'prefer4' | 'ipv4' | 'both'
    extra: { server: string; domains: string[] }[]
    disableCache?: boolean
    disableFallback?: boolean
    disableFallbackIfMatch?: boolean
    parallel?: boolean
    systemHosts?: boolean
    clientIp?: string
    hosts?: { name: string; ip: string }[]
  }
  geo: { source: 'loyalsoldier' | 'runetfreedom' | 'custom'; geoipUrl: string; geositeUrl: string; autoUpdate: boolean }
  services: string[]
  outbounds: CustomOut[]
  geoState: { lastCheck: string; lastUpdate: string; lastError: string; lastSource: string; files: { name: string; size: number; modified: string }[] }
}
export type Trace = { ip: string; country: string; warp: string; latencyMs: number }

export type DomainInfo = {
  name: string
  domain: string
  hasToken: boolean
  ipMode: 'auto' | 'custom'
  ip: string
  staging: boolean
  status: { ipUpdatedAt: string; ipError: string; lastAttempt: string; lastError: string; progress: string }
  cert: { domain: string; notAfter: string; issuer: string; staging: boolean } | null
  publicHost: string
  terms: string
}

export type AmneziaClient = { name: string; id: string; created?: string; lastHandshake?: string; endpoint?: string; rxBytes: number; txBytes: number }
export type AmneziaContainer = {
  name: string
  kind: string
  image: string
  state: string
  status: string
  startedAt: string
  ports: string[]
  cpu: number
  memBytes: number
  clients: AmneziaClient[]
  note?: string
}

export type CoreHealth = { name: string; installed: boolean; running: boolean; version: string; message?: string }

export type Protocol = 'vless' | 'trojan' | 'shadowsocks' | 'vmess' | 'hysteria2' | 'tuic' | 'anytls' | 'shadowtls'
export type Transport = 'xhttp' | 'tcp' | 'grpc' | 'ws' | 'quic'

/** Client app families, by the core inside the app (see service.ClientApps). */
export type AppFamily = 'xray' | 'singbox'

/** What the wizard offers per protocol; mirrors the server's protocol registry. */
export const protocolSpecs: Record<
  Protocol,
  { transports: Transport[]; core: 'xray' | 'hysteria' | 'singbox'; network: string; apps: AppFamily[] }
> = {
  vless: { transports: ['xhttp', 'tcp', 'grpc'], core: 'xray', network: 'tcp', apps: ['xray'] },
  trojan: { transports: ['tcp', 'xhttp', 'grpc'], core: 'xray', network: 'tcp', apps: ['xray'] },
  shadowsocks: { transports: ['tcp'], core: 'xray', network: 'tcp+udp', apps: ['xray', 'singbox'] },
  vmess: { transports: ['ws', 'tcp'], core: 'xray', network: 'tcp', apps: ['xray', 'singbox'] },
  hysteria2: { transports: ['quic'], core: 'hysteria', network: 'udp', apps: ['xray', 'singbox'] },
  tuic: { transports: ['quic'], core: 'singbox', network: 'udp', apps: ['singbox'] },
  anytls: { transports: ['tcp'], core: 'singbox', network: 'tcp', apps: ['singbox'] },
  shadowtls: { transports: ['tcp'], core: 'singbox', network: 'tcp', apps: ['singbox'] },
}
export const protocolOrder = Object.keys(protocolSpecs) as Protocol[]

export type XHTTPExpert = {
  host?: string
  headers?: Record<string, string>
  paddingBytes?: string
  paddingObfsMode?: boolean
  paddingPlacement?: string
  paddingMethod?: string
  uplinkHttpMethod?: string
  sessionPlacement?: string
  seqPlacement?: string
  uplinkDataPlacement?: string
  noSseHeader?: boolean
  noGrpcHeader?: boolean
  maxEachPostBytes?: string
  minPostsIntervalMs?: string
  maxBufferedPosts?: number
  streamUpServerSecs?: string
  xmux?: {
    maxConcurrency?: string
    maxConnections?: string
    cMaxReuseTimes?: string
    hMaxRequestTimes?: string
    hMaxReusableSecs?: string
    hKeepAlivePeriod?: number
  }
}

export type ExpertInput = {
  reality?: { xver: number; maxTimeDiff: number; minClientVer: string; maxClientVer: string; spiderX: string; shortIds: string[]; mldsa65: boolean }
  xhttp?: XHTTPExpert
  sniffing?: { enabled: boolean; destOverride: string[]; metadataOnly: boolean; routeOnly: boolean } | null
  sockopt?: { tcpFastOpen?: boolean; tcpCongestion?: string; tcpMptcp?: boolean; acceptProxyProtocol?: boolean } | null
}

export type Inbound = {
  id: number
  remark: string
  protocol: Protocol
  transport: Transport
  security: string
  network: string // tcp | udp | tcp+udp
  core: 'xray' | 'hysteria' | 'singbox'
  apps: AppFamily[]
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
  hopPorts?: string
  hopInterval?: number
  certPin?: string
  expert: ExpertInput
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
  hopPorts: string
  hopInterval: number
  enabled: boolean
  expert: ExpertInput
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
  overLan: boolean
  links: { inboundId: number; remark: string; protocol: Protocol; transport: Transport; apps: AppFamily[]; url: string }[]
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
      netIface: string
      netRxBps: number
      netTxBps: number
      osName: string
      kernel: string
      swapTotal: number
      swapFree: number
      diskTotal: number
      diskFree: number
      congestion: string
      qdisc: string
      processes: number
    }
    cores: CoreHealth[]
  } | null
}

export type TargetResult = {
  host: string
  ip?: string
  good: boolean
  tls13: boolean
  h2: boolean
  certOk: boolean
  rttMs: number
  error?: string
}

export type LogEntry = { id: number; ts: string; level: 'debug' | 'info' | 'warn' | 'error'; source: string; msg: string; fields?: Record<string, string> }
export type LogFilter = { source?: string; level?: string; q?: string; before?: number }
export type PanelEvent = {
  id: number
  ts: string
  type: string
  severity: 'info' | 'warning' | 'danger'
  userId?: number
  userName?: string
  message: string
  data?: Record<string, unknown>
}
export type AuditRecord = { id: number; ts: string; admin: string; action: string; targetType: string; targetId: string; diff: unknown }

const query = (params: Record<string, string | number | undefined>) => {
  const q = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== '' && v !== 0) q.set(k, String(v))
  const s = q.toString()
  return s ? `?${s}` : ''
}

/** Live log stream URL (Server-Sent Events, same-origin cookie auth). */
export const logStreamUrl = (f: LogFilter) => basePath + 'api/logs/stream' + query({ source: f.source, level: f.level, q: f.q })

export type SessionInfo = { id: string; createdAt: string; expiresAt: string; ip: string; userAgent: string; current: boolean }
export type PanelSettings = { port: number; basePath: string; subPath: string; publicHost: string; decoy: '404' | 'drop' }

export const api = {
  account: {
    get: () => request<{ username: string; totpEnabled: boolean }>('GET', 'account'),
    changePassword: (current: string, next: string) => request<void>('POST', 'account/password', { current, next }),
    totpSetup: () => request<{ secret: string; uri: string }>('POST', 'account/totp/setup', {}),
    totpEnable: (secret: string, code: string) => request<void>('POST', 'account/totp/enable', { secret, code }),
    totpDisable: (password: string, code: string) => request<void>('POST', 'account/totp/disable', { password, code }),
    sessions: () => request<SessionInfo[]>('GET', 'account/sessions'),
    revokeSession: (id: string) => request<void>('DELETE', `account/sessions/${id}`),
    revokeOthers: () => request<{ revoked: number }>('POST', 'account/sessions/revoke-others', {}),
  },
  settings: {
    panel: () => request<{ settings: PanelSettings; pending: boolean; restartSupported: boolean }>('GET', 'settings/panel'),
    savePanel: (s: PanelSettings & { password: string }) => request<{ pending: boolean; url: string }>('PUT', 'settings/panel', s),
    restart: () => request<{ restarting: boolean }>('POST', 'settings/restart', {}),
  },
  logs: (f: LogFilter) => request<LogEntry[]>('GET', 'logs' + query({ ...f, limit: 300 })),
  events: (before?: number) => request<PanelEvent[]>('GET', 'events' + query({ before, limit: 100 })),
  audit: (before?: number) => request<AuditRecord[]>('GET', 'audit' + query({ before, limit: 100 })),
  authState: () => request<AuthState>('GET', 'auth/state'),
  setup: (username: string, password: string) =>
    request<{ admin: Admin }>('POST', 'auth/setup', { username, password }),
  login: (username: string, password: string, code?: string) =>
    request<{ admin: Admin }>('POST', 'auth/login', code ? { username, password, code } : { username, password }),
  logout: () => request<void>('POST', 'auth/logout'),
  system: () => request<SystemInfo>('GET', 'system'),
  domain: {
    get: () => request<DomainInfo>('GET', 'domain'),
    save: (d: { name: string; token: string; ipMode: string; ip: string; staging: boolean }) => request<DomainInfo>('PUT', 'domain', d),
    issue: () => request<{ started: boolean }>('POST', 'domain/issue', {}),
    remove: () => request<DomainInfo>('DELETE', 'domain'),
  },
  amnezia: {
    status: () => request<{ helper: boolean; status?: { docker: boolean; containers: AmneziaContainer[]; checkedAt: string } }>('GET', 'amnezia'),
    logs: (name: string) => request<string[]>('GET', 'amnezia/logs?tail=300&name=' + encodeURIComponent(name)),
  },
  update: {
    status: (check: boolean) =>
      request<{ current: string; repo: string; latest: { tag: string; url: string; published: string } | null; error: string; supported: boolean; pending: boolean; log?: string[] }>(
        'GET',
        'system/update' + (check ? '?check=1' : ''),
      ),
    start: (cores: boolean) => request<{ started: boolean }>('POST', 'system/update', { cores }),
  },
  restartCores: () => request<void>('POST', 'system/restart-cores', {}),
  reboot: (password: string) => request<{ rebooting: boolean }>('POST', 'system/reboot', { password }),
  online: () => request<OnlineEntry[]>('GET', 'online'),
  routing: {
    get: () => request<RoutingSettings>('GET', 'routing'),
    save: (s: RoutingSettings) =>
      request<RoutingSettings>('PUT', 'routing', {
        presets: s.presets,
        rules: s.rules.map(({ support: _s, ...r }) => r),
        warp: { mode: s.warp.mode },
        dns: s.dns,
        geo: s.geo,
        outbounds: s.outbounds.map(({ id, name, link }) => ({ id, name, link: link ?? '' })),
      }),
    registerWarp: () => request<RoutingSettings>('POST', 'routing/warp', {}),
    warpLicense: (license: string) => request<RoutingSettings>('POST', 'routing/warp/license', { license }),
    deleteWarp: () => request<RoutingSettings>('DELETE', 'routing/warp'),
    checkOutbound: (id: string) => request<{ trace?: Trace; error?: string }>('POST', `routing/outbounds/${id}/check`, {}),
    checkWarp: () => request<{ direct: Trace; warp?: Trace; error?: string }>('POST', 'routing/warp/check', {}),
    updateGeo: () => request<{ changed: boolean }>('POST', 'routing/geo/update', {}),
    geoCountries: () => request<{ countries: { code: string; hasSite: boolean }[]; ruBlocked: boolean }>('GET', 'routing/geo/countries'),
    geoCodes: (q: string) => request<string[]>('GET', 'routing/geo/codes?q=' + encodeURIComponent(q)),
  },
  trafficByDay: (days = 30) => request<DayTraffic[]>('GET', `stats/traffic?days=${days}`),
  users: {
    list: () => request<User[]>('GET', 'users'),
    create: (input: UserInput) => request<User>('POST', 'users', input),
    update: (id: number, input: UserInput) => request<User>('PUT', `users/${id}`, input),
    remove: (id: number) => request<void>('DELETE', `users/${id}`),
    links: (id: number) => request<UserLinks>('GET', `users/${id}/links`),
    traffic: (id: number, days = 30) => request<DayTraffic[]>('GET', `users/${id}/traffic?days=${days}`),
    extend: (id: number, days: number) => request<User>('POST', `users/${id}/extend`, { days }),
    resetTraffic: (id: number) => request<User>('POST', `users/${id}/reset-traffic`, {}),
  },
  inbounds: {
    list: () => request<Inbound[]>('GET', 'inbounds'),
    suggest: (protocol: Protocol) => request<InboundSuggest>('GET', `inbounds/suggest?protocol=${protocol}`),
    createTestKit: () => request<{ inbounds: number }>('POST', 'inbounds/test-kit', {}),
    scanTargets: (hosts: string[] = []) => request<TargetResult[]>('POST', 'inbounds/scan-targets', { hosts }),
    deleteTestKit: () => request<{ inbounds: number }>('DELETE', 'inbounds/test-kit'),
    create: (input: InboundInput) => request<Inbound>('POST', 'inbounds', input),
    update: (id: number, input: InboundInput) => request<Inbound>('PUT', `inbounds/${id}`, input),
    remove: (id: number) => request<void>('DELETE', `inbounds/${id}`),
  },
}
