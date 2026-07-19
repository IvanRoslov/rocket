import { api, ApiError } from './client'

const BASE = 'http://10.0.0.5:4477'

function mockFetch(impl: (url: string, init?: RequestInit) => Promise<Partial<Response>>) {
  globalThis.fetch = jest.fn(impl) as unknown as typeof fetch
}

describe('api client', () => {
  afterEach(() => jest.restoreAllMocks())

  it('returns parsed JSON on success', async () => {
    mockFetch(async () => ({ ok: true, json: async () => ({ status: 'ok' }) }))
    await expect(api.get<{ status: string }>(BASE, '/v1/health')).resolves.toEqual({ status: 'ok' })
    expect(fetch).toHaveBeenCalledWith(`${BASE}/v1/health`, expect.anything())
  })

  it('throws ApiError with daemon error envelope', async () => {
    mockFetch(async () => ({
      ok: false,
      status: 409,
      json: async () => ({ error: { code: 'task_not_closed', message: 'tasks must be closed' } }),
    }))
    const err = (await api.get(BASE, '/v1/projects/x').catch((e) => e)) as ApiError
    expect(err).toBeInstanceOf(ApiError)
    expect(err.code).toBe('task_not_closed')
    expect(err.message).toBe('tasks must be closed')
    expect(err.status).toBe(409)
  })

  it('falls back to HTTP status for non-JSON error bodies', async () => {
    mockFetch(async () => ({
      ok: false,
      status: 502,
      json: async () => {
        throw new Error('not json')
      },
    }))
    const err = (await api.get(BASE, '/v1/health').catch((e) => e)) as ApiError
    expect(err).toBeInstanceOf(ApiError)
    expect(err.code).toBe('http_error')
    expect(err.message).toBe('HTTP 502')
  })

  it('serializes POST bodies as JSON', async () => {
    mockFetch(async () => ({ ok: true, json: async () => ({ id: 1 }) }))
    await api.post(BASE, '/v1/messages', { to: 'orch', body: 'hi' })
    const [, init] = (fetch as jest.Mock).mock.calls[0]
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body)).toEqual({ to: 'orch', body: 'hi' })
  })
})
