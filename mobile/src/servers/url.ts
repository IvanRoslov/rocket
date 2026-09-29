export interface ServerEntry {
  id: string
  name: string
  baseUrl: string
  hasToken: boolean
}

/** Trims, adds `http://` when no scheme is present, strips trailing slashes. */
export function normalizeBaseUrl(input: string): string {
  let s = input.trim()
  if (!/^[a-z][a-z0-9+.-]*:\/\//i.test(s)) s = `http://${s}`
  return s.replace(/\/+$/, '')
}

/**
 * Accepts persisted server records of either shape and returns v2 entries.
 * v1 `{id,name,host,port}` records become `http://host:port` without a token.
 * Anything unrecognised is dropped rather than crashing the app.
 */
export function migrateServers(raw: unknown): ServerEntry[] {
  if (!Array.isArray(raw)) return []
  const out: ServerEntry[] = []
  for (const r of raw) {
    if (!r || typeof r !== 'object') continue
    const o = r as Record<string, unknown>
    if (typeof o.baseUrl === 'string' && typeof o.name === 'string') {
      out.push({ id: typeof o.id === 'string' ? o.id : o.baseUrl, name: o.name, baseUrl: o.baseUrl, hasToken: o.hasToken === true })
    } else if (typeof o.host === 'string' && o.port !== undefined) {
      const baseUrl = `http://${o.host}:${o.port}`
      out.push({ id: baseUrl, name: typeof o.name === 'string' ? o.name : o.host, baseUrl, hasToken: false })
    }
  }
  return out
}
