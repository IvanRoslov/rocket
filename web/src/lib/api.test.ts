import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { api, ApiError } from './api'
import { resetUnauthorizedLatch } from './auth'
import { projects } from '../mocks/fixtures'

const server = setupServer()

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => {
  server.resetHandlers()
  resetUnauthorizedLatch()
})
afterAll(() => server.close())

describe('api', () => {
  it('parses a successful GET /v1/projects response', async () => {
    server.use(http.get('/v1/projects', () => HttpResponse.json(projects)))

    const result = await api.get<typeof projects>('/v1/projects')

    expect(result).toEqual(projects)
  })

  it('throws ApiError with code/message/status on an error envelope', async () => {
    server.use(
      http.get('/v1/repos', () =>
        HttpResponse.json(
          { error: { code: 'repo_in_use', message: 'repo is still linked to a project' } },
          { status: 409 },
        ),
      ),
    )

    await expect(api.get('/v1/repos')).rejects.toMatchObject({
      status: 409,
      code: 'repo_in_use',
      message: 'repo is still linked to a project',
    })
    await expect(api.get('/v1/repos')).rejects.toBeInstanceOf(ApiError)
  })

  it('sends a JSON body on POST', async () => {
    let receivedBody: unknown
    let receivedContentType: string | null = null
    server.use(
      http.post('/v1/messages', async ({ request }) => {
        receivedContentType = request.headers.get('content-type')
        receivedBody = await request.json()
        return HttpResponse.json({ id: 1, status: 'queued' })
      }),
    )

    const body = { to: 's-billing-v2-orch', body: 'hello' }
    const result = await api.post<{ id: number; status: string }>('/v1/messages', body)

    expect(receivedBody).toEqual(body)
    expect(receivedContentType).toContain('application/json')
    expect(result).toEqual({ id: 1, status: 'queued' })
  })
})

describe('api 401 handling', () => {
  it('redirects to /login via the unauthorized handler and still throws ApiError', async () => {
    const assign = vi.fn()
    const loc = window.location
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: { href: 'http://localhost:3000/p/1?a=b', origin: 'http://localhost:3000', pathname: '/p/1', search: '?a=b', assign },
    })
    try {
      server.use(
        http.get('/v1/projects', () =>
          HttpResponse.json({ error: { code: 'unauthorized', message: 'no' } }, { status: 401 }),
        ),
      )
      await expect(api.get('/v1/projects')).rejects.toBeInstanceOf(ApiError)
      expect(assign).toHaveBeenCalledWith('/login?next=%2Fp%2F1%3Fa%3Db')
    } finally {
      Object.defineProperty(window, 'location', { configurable: true, value: loc })
    }
  })

  it('survives a failing assign and can redirect again later', async () => {
    const assign = vi
      .fn()
      .mockImplementationOnce(() => {
        throw new Error('Not implemented: navigation')
      })
      .mockImplementation(() => {})
    const loc = window.location
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: { href: 'http://localhost:3000/', origin: 'http://localhost:3000', pathname: '/', search: '', assign },
    })
    try {
      server.use(
        http.get('/v1/projects', () =>
          HttpResponse.json({ error: { code: 'unauthorized', message: 'no' } }, { status: 401 }),
        ),
      )
      await expect(api.get('/v1/projects')).rejects.toBeInstanceOf(ApiError)
      await expect(api.get('/v1/projects')).rejects.toBeInstanceOf(ApiError)
      expect(assign).toHaveBeenCalledTimes(2)
    } finally {
      Object.defineProperty(window, 'location', { configurable: true, value: loc })
    }
  })
})
