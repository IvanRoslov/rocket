import AsyncStorage from '@react-native-async-storage/async-storage'
import { act, renderHook, waitFor } from '@testing-library/react-native'
import * as SecureStore from 'expo-secure-store'
import { authHeaders, notifyUnauthorized } from '../api/client'
import { ServerProvider, useServers } from './ServerContext'
import { tokenKey } from './tokens'
import { migrateServers, normalizeBaseUrl } from './url'

jest.mock('@react-native-async-storage/async-storage', () =>
  require('@react-native-async-storage/async-storage/jest/async-storage-mock'),
)

const wrapper = ({ children }: { children: React.ReactNode }) => <ServerProvider>{children}</ServerProvider>
const A = 'http://10.0.0.1:4477'
const B = 'http://10.0.0.2:4477'

describe('ServerContext', () => {
  beforeEach(async () => {
    await AsyncStorage.clear()
    ;(SecureStore as unknown as { __reset: () => void }).__reset()
  })

  it('starts empty and loaded', async () => {
    const { result } = await renderHook(() => useServers(), { wrapper })
    await waitFor(() => expect(result.current.loaded).toBe(true))
    expect(result.current.servers).toEqual([])
    expect(result.current.active).toBeNull()
    expect(result.current.baseUrl).toBeNull()
    expect(result.current.authLost).toBe(false)
  })

  it('addServer activates it and persists under v2', async () => {
    const { result } = await renderHook(() => useServers(), { wrapper })
    await waitFor(() => expect(result.current.loaded).toBe(true))
    await act(async () => result.current.addServer({ name: 'Desk', baseUrl: A, token: 'rkt_t' }))
    expect(result.current.active?.name).toBe('Desk')
    expect(result.current.baseUrl).toBe(A)
    expect(result.current.active?.hasToken).toBe(true)
    expect(authHeaders(A)).toEqual({ Authorization: 'Bearer rkt_t' })
    await waitFor(async () => {
      const raw = await AsyncStorage.getItem('rocket.servers.v2')
      expect(JSON.parse(raw!).activeId).toBe(A)
    })
  })

  it('removeServer clears active, token and SecureStore key', async () => {
    const { result } = await renderHook(() => useServers(), { wrapper })
    await waitFor(() => expect(result.current.loaded).toBe(true))
    await act(async () => result.current.addServer({ name: 'A', baseUrl: A, token: 'rkt_t' }))
    await act(async () => result.current.removeServer(A))
    expect(result.current.servers).toEqual([])
    expect(result.current.active).toBeNull()
    expect(authHeaders(A)).toEqual({})
    expect(await SecureStore.getItemAsync(tokenKey(A))).toBeNull()
  })

  it('switching servers resets active project', async () => {
    const { result } = await renderHook(() => useServers(), { wrapper })
    await waitFor(() => expect(result.current.loaded).toBe(true))
    await act(async () => result.current.addServer({ name: 'A', baseUrl: A, token: 'rkt_a' }))
    await act(async () => result.current.addServer({ name: 'B', baseUrl: B, token: 'rkt_b' }))
    await act(async () => result.current.setActiveProjectId('billing'))
    expect(result.current.activeProjectId).toBe('billing')
    await act(async () => result.current.setActive(A))
    expect(result.current.activeProjectId).toBeNull()
  })

  it('restores persisted v2 state and loads tokens from SecureStore', async () => {
    await SecureStore.setItemAsync(tokenKey('http://x:1'), 'rkt_x')
    await AsyncStorage.setItem(
      'rocket.servers.v2',
      JSON.stringify({ servers: [{ id: 'http://x:1', name: 'X', baseUrl: 'http://x:1', hasToken: true }], activeId: 'http://x:1' }),
    )
    const { result } = await renderHook(() => useServers(), { wrapper })
    await waitFor(() => expect(result.current.loaded).toBe(true))
    expect(result.current.active?.name).toBe('X')
    expect(result.current.authLost).toBe(false)
    expect(authHeaders('http://x:1')).toEqual({ Authorization: 'Bearer rkt_x' })
  })

  it('migrates v1 host/port records to baseUrl without token', async () => {
    await AsyncStorage.setItem(
      'rocket.servers.v1',
      JSON.stringify({
        servers: [{ id: '192.168.1.10:4477', name: 'Desk', host: '192.168.1.10', port: 4477 }],
        activeId: '192.168.1.10:4477',
      }),
    )
    const { result } = await renderHook(() => useServers(), { wrapper })
    await waitFor(() => expect(result.current.loaded).toBe(true))
    expect(result.current.servers[0]).toEqual({
      id: 'http://192.168.1.10:4477',
      name: 'Desk',
      baseUrl: 'http://192.168.1.10:4477',
      hasToken: false,
    })
    expect(result.current.baseUrl).toBe('http://192.168.1.10:4477')
    expect(result.current.authLost).toBe(true)
  })

  it('addServer stores the token in SecureStore, not AsyncStorage', async () => {
    const { result } = await renderHook(() => useServers(), { wrapper })
    await waitFor(() => expect(result.current.loaded).toBe(true))
    await act(async () => result.current.addServer({ name: 'Mac', baseUrl: 'https://mac.tail1.ts.net/', token: 'rkt_x' }))
    expect(result.current.baseUrl).toBe('https://mac.tail1.ts.net')
    expect(await SecureStore.getItemAsync('rocket.token.https___mac.tail1.ts.net')).toBe('rkt_x')
    await waitFor(async () => expect(await AsyncStorage.getItem('rocket.servers.v2')).not.toBeNull())
    const raw = await AsyncStorage.getItem('rocket.servers.v2')
    expect(raw).not.toContain('rkt_x')
    expect(result.current.authLost).toBe(false)
  })

  it('a 401 for the active server raises authLost; re-adding clears it', async () => {
    const { result } = await renderHook(() => useServers(), { wrapper })
    await waitFor(() => expect(result.current.loaded).toBe(true))
    await act(async () => result.current.addServer({ name: 'A', baseUrl: A, token: 'rkt_t' }))
    await act(async () => notifyUnauthorized(B))
    expect(result.current.authLost).toBe(false)
    await act(async () => notifyUnauthorized(A))
    expect(result.current.authLost).toBe(true)
    await act(async () => result.current.addServer({ name: 'A', baseUrl: A, token: 'rkt_new' }))
    expect(result.current.authLost).toBe(false)
  })
})

describe('url helpers', () => {
  it('normalizeBaseUrl', () => {
    expect(normalizeBaseUrl(' 10.0.0.5:4477/ ')).toBe('http://10.0.0.5:4477')
    expect(normalizeBaseUrl('https://m.ts.net')).toBe('https://m.ts.net')
  })

  it('migrateServers tolerates garbage and passes v2 through', () => {
    expect(migrateServers(null)).toEqual([])
    expect(migrateServers([{ nope: 1 }])).toEqual([])
    const v2 = { id: 'http://a:1', name: 'a', baseUrl: 'http://a:1', hasToken: true }
    expect(migrateServers([v2])).toEqual([v2])
  })

  it('tokenKey only uses SecureStore-legal characters', () => {
    expect(tokenKey('https://mac.tail1.ts.net')).toBe('rocket.token.https___mac.tail1.ts.net')
    expect(tokenKey('http://10.0.0.1:4477')).toMatch(/^[A-Za-z0-9._-]+$/)
  })
})
