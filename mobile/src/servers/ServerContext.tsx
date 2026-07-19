import AsyncStorage from '@react-native-async-storage/async-storage'
import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'

export interface ServerEntry {
  id: string
  name: string
  host: string
  port: number
}

interface ServerContextValue {
  servers: ServerEntry[]
  activeId: string | null
  active: ServerEntry | null
  /** Base URL of the active server, e.g. "http://192.168.1.10:4477". */
  baseUrl: string | null
  loaded: boolean
  addServer: (s: Omit<ServerEntry, 'id'>) => void
  removeServer: (id: string) => void
  setActive: (id: string) => void
  activeProjectId: string | null
  setActiveProjectId: (id: string | null) => void
}

const STORAGE_KEY = 'rocket.servers.v1'

const Ctx = createContext<ServerContextValue | null>(null)

export function ServerProvider({ children }: { children: ReactNode }) {
  const [servers, setServers] = useState<ServerEntry[]>([])
  const [activeId, setActiveId] = useState<string | null>(null)
  const [activeProjectId, setActiveProjectId] = useState<string | null>(null)
  const [loaded, setLoaded] = useState(false)

  useEffect(() => {
    AsyncStorage.getItem(STORAGE_KEY)
      .then((raw) => {
        if (raw) {
          const parsed = JSON.parse(raw) as { servers: ServerEntry[]; activeId: string | null }
          setServers(parsed.servers ?? [])
          setActiveId(parsed.activeId ?? null)
        }
      })
      .catch(() => {})
      .finally(() => setLoaded(true))
  }, [])

  useEffect(() => {
    if (!loaded) return
    AsyncStorage.setItem(STORAGE_KEY, JSON.stringify({ servers, activeId })).catch(() => {})
  }, [servers, activeId, loaded])

  const value = useMemo<ServerContextValue>(() => {
    const active = servers.find((s) => s.id === activeId) ?? null
    return {
      servers,
      activeId,
      active,
      baseUrl: active ? `http://${active.host}:${active.port}` : null,
      loaded,
      addServer: (s) => {
        const id = `${s.host}:${s.port}`
        setServers((prev) => [...prev.filter((p) => p.id !== id), { ...s, id }])
        setActiveId(id)
      },
      removeServer: (id) => {
        setServers((prev) => prev.filter((p) => p.id !== id))
        setActiveId((cur) => (cur === id ? null : cur))
      },
      setActive: (id) => {
        setActiveId(id)
        setActiveProjectId(null)
      },
      activeProjectId,
      setActiveProjectId,
    }
  }, [servers, activeId, activeProjectId, loaded])

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}

export function useServers(): ServerContextValue {
  const v = useContext(Ctx)
  if (!v) throw new Error('useServers outside ServerProvider')
  return v
}
