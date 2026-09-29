import AsyncStorage from '@react-native-async-storage/async-storage'
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { onUnauthorized, setAuthToken } from '../api/client'
import { deleteToken, getToken, setToken } from './tokens'
import { migrateServers, normalizeBaseUrl, type ServerEntry } from './url'

export type { ServerEntry } from './url'
export { migrateServers, normalizeBaseUrl } from './url'

interface ServerContextValue {
  servers: ServerEntry[]
  activeId: string | null
  active: ServerEntry | null
  /** Base URL of the active server, e.g. "https://mac.tailnet.ts.net". */
  baseUrl: string | null
  loaded: boolean
  /** The active server has no device token, or the daemon answered 401. */
  authLost: boolean
  addServer: (s: { name: string; baseUrl: string; token: string }) => Promise<void>
  removeServer: (id: string) => Promise<void>
  setActive: (id: string) => void
  activeProjectId: string | null
  setActiveProjectId: (id: string | null) => void
}

const STORAGE_KEY = 'rocket.servers.v2'
const LEGACY_STORAGE_KEY = 'rocket.servers.v1'

const Ctx = createContext<ServerContextValue | null>(null)

export function ServerProvider({ children }: { children: ReactNode }) {
  const [servers, setServers] = useState<ServerEntry[]>([])
  const [activeId, setActiveId] = useState<string | null>(null)
  const [activeProjectId, setActiveProjectId] = useState<string | null>(null)
  const [loaded, setLoaded] = useState(false)
  const [unauthorizedIds, setUnauthorizedIds] = useState<ReadonlySet<string>>(new Set())

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      try {
        const rawV2 = await AsyncStorage.getItem(STORAGE_KEY)
        const raw = rawV2 ?? (await AsyncStorage.getItem(LEGACY_STORAGE_KEY))
        if (!raw) return
        const parsed = JSON.parse(raw) as { servers?: unknown; activeId?: string | null }
        const list = migrateServers(parsed.servers)
        // v1 ids were "host:port"; entries migrate to id === baseUrl.
        const legacyId = rawV2 ? null : (parsed.activeId ?? null)
        const migratedActive =
          legacyId === null ? null : (list.find((s) => s.baseUrl === `http://${legacyId}`)?.id ?? null)
        const withTokens = await Promise.all(
          list.map(async (s) => {
            const token = await getToken(s.baseUrl)
            setAuthToken(s.baseUrl, token)
            return { ...s, hasToken: token !== null }
          }),
        )
        if (cancelled) return
        setServers(withTokens)
        setActiveId(rawV2 ? (parsed.activeId ?? null) : migratedActive)
      } catch {
        // corrupt storage: start empty
      } finally {
        if (!cancelled) setLoaded(true)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    if (!loaded) return
    AsyncStorage.setItem(STORAGE_KEY, JSON.stringify({ servers, activeId })).catch(() => {})
  }, [servers, activeId, loaded])

  useEffect(
    () =>
      onUnauthorized((baseUrl) =>
        setUnauthorizedIds((prev) => (prev.has(baseUrl) ? prev : new Set(prev).add(baseUrl))),
      ),
    [],
  )

  const addServer = useCallback(async (s: { name: string; baseUrl: string; token: string }) => {
    const baseUrl = normalizeBaseUrl(s.baseUrl)
    await setToken(baseUrl, s.token)
    setAuthToken(baseUrl, s.token)
    setServers((prev) => [
      ...prev.filter((p) => p.id !== baseUrl),
      { id: baseUrl, name: s.name, baseUrl, hasToken: true },
    ])
    setUnauthorizedIds((prev) => {
      if (!prev.has(baseUrl)) return prev
      const next = new Set(prev)
      next.delete(baseUrl)
      return next
    })
    setActiveId(baseUrl)
  }, [])

  const removeServer = useCallback(async (id: string) => {
    setServers((prev) => prev.filter((p) => p.id !== id))
    setActiveId((cur) => (cur === id ? null : cur))
    setAuthToken(id, null)
    await deleteToken(id)
  }, [])

  const value = useMemo<ServerContextValue>(() => {
    const active = servers.find((s) => s.id === activeId) ?? null
    return {
      servers,
      activeId,
      active,
      baseUrl: active ? active.baseUrl : null,
      loaded,
      authLost: active !== null && (!active.hasToken || unauthorizedIds.has(active.baseUrl)),
      addServer,
      removeServer,
      setActive: (id) => {
        setActiveId(id)
        setActiveProjectId(null)
      },
      activeProjectId,
      setActiveProjectId,
    }
  }, [servers, activeId, activeProjectId, loaded, unauthorizedIds, addServer, removeServer])

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}

export function useServers(): ServerContextValue {
  const v = useContext(Ctx)
  if (!v) throw new Error('useServers outside ServerProvider')
  return v
}
