// Display helpers for agent usage (task #5138): the period presets of the
// Usage screen and the token / $ / duration formats shared by its tables,
// the task Usage tab and Settings › Prices.

import { formatUptime } from './format'

/** YYYY-MM-DD in the viewer's local time — the daemon reads the period as its local dates. */
export function localDate(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

/** The last `days` calendar days, today included. */
export function presetRange(days: number, today: Date = new Date()): { from: string; to: string } {
  const from = new Date(today.getFullYear(), today.getMonth(), today.getDate() - (days - 1))
  return { from: localDate(from), to: localDate(today) }
}

const DATE_RE = /^\d{4}-\d{2}-\d{2}$/

/** Two well-formed dates, `from` not after `to` (ISO dates compare as strings). */
export function validRange(from: string, to: string): boolean {
  return DATE_RE.test(from) && DATE_RE.test(to) && from <= to
}

const compact = new Intl.NumberFormat('en', { notation: 'compact', maximumSignificantDigits: 3 })

/** 2_330_000 → "2.33M". */
export function formatTokens(n: number): string {
  return compact.format(n)
}

const usd = new Intl.NumberFormat('en', { style: 'currency', currency: 'USD', minimumFractionDigits: 2 })

/** null — the price is unknown, shown as a dash. */
export function formatCost(value: number | null): string {
  if (value === null) return '—'
  if (value > 0 && value < 0.01) return '<$0.01'
  return usd.format(value)
}

/** Wall-clock session lifetime; null while the session lives. */
export function formatDuration(seconds: number | null): string {
  if (seconds === null) return '—'
  if (seconds < 60) return `${seconds}s`
  return formatUptime(seconds)
}
