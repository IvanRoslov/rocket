import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react-native'
import { SafeAreaProvider } from 'react-native-safe-area-context'
import PairScreen from '../../app/pair'
import ServersScreen from '../../app/servers'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

const mockAddServer = jest.fn()
const mockScan: { current: ((e: { data: string }) => void) | null } = { current: null }
const mockParams: { current: Record<string, string> } = { current: {} }

jest.mock('expo-router', () => ({
  router: { navigate: jest.fn(), push: jest.fn(), back: jest.fn(), replace: jest.fn() },
  useLocalSearchParams: () => mockParams.current,
}))

jest.mock('../../src/servers/ServerContext', () => ({
  useServers: () => ({ servers: [], activeId: null, addServer: mockAddServer, setActive: jest.fn(), removeServer: jest.fn() }),
}))

jest.mock('expo-camera', () => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { View } = require('react-native')
  return {
    useCameraPermissions: () => [{ granted: true }, jest.fn()],
    CameraView: (props: { onBarcodeScanned: (e: { data: string }) => void }) => {
      mockScan.current = props.onBarcodeScanned
      return <View testID="camera" />
    },
  }
})

// eslint-disable-next-line @typescript-eslint/no-require-imports
const { router } = require('expo-router') as { router: { replace: jest.Mock; push: jest.Mock } }

function mockPairFetch(status = 200, body: unknown = { token: 'rkt_x', device: { id: 1 } }) {
  const fn = jest.fn(async () => new Response(JSON.stringify(body), { status }))
  globalThis.fetch = fn as unknown as typeof fetch
  return fn
}

async function renderScreen(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } })
  const metrics = {
    frame: { x: 0, y: 0, width: 390, height: 844 },
    insets: { top: 47, left: 0, right: 0, bottom: 34 },
  }
  return await render(
    <SafeAreaProvider initialMetrics={metrics}>
      <QueryClientProvider client={qc}>{ui}</QueryClientProvider>
    </SafeAreaProvider>,
  )
}

async function openForm() {
  await fireEvent.press(await screen.findByText('Add server'))
}

beforeEach(() => {
  jest.clearAllMocks()
  mockAddServer.mockResolvedValue(undefined)
  mockParams.current = {}
})

describe('servers screen pairing form', () => {
  it('pairs manually with address + code', async () => {
    const fetchMock = mockPairFetch()
    await renderScreen(<ServersScreen />)
    await openForm()
    await fireEvent.changeText(screen.getByLabelText('Адрес'), 'https://m.ts.net')
    await fireEvent.changeText(screen.getByLabelText('Код'), 'AB12-CD34')
    await fireEvent.press(screen.getByText('Подключить'))
    await waitFor(() =>
      expect(mockAddServer).toHaveBeenCalledWith(
        expect.objectContaining({ baseUrl: 'https://m.ts.net', token: 'rkt_x' }),
      ),
    )
    expect(fetchMock.mock.calls.some((c) => (c as unknown[])[0] === 'https://m.ts.net/v1/auth/pair')).toBe(true)
    expect(router.replace).toHaveBeenCalledWith('/(tabs)')
  })

  it('rejects an empty or non-http address before any request', async () => {
    const fetchMock = mockPairFetch()
    await renderScreen(<ServersScreen />)
    await openForm()
    await fireEvent.changeText(screen.getByLabelText('Код'), 'AB12-CD34')
    await fireEvent.press(screen.getByText('Подключить'))
    expect(await screen.findByText(/Укажи адрес сервера/)).toBeTruthy()
    await fireEvent.changeText(screen.getByLabelText('Адрес'), 'ftp://m.ts.net')
    await fireEvent.press(screen.getByText('Подключить'))
    expect(await screen.findByText(/должен начинаться с http/)).toBeTruthy()
    expect(fetchMock).not.toHaveBeenCalled()
    expect(mockAddServer).not.toHaveBeenCalled()
  })

  it('shows the pairing error under the form', async () => {
    mockPairFetch(400, { error: { code: 'invalid_code', message: 'x' } })
    await renderScreen(<ServersScreen />)
    await openForm()
    await fireEvent.changeText(screen.getByLabelText('Адрес'), 'https://m.ts.net')
    await fireEvent.changeText(screen.getByLabelText('Код'), 'BAD')
    await fireEvent.press(screen.getByText('Подключить'))
    expect(await screen.findByText(/Код неверный, истёк или уже использован/)).toBeTruthy()
    expect(mockAddServer).not.toHaveBeenCalled()
  })

  it('shows a storage failure from addServer', async () => {
    mockPairFetch()
    mockAddServer.mockRejectedValue(new Error('SecureStore недоступен'))
    await renderScreen(<ServersScreen />)
    await openForm()
    await fireEvent.changeText(screen.getByLabelText('Адрес'), 'https://m.ts.net')
    await fireEvent.changeText(screen.getByLabelText('Код'), 'AB12-CD34')
    await fireEvent.press(screen.getByText('Подключить'))
    expect(await screen.findByText('SecureStore недоступен')).toBeTruthy()
    expect(router.replace).not.toHaveBeenCalled()
  })

  it('opens the scanner immediately with ?pair=1', async () => {
    mockParams.current = { pair: '1' }
    await renderScreen(<ServersScreen />)
    expect(await screen.findByText('Ввести вручную')).toBeTruthy()
    expect(screen.queryByText('Сканировать QR')).toBeNull()
  })

  it('routes a scanned QR to the confirm screen without pairing', async () => {
    const fetchMock = mockPairFetch()
    mockParams.current = { pair: '1' }
    await renderScreen(<ServersScreen />)
    await screen.findByText('Ввести вручную')
    await act(async () => {
      mockScan.current?.({ data: 'rocketmobile://pair?url=https%3A%2F%2Fm.ts.net&code=AB12-CD34' })
    })
    expect(router.push).toHaveBeenCalledWith({
      pathname: '/pair',
      params: { url: 'https://m.ts.net', code: 'AB12-CD34' },
    })
    expect(fetchMock).not.toHaveBeenCalled()
    expect(mockAddServer).not.toHaveBeenCalled()
    expect(router.replace).not.toHaveBeenCalled()
  })
})

describe('deep-link pair screen', () => {
  it('waits for a tap, then pairs', async () => {
    const fetchMock = mockPairFetch()
    mockParams.current = { code: 'AB12-CD34', url: 'https://m.ts.net' }
    await renderScreen(<PairScreen />)
    expect(await screen.findByText('https://m.ts.net')).toBeTruthy()
    expect(fetchMock).not.toHaveBeenCalled()
    await fireEvent.press(screen.getByText('Подключить'))
    await waitFor(() =>
      expect(mockAddServer).toHaveBeenCalledWith(
        expect.objectContaining({ baseUrl: 'https://m.ts.net', token: 'rkt_x' }),
      ),
    )
    expect(router.replace).toHaveBeenCalledWith('/(tabs)')
  })

  it('rejects a malformed link', async () => {
    mockParams.current = { url: 'javascript:alert(1)', code: 'X' }
    await renderScreen(<PairScreen />)
    expect(await screen.findByText('Ссылка сопряжения повреждена')).toBeTruthy()
    expect(screen.queryByText('Подключить')).toBeNull()
  })
})
