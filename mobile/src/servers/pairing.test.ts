import { pairWithServer, parsePairLink, validatePairInput } from './pairing'

describe('parsePairLink', () => {
  it('parses the rocket pair QR', () => {
    expect(parsePairLink('rocketmobile://pair?code=AB12-CD34&url=https%3A%2F%2Fmac.tail1.ts.net')).toEqual({
      baseUrl: 'https://mac.tail1.ts.net',
      code: 'AB12-CD34',
    })
  })
  it('parses either query order', () => {
    expect(parsePairLink('rocketmobile://pair?url=https%3A%2F%2Fmac.tail1.ts.net%2F&code=AB12-CD34')).toEqual({
      baseUrl: 'https://mac.tail1.ts.net',
      code: 'AB12-CD34',
    })
  })
  it('rejects other QRs', () => {
    expect(parsePairLink('https://example.com')).toBeNull()
    expect(parsePairLink('rocketmobile://pair?code=AB12-CD34')).toBeNull()
    expect(parsePairLink('rocketmobile://pair?url=javascript%3Aalert(1)&code=X')).toBeNull()
  })
})

describe('validatePairInput', () => {
  it('normalizes a valid address', () => {
    expect(validatePairInput(' https://m.ts.net/ ')).toEqual({ baseUrl: 'https://m.ts.net' })
  })
  it('rejects empty and non-http addresses', () => {
    expect(validatePairInput('   ')).toEqual({ error: expect.stringContaining('адрес') })
    expect(validatePairInput('ftp://m.ts.net')).toEqual({ error: expect.stringContaining('http') })
  })
})

describe('pairWithServer', () => {
  it('returns the token', async () => {
    globalThis.fetch = jest.fn(async (url: string, init: RequestInit) => {
      expect(url).toBe('https://m.ts.net/v1/auth/pair')
      expect(JSON.parse(init.body as string)).toEqual({ code: 'AB12-CD34', name: 'iPhone', kind: 'mobile' })
      return new Response(JSON.stringify({ token: 'rkt_x', device: { id: 1 } }), { status: 200 })
    }) as unknown as typeof fetch
    await expect(pairWithServer('https://m.ts.net', 'AB12-CD34', 'iPhone')).resolves.toEqual({ token: 'rkt_x' })
  })
  it('maps invalid_code to a human message', async () => {
    globalThis.fetch = jest.fn(async () =>
      new Response(JSON.stringify({ error: { code: 'invalid_code', message: 'x' } }), { status: 400 }),
    ) as unknown as typeof fetch
    await expect(pairWithServer('https://m.ts.net', 'X', 'iPhone')).rejects.toThrow('Код неверный, истёк или уже использован')
  })
  it('maps rate_limited and network errors', async () => {
    globalThis.fetch = jest.fn(async () =>
      new Response(JSON.stringify({ error: { code: 'rate_limited', message: 'x' } }), { status: 429 }),
    ) as unknown as typeof fetch
    await expect(pairWithServer('https://m.ts.net', 'X', 'iPhone')).rejects.toThrow('Слишком много попыток')
    globalThis.fetch = jest.fn(async () => {
      throw new TypeError('Network request failed')
    }) as unknown as typeof fetch
    await expect(pairWithServer('https://m.ts.net', 'X', 'iPhone')).rejects.toThrow('Сервер недоступен')
  })
})
