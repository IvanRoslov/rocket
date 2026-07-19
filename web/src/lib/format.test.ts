import { describe, expect, it } from 'vitest'
import { formatBytes, formatUptime, shortSession, timeAgo } from './format'

describe('timeAgo', () => {
  const NOW = 1_800_000_000 // unix seconds

  it('accepts a unix-seconds number', () => {
    expect(timeAgo(NOW - 5 * 60, NOW)).toBe('5m ago')
  })

  it('accepts an ISO date string', () => {
    const iso = new Date((NOW - 5 * 60) * 1000).toISOString()
    expect(timeAgo(iso, NOW)).toBe('5m ago')
  })

  it('treats very large numbers as unix milliseconds', () => {
    const ms = (NOW - 5 * 60) * 1000
    expect(timeAgo(ms, NOW)).toBe('5m ago')
  })

  it('returns "just now" for very recent timestamps', () => {
    expect(timeAgo(NOW - 10, NOW)).toBe('just now')
  })
})

describe('shortSession', () => {
  it('strips a feature-slug prefix', () => {
    expect(shortSession('billing-v2-w1')).toBe('v2-w1')
  })

  it('leaves short names untouched', () => {
    expect(shortSession('orch')).toBe('orch')
  })
})

describe('formatBytes', () => {
  it('renders KB/MB/GB tiers', () => {
    expect(formatBytes(512 * 1024)).toBe('512 KB')
    expect(formatBytes(2000)).toBe('2 KB')
    expect(formatBytes(2 * 1024 * 1024 * 1024)).toBe('2.0 GB')
  })
})

describe('formatUptime', () => {
  it('renders d/h/m tiers', () => {
    expect(formatUptime(2 * 86400 + 4 * 3600)).toBe('2d 4h')
    expect(formatUptime(90 * 60)).toBe('1h 30m')
    expect(formatUptime(45 * 60)).toBe('45m')
  })
})
