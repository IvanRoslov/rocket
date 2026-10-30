import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render } from '@testing-library/react-native'
import { SafeAreaProvider } from 'react-native-safe-area-context'
import { ToastProvider } from '../../src/components/Toast'
import { ServerProvider } from '../../src/servers/ServerContext'
import SystemScreen from '../../app/system'

jest.mock('expo-router', () => ({ router: { back: jest.fn() } }))
jest.mock('../../src/api/queries', () => ({
  useSystem: () => ({ data: undefined, isError: false, refetch: jest.fn() }),
  useSessions: () => ({ data: [] }),
  useSystemCleanup: () => ({ mutate: jest.fn(), isPending: false }),
}))

test('Agents running names every registered adapter', async () => {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const metrics = { frame: { x: 0, y: 0, width: 390, height: 844 }, insets: { top: 47, left: 0, right: 0, bottom: 34 } }
  const { getByText } = await render(
    <SafeAreaProvider initialMetrics={metrics}>
      <QueryClientProvider client={queryClient}>
        <ServerProvider>
          <ToastProvider>
            <SystemScreen />
          </ToastProvider>
        </ServerProvider>
      </QueryClientProvider>
    </SafeAreaProvider>,
  )

  expect(getByText('claude/codex/kimi/pi/deepseek')).toBeTruthy()
})
