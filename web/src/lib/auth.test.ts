import { describe, expect, it } from 'vitest'
import { browserLabel } from './auth'

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
