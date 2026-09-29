// Device-auth helpers for the dashboard (docs/03-daemon-api.md «Аутентификация»).
// The dashboard authenticates with an HttpOnly cookie set by POST /v1/auth/pair,
// so fetch/EventSource/WebSocket need no changes — only 401 handling.

import { ApiError } from './api'

export interface Device {
  id: number
  name: string
  kind: 'mobile' | 'web'
  created_at: number
  last_seen_at: number | null
  current?: boolean
}

export interface PairingCode {
  code: string
  expires_at: number
  url: string
}

let redirecting = false

/** Test hook: clears the once-only redirect latch. */
export function resetUnauthorizedLatch(): void {
  redirecting = false
}

export function onUnauthorized(): void {
  if (redirecting || window.location.pathname === '/login') return
  redirecting = true
  const next = window.location.pathname + window.location.search
  try {
    window.location.assign(`/login?next=${encodeURIComponent(next)}`)
  } catch {
    // Navigation unavailable (e.g. jsdom) or blocked: don't latch the flag,
    // or a long-lived tab would never redirect again.
    redirecting = false
  }
}

export async function fetchAuthStatus(): Promise<{ authenticated: boolean; device?: Device }> {
  const res = await fetch('/v1/auth/status')
  if (!res.ok) return { authenticated: false }
  return res.json()
}

export function browserLabel(ua: string): string {
  const browser = /Edg\//.test(ua)
    ? 'Edge'
    : /Firefox\//.test(ua)
      ? 'Firefox'
      : /Chrome\//.test(ua)
        ? 'Chrome'
        : /Safari\//.test(ua)
          ? 'Safari'
          : null
  const os = /iPhone|iPad/.test(ua)
    ? 'iOS'
    : /Android/.test(ua)
      ? 'Android'
      : /Mac OS X/.test(ua)
        ? 'macOS'
        : /Windows/.test(ua)
          ? 'Windows'
          : /Linux/.test(ua)
            ? 'Linux'
            : null
  if (!browser) return 'Браузер'
  return os ? `${browser} · ${os}` : browser
}

export async function pairWeb(code: string): Promise<void> {
  const res = await fetch('/v1/auth/pair', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ code, name: browserLabel(navigator.userAgent), kind: 'web' }),
  })
  if (!res.ok) {
    const body = (await res.json().catch(() => null)) as { error?: { code?: string; message?: string } } | null
    throw new ApiError(res.status, body?.error?.code ?? 'unknown', body?.error?.message ?? res.statusText)
  }
}

/** Only same-site paths are valid redirect targets after login. */
export function safeNext(next: string | null): string {
  return next && next.startsWith('/') && !next.startsWith('//') ? next : '/'
}
