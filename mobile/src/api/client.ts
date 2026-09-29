export class ApiError extends Error {
  code: string
  status: number
  constructor(code: string, message: string, status: number) {
    super(message)
    this.code = code
    this.status = status
  }
}

const tokens = new Map<string, string>()
const unauthorizedListeners = new Set<(baseUrl: string) => void>()

export function setAuthToken(baseUrl: string, token: string | null): void {
  if (token) tokens.set(baseUrl, token)
  else tokens.delete(baseUrl)
}

export function authHeaders(baseUrl: string): Record<string, string> {
  const t = tokens.get(baseUrl)
  return t ? { Authorization: `Bearer ${t}` } : {}
}

export function onUnauthorized(cb: (baseUrl: string) => void): () => void {
  unauthorizedListeners.add(cb)
  return () => {
    unauthorizedListeners.delete(cb)
  }
}

export function notifyUnauthorized(baseUrl: string): void {
  unauthorizedListeners.forEach((cb) => cb(baseUrl))
}

async function request<T>(baseUrl: string, path: string, init?: RequestInit): Promise<T> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), 8000)
  let res: Response
  try {
    res = await fetch(`${baseUrl}${path}`, {
      ...init,
      signal: controller.signal,
      headers: { 'Content-Type': 'application/json', ...authHeaders(baseUrl), ...init?.headers },
    })
  } finally {
    clearTimeout(timer)
  }
  if (!res.ok) {
    if (res.status === 401) notifyUnauthorized(baseUrl)
    let code = 'http_error'
    let message = `HTTP ${res.status}`
    try {
      const body = (await res.json()) as { error?: { code?: string; message?: string } }
      if (body.error) {
        code = body.error.code ?? code
        message = body.error.message ?? message
      }
    } catch {
      // non-JSON error body
    }
    throw new ApiError(code, message, res.status)
  }
  if (res.status === 204) return undefined as T
  return (await res.json().catch(() => undefined)) as T
}

export const api = {
  get: <T>(baseUrl: string, path: string) => request<T>(baseUrl, path),
  post: <T>(baseUrl: string, path: string, body?: unknown) =>
    request<T>(baseUrl, path, { method: 'POST', body: body === undefined ? undefined : JSON.stringify(body) }),
  put: <T>(baseUrl: string, path: string, body: unknown) =>
    request<T>(baseUrl, path, { method: 'PUT', body: JSON.stringify(body) }),
  patch: <T>(baseUrl: string, path: string, body: unknown) =>
    request<T>(baseUrl, path, { method: 'PATCH', body: JSON.stringify(body) }),
  del: <T = void>(baseUrl: string, path: string) => request<T>(baseUrl, path, { method: 'DELETE' }),
}
