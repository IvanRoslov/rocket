/**
 * The kanban tab (task #1077, spec v1 «статус brainstorm»): a task is
 * brainstormed before it is taken into work, so the board carries a
 * Brainstorm column between Backlog and In Progress.
 */
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native'
import { SafeAreaProvider } from 'react-native-safe-area-context'
import { ToastProvider } from '../../src/components/Toast'
import { ServerProvider } from '../../src/servers/ServerContext'
import KanbanScreen from '../../app/(tabs)/kanban'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

jest.mock('expo-router', () => ({
  router: { navigate: jest.fn(), push: jest.fn(), back: jest.fn() },
  useLocalSearchParams: () => ({}),
}))

const PROJECTS = [
  { id: 'platform', name: 'Platform', main: 'rocket', linked: [], live_sessions: 0 },
]

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
    {
      id: 8,
      title: 'ship the thing',
      project_id: 'platform',
      status: 'in_progress',
      created_by: 'user',
      created_at: 1785622872,
      updated_at: 1785622879,
    },
  ],
}

let patched: unknown[] = []

function mockApi(overrides: Record<string, unknown> = {}) {
  const bodies: Record<string, unknown> = {
    '/v1/projects': PROJECTS,
    '/v1/tasks?project=platform': TASKS,
    '/v1/sessions?project=platform': [],
    ...overrides,
  }
  globalThis.fetch = jest.fn(async (url: string, init?: { method?: string; body?: string }) => {
    const path = String(url).replace(/^https?:\/\/[^/]+/, '')
    if (init?.method === 'PATCH') {
      patched.push({ path, body: JSON.parse(init.body ?? '{}') })
      return { ok: true, status: 200, json: async () => ({}) }
    }
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

describe('kanban brainstorm column', () => {
  beforeEach(() => {
    patched = []
  })
  afterEach(() => jest.restoreAllMocks())

  it('sits between Backlog and In Progress', async () => {
    mockApi()
    renderWithProviders(<KanbanScreen />)

    await waitFor(() => expect(screen.getByText('Brainstorm')).toBeTruthy())
    const chips = ['Backlog', 'Brainstorm', 'In Progress', 'Review', 'Done'].map(
      (label) => screen.getByText(label),
    )
    expect(chips).toHaveLength(5)
  })

  it('shows the brainstormed tasks and their count', async () => {
    mockApi()
    renderWithProviders(<KanbanScreen />)

    await waitFor(() => expect(screen.getByText('ship the thing')).toBeTruthy())
    fireEvent.press(screen.getByText('Brainstorm'))
    await waitFor(() => expect(screen.getByText('still arguing about the shape')).toBeTruthy())
    expect(screen.queryByText('ship the thing')).toBeNull()
  })

  it('offers no Start orchestrator button once the task is brainstormed', async () => {
    mockApi()
    renderWithProviders(<KanbanScreen />)

    await waitFor(() => expect(screen.getByText('ship the thing')).toBeTruthy())
    fireEvent.press(screen.getByText('Brainstorm'))
    await waitFor(() => expect(screen.getByText('still arguing about the shape')).toBeTruthy())
    expect(screen.queryByText('Start orchestrator ▸')).toBeNull()
  })

  it('moves a task into Brainstorm from the card menu', async () => {
    mockApi()
    renderWithProviders(<KanbanScreen />)

    await waitFor(() => expect(screen.getByText('ship the thing')).toBeTruthy())
    fireEvent(screen.getByText('ship the thing'), 'longPress')
    fireEvent.press(await screen.findByText('Move to Brainstorm'))

    await waitFor(() => expect(patched).toHaveLength(1))
    expect(patched[0]).toEqual({ path: '/v1/tasks/8', body: { status: 'brainstorm' } })
  })
})
