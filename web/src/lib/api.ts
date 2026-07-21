// Thin fetch wrapper for the rocket daemon API. Errors come back as
// `{"error":{"code":"...","message":"..."}}`; we surface those as
// ApiError so callers can branch on `code` without parsing text.

export class ApiError extends Error {
  status: number
  code: string

  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

interface ErrorEnvelope {
  error?: { code?: string; message?: string }
}

async function req<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  if (!res.ok) {
    const payload = (await res.json().catch(() => null)) as ErrorEnvelope | null
    throw new ApiError(
      res.status,
      payload?.error?.code ?? 'unknown',
      payload?.error?.message ?? res.statusText,
    )
  }
  return res.status === 204 ? (undefined as T) : res.json()
}

/** Raw-body upload to POST /v1/attachments (the body IS the file; no JSON,
 * no multipart). Same error-envelope handling as `req`. */
async function upload(file: Blob): Promise<{ id: number; url: string }> {
  const res = await fetch('/v1/attachments', {
    method: 'POST',
    headers: { 'Content-Type': file.type },
    body: file,
  })
  if (!res.ok) {
    const payload = (await res.json().catch(() => null)) as ErrorEnvelope | null
    throw new ApiError(
      res.status,
      payload?.error?.code ?? 'unknown',
      payload?.error?.message ?? res.statusText,
    )
  }
  return res.json()
}

export const api = {
  get: <T>(p: string) => req<T>('GET', p),
  post: <T>(p: string, b?: unknown) => req<T>('POST', p, b),
  patch: <T>(p: string, b: unknown) => req<T>('PATCH', p, b),
  put: <T>(p: string, b: unknown) => req<T>('PUT', p, b),
  del: <T>(p: string) => req<T>('DELETE', p),
  upload,
}
