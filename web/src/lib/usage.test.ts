import { describe, expect, it } from 'vitest'
import { formatCost, formatDuration, formatTokens, localDate, presetRange, validRange } from './usage'

describe('usage helpers', () => {
  it('localDate renders the viewer-local calendar day', () => {
    expect(localDate(new Date(2026, 0, 5, 23, 59))).toBe('2026-01-05')
  })

  it('presetRange covers exactly N days ending today', () => {
    const today = new Date(2026, 9, 3, 12)
    expect(presetRange(7, today)).toEqual({ from: '2026-09-27', to: '2026-10-03' })
    expect(presetRange(30, today)).toEqual({ from: '2026-09-04', to: '2026-10-03' })
    expect(presetRange(90, today)).toEqual({ from: '2026-07-06', to: '2026-10-03' })
  })

  it('validRange wants two dates in order', () => {
    expect(validRange('2026-09-01', '2026-09-30')).toBe(true)
    expect(validRange('2026-09-30', '2026-09-30')).toBe(true)
    expect(validRange('2026-10-01', '2026-09-30')).toBe(false)
    expect(validRange('', '2026-09-30')).toBe(false)
    expect(validRange('2026-9-1', '2026-09-30')).toBe(false)
    expect(validRange('2026-02-30', '2026-03-10')).toBe(false)
    expect(validRange('2026-02-28', '2026-13-01')).toBe(false)
  })

  it('formatTokens is compact with three significant digits', () => {
    expect(formatTokens(0)).toBe('0')
    expect(formatTokens(950)).toBe('950')
    expect(formatTokens(145_000)).toBe('145K')
    expect(formatTokens(2_330_000)).toBe('2.33M')
    expect(formatTokens(1_250_000_000)).toBe('1.25B')
  })

  it('formatCost shows a dash for an unknown price', () => {
    expect(formatCost(null)).toBe('—')
    expect(formatCost(0)).toBe('$0.00')
    expect(formatCost(0.004)).toBe('<$0.01')
    expect(formatCost(34.857)).toBe('$34.86')
    expect(formatCost(1234.5)).toBe('$1,234.50')
  })

  it('formatDuration is short and human', () => {
    expect(formatDuration(null)).toBe('—')
    expect(formatDuration(42)).toBe('42s')
    expect(formatDuration(42 * 60)).toBe('42m')
    expect(formatDuration(2 * 3600 + 5 * 60)).toBe('2h 5m')
    expect(formatDuration(2 * 86400 - 90 * 60)).toBe('1d 22h')
  })
})
