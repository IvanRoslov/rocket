/**
 * Renders the two agent screens against the daemon's real JSON shapes
 * (captured from `/v1/agents*` on a live rocketd), so a contract drift or a
 * crashing branch fails here rather than in the app.
 */
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native'
import { SafeAreaProvider } from 'react-native-safe-area-context'
import { ToastProvider } from '../../src/components/Toast'
import { ServerProvider } from '../../src/servers/ServerContext'
import AgentsScreen from '../(tabs)/agents'
import AgentScreen from './[id]'

// react-query's polling re-renders outside act(); mark the environment so
// React reports them as handled instead of warning on every tick.
;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

jest.mock('expo-router', () => ({
  router: { navigate: jest.fn(), push: jest.fn(), back: jest.fn() },
  useLocalSearchParams: () => ({ id: 'sre' }),
}))

const AGENT = {
  id: 'sre',
  description: 'platform on-call',
  project: 'platform',
  dir: '/home/agents/sre',
  command: 'claude',
  enabled: true,
  session_alive: false,
  unread: 2,
  open_questions: 1,
  awaiting_user: 1,
  created_at: 1785622872,
  updated_at: 1785622879,
}

const QUESTIONS = {
  questions: [
    {
      id: 1,
      role_id: 'sre',
      ordinal: 1,
      asked_by: '',
      body: 'status?',
      context: 'from mobile',
      status: 'open',
      whose_turn: 'role',
      asked_at: 1785622879,
      messages: [],
    },
  ],
}

const INBOX = [
  { id: 7, from: 'ivan', body: 'db is down', status: 'unread', created_at: 1785622879 },
  { id: 6, from: 'orch', body: 'take #12', status: 'read', created_at: 1785622800, read_at: 1785622850 },
]

/** Routes a request path to a canned body; unknown paths 404 like the daemon. */
function mockApi(overrides: Record<string, unknown> = {}) {
  const bodies: Record<string, unknown> = {
    '/v1/agents': [AGENT],
    '/v1/agents?project=platform': [AGENT],
    '/v1/agents/sre': AGENT,
    '/v1/agents/sre/questions': QUESTIONS,
    '/v1/agents/sre/inbox': INBOX,
    '/v1/sessions': [],
    '/v1/sessions?project=platform': [],
    '/v1/projects': [{ id: 'platform', name: 'Platform', main: 'demo', linked: [], live_sessions: 0, created_at: 1 }],
    ...overrides,
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

describe('AgentsScreen', () => {
  afterEach(() => jest.restoreAllMocks())

  it('lists agents with their description and badges', async () => {
    mockApi()
    renderWithProviders(<AgentsScreen />)
    await waitFor(() => expect(screen.getByText('sre')).toBeTruthy())
    expect(screen.getByText('platform on-call')).toBeTruthy()
    expect(screen.getByText('? 1 awaiting you')).toBeTruthy()
    expect(screen.getByText('2 unread')).toBeTruthy()
    expect(screen.getByText('1 open Q')).toBeTruthy()
  })

  it('shows an empty state when the project has no agents', async () => {
    mockApi({ '/v1/agents': [] })
    renderWithProviders(<AgentsScreen />)
    await waitFor(() => expect(screen.getByText(/No agents yet/)).toBeTruthy())
  })
})

describe('AgentScreen', () => {
  afterEach(() => jest.restoreAllMocks())

  it('opens on the questions tab and renders the thread', async () => {
    mockApi()
    renderWithProviders(<AgentScreen />)
    await waitFor(() => expect(screen.getByText('status?')).toBeTruthy())
    // The user opened this thread, so the agent owes the answer.
    expect(screen.getByText('waiting for sre')).toBeTruthy()
    expect(screen.getByText('Q1')).toBeTruthy()
  })

  it('renders the inbox messages with sender and status', async () => {
    mockApi()
    renderWithProviders(<AgentScreen />)
    await waitFor(() => expect(screen.getByText('Inbox')).toBeTruthy())
    fireEvent.press(screen.getByText('Inbox'))
    await waitFor(() => expect(screen.getByText('db is down')).toBeTruthy())
    expect(screen.getByText('ivan')).toBeTruthy()
    expect(screen.getByText('unread')).toBeTruthy()
    expect(screen.getByText('take #12')).toBeTruthy()
  })

  it('offers Start while the session is dead', async () => {
    mockApi()
    renderWithProviders(<AgentScreen />)
    await waitFor(() => expect(screen.getByText('Start')).toBeTruthy())
  })

  it('drops Start for a live session and offers the terminal instead', async () => {
    mockApi({ '/v1/agents/sre': { ...AGENT, session_alive: true } })
    renderWithProviders(<AgentScreen />)
    await waitFor(() => expect(screen.getByText('Open terminal')).toBeTruthy())
    expect(screen.queryByText('Start')).toBeNull()
  })

  it('has no dossier, prompt or memory tabs', async () => {
    mockApi()
    renderWithProviders(<AgentScreen />)
    await waitFor(() => expect(screen.getByText('Questions')).toBeTruthy())
    expect(screen.queryByText('Dossier')).toBeNull()
    expect(screen.queryByText('Prompt')).toBeNull()
    expect(screen.queryByText('Memory')).toBeNull()
  })
})
