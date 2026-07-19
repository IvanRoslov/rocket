export class ApiError extends Error {
  code: string
  status: number
  constructor(code: string, message: string, status: number) {
    super(message)
    this.code = code
    this.status = status
  }
}

async function request<T>(baseUrl: string, path: string, init?: RequestInit): Promise<T> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), 8000)
  let res: Response
  try {
    res = await fetch(`${baseUrl}${path}`, {
      ...init,
      signal: controller.signal,
      headers: { 'Content-Type': 'application/json', ...init?.headers },
    })
  } finally {
    clearTimeout(timer)
  }
  if (!res.ok) {
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
  return (await res.json()) as T
}

export const api = {
  get: <T>(baseUrl: string, path: string) => request<T>(baseUrl, path),
  post: <T>(baseUrl: string, path: string, body?: unknown) =>
    request<T>(baseUrl, path, { method: 'POST', body: body === undefined ? undefined : JSON.stringify(body) }),
  put: <T>(baseUrl: string, path: string, body: unknown) =>
    request<T>(baseUrl, path, { method: 'PUT', body: JSON.stringify(body) }),
  patch: <T>(baseUrl: string, path: string, body: unknown) =>
    request<T>(baseUrl, path, { method: 'PATCH', body: JSON.stringify(body) }),
}
