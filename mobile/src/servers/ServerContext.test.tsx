import AsyncStorage from '@react-native-async-storage/async-storage'
import { act, renderHook, waitFor } from '@testing-library/react-native'
import { ServerProvider, useServers } from './ServerContext'

jest.mock('@react-native-async-storage/async-storage', () =>
  require('@react-native-async-storage/async-storage/jest/async-storage-mock'),
)

const wrapper = ({ children }: { children: React.ReactNode }) => <ServerProvider>{children}</ServerProvider>

describe('ServerContext', () => {
  beforeEach(() => AsyncStorage.clear())

  it('starts empty and loaded', async () => {
    const { result } = await renderHook(() => useServers(), { wrapper })
    await waitFor(() => expect(result.current.loaded).toBe(true))
    expect(result.current.servers).toEqual([])
    expect(result.current.active).toBeNull()
    expect(result.current.baseUrl).toBeNull()
  })

  it('addServer activates it and persists', async () => {
    const { result } = await renderHook(() => useServers(), { wrapper })
    await waitFor(() => expect(result.current.loaded).toBe(true))
    await act(async () => result.current.addServer({ name: 'Desk', host: '192.168.1.10', port: 4477 }))
    expect(result.current.active?.name).toBe('Desk')
    expect(result.current.baseUrl).toBe('http://192.168.1.10:4477')
    await waitFor(async () => {
      const raw = await AsyncStorage.getItem('rocket.servers.v1')
      expect(JSON.parse(raw!).activeId).toBe('192.168.1.10:4477')
    })
  })

  it('removeServer clears active when removing the active one', async () => {
    const { result } = await renderHook(() => useServers(), { wrapper })
    await waitFor(() => expect(result.current.loaded).toBe(true))
    await act(async () => result.current.addServer({ name: 'A', host: '10.0.0.1', port: 4477 }))
    await act(async () => result.current.removeServer('10.0.0.1:4477'))
    expect(result.current.servers).toEqual([])
    expect(result.current.active).toBeNull()
  })

  it('switching servers resets active project', async () => {
    const { result } = await renderHook(() => useServers(), { wrapper })
    await waitFor(() => expect(result.current.loaded).toBe(true))
    await act(async () => result.current.addServer({ name: 'A', host: '10.0.0.1', port: 4477 }))
    await act(async () => result.current.addServer({ name: 'B', host: '10.0.0.2', port: 4477 }))
    await act(async () => result.current.setActiveProjectId('billing'))
    expect(result.current.activeProjectId).toBe('billing')
    await act(async () => result.current.setActive('10.0.0.1:4477'))
    expect(result.current.activeProjectId).toBeNull()
  })

  it('restores persisted state on mount', async () => {
    await AsyncStorage.setItem(
      'rocket.servers.v1',
      JSON.stringify({ servers: [{ id: 'x:1', name: 'X', host: 'x', port: 1 }], activeId: 'x:1' }),
    )
    const { result } = await renderHook(() => useServers(), { wrapper })
    await waitFor(() => expect(result.current.loaded).toBe(true))
    expect(result.current.active?.name).toBe('X')
  })
})
