/**
 * The Milestones tab (task #1023, spec v2 «Дашборд и mobile»): the work the
 * persistent agents hold. Rendered against the daemon's real JSON shapes, so
 * a contract drift fails here rather than in the app.
 */
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native'
import { router } from 'expo-router'
import { SafeAreaProvider } from 'react-native-safe-area-context'
import { ToastProvider } from '../../src/components/Toast'
import { ServerProvider } from '../../src/servers/ServerContext'
import MilestonesScreen from '../../app/(tabs)/milestones'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

jest.mock('expo-router', () => ({
  router: { navigate: jest.fn(), push: jest.fn(), back: jest.fn() },
  useLocalSearchParams: () => ({}),
}))

const AGENTS = [
  {
    id: 'sre',
    description: 'platform on-call',
    project: '',
    dir: '',
    command: 'claude',
    enabled: true,
    session_alive: true,
    unread: 0,
    open_questions: 0,
    awaiting_user: 0,
    milestones: [{ id: 41, title: 'Own the incident review ritual', status: 'in_progress' }],
    created_at: 1785622872,
    updated_at: 1785622879,
  },
  {
    id: 'librarian',
    description: 'keeps the docs honest',
    project: '',
    dir: '',
    command: 'claude',
    enabled: true,
    session_alive: false,
    unread: 0,
    open_questions: 0,
    awaiting_user: 0,
    milestones: [],
    created_at: 1785622872,
    updated_at: 1785622879,
  },
]

const MILESTONES = {
  tasks: [
    {
      id: 41,
      title: 'Own the incident review ritual',
      project_id: '',
      status: 'in_progress',
      milestone: true,
      assigned_role: 'sre',
      quiet: true,
      created_by: 'user',
      created_at: 1785622872,
      updated_at: 1785622879,
      open_questions: 2,
      questions_awaiting_user: 1,
    },
    {
      id: 40,
      title: 'Cut the on-call pager noise in half',
      project_id: '',
      status: 'backlog',
      milestone: true,
      created_by: 'user',
      created_at: 1785622872,
      updated_at: 1785622879,
      open_questions: 0,
      questions_awaiting_user: 0,
    },
  ],
}

let assigned: unknown[] = []

function mockApi(overrides: Record<string, unknown> = {}) {
  const bodies: Record<string, unknown> = {
    '/v1/tasks?milestones=true': MILESTONES,
    '/v1/agents': AGENTS,
    '/v1/projects': [],
    ...overrides,
  }
  globalThis.fetch = jest.fn(async (url: string, init?: { method?: string; body?: string }) => {
    const path = String(url).replace(/^https?:\/\/[^/]+/, '')
    if (init?.method === 'POST' && path.endsWith('/assign')) {
      assigned.push({ path, body: JSON.parse(init.body ?? '{}') })
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

describe('MilestonesScreen', () => {
  beforeEach(() => {
    assigned = []
  })
  afterEach(() => jest.restoreAllMocks())

  it('lists the in-progress milestones with their holder and badges', async () => {
    mockApi()
    renderWithProviders(<MilestonesScreen />)

    await waitFor(() => expect(screen.getByText('Own the incident review ritual')).toBeTruthy())
    expect(screen.getByText('◆ sre')).toBeTruthy()
    expect(screen.getByText(/quiet/)).toBeTruthy()
    expect(screen.getByText('? 1 awaiting you')).toBeTruthy()
  })

  it('a milestone nobody took says so', async () => {
    mockApi()
    renderWithProviders(<MilestonesScreen />)

    await waitFor(() => expect(screen.getByText('Own the incident review ritual')).toBeTruthy())
    fireEvent.press(screen.getByText('Backlog'))
    await waitFor(() => expect(screen.getByText('Cut the on-call pager noise in half')).toBeTruthy())
    expect(screen.getByText('not taken')).toBeTruthy()
  })

  it('opens the milestone card on tap', async () => {
    mockApi()
    renderWithProviders(<MilestonesScreen />)

    await waitFor(() => expect(screen.getByText('Own the incident review ritual')).toBeTruthy())
    fireEvent.press(screen.getByText('Own the incident review ritual'))
    expect(router.navigate).toHaveBeenCalledWith('/task/41')
  })

  it('chats with the holding agent, not with a task session', async () => {
    mockApi()
    renderWithProviders(<MilestonesScreen />)

    await waitFor(() => expect(screen.getByText('Own the incident review ritual')).toBeTruthy())
    fireEvent.press(screen.getByText('💬 chat'))
    expect(router.navigate).toHaveBeenCalledWith('/chat/sre?agent=1')
  })

  it('assigns an agent from the card', async () => {
    mockApi()
    renderWithProviders(<MilestonesScreen />)

    await waitFor(() => expect(screen.getByText('Own the incident review ritual')).toBeTruthy())
    fireEvent.press(screen.getByText('◆ assign'))
    fireEvent.press(await screen.findByText('librarian'))

    await waitFor(() => expect(assigned).toHaveLength(1))
    expect(assigned[0]).toEqual({ path: '/v1/tasks/41/assign', body: { agent_id: 'librarian' } })
  })
})
