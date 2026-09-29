import { afterEach, describe, expect, it, vi } from 'vitest'
import { browserLabel, fetchAuthStatus, safeNext } from './auth'

describe('browserLabel', () => {
  it('names browser and OS', () => {
    expect(
      browserLabel('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36'),
    ).toBe('Chrome · macOS')
    expect(browserLabel('Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Version/18.0 Mobile/15E148 Safari/604.1')).toBe(
      'Safari · iOS',
    )
    expect(browserLabel('weird')).toBe('Браузер')
  })
})

describe('fetchAuthStatus', () => {
  afterEach(() => vi.unstubAllGlobals())
  const stub = (res: Response) => vi.stubGlobal('fetch', vi.fn().mockResolvedValue(res))

  it('401 means unauthenticated', async () => {
    stub(new Response('{}', { status: 401 }))
    await expect(fetchAuthStatus()).resolves.toEqual({ authenticated: false })
  })
  it('200 with authenticated:false means unauthenticated', async () => {
    stub(new Response(JSON.stringify({ authenticated: false }), { status: 200 }))
    await expect(fetchAuthStatus()).resolves.toEqual({ authenticated: false })
  })
  it('200 with authenticated:true passes through', async () => {
    stub(new Response(JSON.stringify({ authenticated: true }), { status: 200 }))
    await expect(fetchAuthStatus()).resolves.toEqual({ authenticated: true })
  })
  it('503 (daemon restarting) rejects instead of reporting unauthenticated', async () => {
    stub(new Response('', { status: 503 }))
    await expect(fetchAuthStatus()).rejects.toThrow()
  })
})

describe('safeNext', () => {
  it('accepts same-site paths', () => {
    expect(safeNext('/p/7')).toBe('/p/7')
  })
  it.each(['//evil', '/\\evil', 'https://x', '/login?next=/x', '/login', null])('rejects %s', (v) => {
    expect(safeNext(v)).toBe('/')
  })
})
