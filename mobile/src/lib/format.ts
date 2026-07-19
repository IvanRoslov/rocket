import type { SessionActivity, SessionState } from '../api/types'
import { colors } from '../theme'

/** "12m ago" style relative time from a unix-seconds timestamp. */
export function ago(ts: number | undefined): string {
  if (!ts) return ''
  const s = Math.max(0, Math.floor(Date.now() / 1000 - ts))
  if (s < 60) return 'just now'
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  return `${Math.floor(h / 24)}d ago`
}

export function bytes(n: number): string {
  if (n >= 1 << 30) return `${(n / (1 << 30)).toFixed(1)} GB`
  if (n >= 1 << 20) return `${Math.round(n / (1 << 20))} MB`
  if (n >= 1 << 10) return `${Math.round(n / (1 << 10))} KB`
  return `${n} B`
}

export function uptime(sec: number): string {
  const d = Math.floor(sec / 86400)
  const h = Math.floor((sec % 86400) / 3600)
  const m = Math.floor((sec % 3600) / 60)
  if (d > 0) return `${d}d ${h}h`
  if (h > 0) return `${h}h ${m}m`
  return `${m}m`
}

/** Status dot color for a session, matching the mockups. */
export function sessionDot(state: SessionState, activity?: SessionActivity): string {
  if (state === 'errored' || activity === 'blocked') return colors.red
  if (state === 'killed' || state === 'done' || activity === 'exited') return colors.slate
  if (activity === 'active' || activity === 'ready') return colors.green
  if (activity === 'idle' || activity === 'waiting_input' || state === 'spawning') return colors.amber
  return colors.green
}

/** Badge palette per session activity/state label. */
export function sessionBadge(state: SessionState, activity?: SessionActivity): {
  label: string
  fg: string
  bg: string
} {
  const label = state === 'running' ? (activity ?? 'running') : state
  if (label === 'active' || label === 'ready' || label === 'running')
    return { label, fg: colors.greenFg, bg: colors.greenBg }
  if (label === 'blocked' || label === 'errored') return { label, fg: colors.redFg, bg: colors.redBg }
  if (label === 'idle' || label === 'waiting_input' || label === 'spawning')
    return { label, fg: colors.amberFg, bg: colors.amberBg }
  return { label, fg: colors.slateFg, bg: colors.slateBg }
}

/** Strip ANSI escape sequences from tmux capture-pane output. */
export function stripAnsi(s: string): string {
  // eslint-disable-next-line no-control-regex
  return s.replace(/\[[0-9;?]*[a-zA-Z]|\][^]*|[()][0-9A-B]/g, '')
}
