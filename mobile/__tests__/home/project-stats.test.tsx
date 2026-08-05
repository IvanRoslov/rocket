/**
 * The project cards on the home tab (task #1077): a project whose tasks are
 * being brainstormed is working, not idle.
 */
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react-native'
import { SafeAreaProvider } from 'react-native-safe-area-context'
import { ToastProvider } from '../../src/components/Toast'
import { ServerProvider } from '../../src/servers/ServerContext'
import HomeScreen from '../../app/(tabs)/index'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

jest.mock('expo-router', () => ({
  router: { navigate: jest.fn(), push: jest.fn(), back: jest.fn() },
  useLocalSearchParams: () => ({}),
}))

const PROJECTS = [{ id: 'platform', name: 'Platform', main: 'rocket', linked: [], live_sessions: 0 }]

const TASKS = {
  tasks: [
    {
      id: 7,
      title: 'still arguing about the shape',
      project_id: 'platform',
      status: 'brainstorm',
      created_by: 'user',
      created_at: 1785622872,
      updated_at: 1785622879,
    },
  ],
}

function mockApi() {
  const bodies: Record<string, unknown> = {
    '/v1/projects': PROJECTS,
    '/v1/tasks': TASKS,
  }
  globalThis.fetch = jest.fn(async (url: string) => {
    const path = String(url).replace(/^https?:\/\/[^/]+/, '')
    if (!(path in bodies)) {
      return { ok: false, status: 404, json: async () => ({ error: { code: 'not_found' } }) }
    }
    return { ok: true, status: 200, json: async () => bodies[path] }
  }) as unknown as typeof fetch
}

function renderWithProviders(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } })
  const metrics = {
    frame: { x: 0, y: 0, width: 390, height: 844 },
    insets: { top: 47, left: 0, right: 0, bottom: 34 },
  }
  return render(
    <SafeAreaProvider initialMetrics={metrics}>
      <QueryClientProvider client={qc}>
        <ServerProvider>
          <ToastProvider>{ui}</ToastProvider>
        </ServerProvider>
      </QueryClientProvider>
    </SafeAreaProvider>,
  )
}

describe('project card counters', () => {
  afterEach(() => jest.restoreAllMocks())

  it('counts brainstormed tasks and does not call the project idle', async () => {
    mockApi()
    renderWithProviders(<HomeScreen />)

    await waitFor(() => expect(screen.getByText('1 brainstorm')).toBeTruthy())
    expect(screen.queryByText('idle')).toBeNull()
  })
})
