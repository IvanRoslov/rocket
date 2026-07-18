// Small display-formatting helpers shared across screens.

/** Renders a unix-seconds timestamp as a short relative string, e.g. "12m ago". */
export function timeAgo(ts: number, now: number = Date.now() / 1000): string {
  const diff = Math.max(0, Math.floor(now - ts))

  if (diff < 60) return 'just now'

  const units: Array<[label: string, secs: number]> = [
    ['y', 365 * 24 * 60 * 60],
    ['mo', 30 * 24 * 60 * 60],
    ['d', 24 * 60 * 60],
    ['h', 60 * 60],
    ['m', 60],
  ]

  for (const [label, secs] of units) {
    if (diff >= secs) {
      const n = Math.floor(diff / secs)
      return `${n}${label} ago`
    }
  }

  return 'just now'
}

/** Strips a feature-slug prefix off a tmux session name for compact display. */
export function shortSession(name: string): string {
  const parts = name.split('-')
  if (parts.length <= 2) return name
  return parts.slice(-2).join('-')
}

/** Renders a byte count as a short human string, e.g. "612 MB" / "1.8 GB". */
export function formatBytes(bytes: number): string {
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`
  const mb = bytes / (1024 * 1024)
  if (mb < 1024) return `${Math.round(mb)} MB`
  const gb = mb / 1024
  return `${gb.toFixed(1)} GB`
}

/** Renders a duration in seconds as a short human string, e.g. "2d 4h". */
export function formatUptime(seconds: number): string {
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (d > 0) return `${d}d ${h}h`
  if (h > 0) return `${h}h ${m}m`
  return `${m}m`
}
