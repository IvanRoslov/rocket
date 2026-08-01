/**
 * Renders the two role screens against the daemon's real JSON shapes
 * (captured from `/v1/agents*` on a live rocketd), so a contract drift or a
 * crashing branch fails here rather than in the app.
 */
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react-native'
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
  project: 'platform',
  prompt_path: '/home/agents/sre/role.md',
  prompt: '# SRE\n\nTriage policy: take small things.\n',
  subscriptions: [{ repo: 'o/r', labels: ['bug'], mention_only: true }],
  cron: '0 * * * *',
  agent: 'claude-code',
  enabled: true,
  inbox_queued: 2,
  items: 1,
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

/** Routes a request path to a canned body; unknown paths 404 like the daemon. */
function mockApi(overrides: Record<string, unknown> = {}) {
  const bodies: Record<string, unknown> = {
    '/v1/agents': [AGENT],
    '/v1/agents?project=platform': [AGENT],
    '/v1/agents/sre': AGENT,
    '/v1/agents/sre/questions': QUESTIONS,
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

  it('lists roles with their badges', async () => {
    mockApi()
    renderWithProviders(<AgentsScreen />)
    await waitFor(() => expect(screen.getByText('sre')).toBeTruthy())
    expect(screen.getByText('? 1 awaiting you')).toBeTruthy()
    expect(screen.getByText('2 queued')).toBeTruthy()
    expect(screen.getByText('1 open Q')).toBeTruthy()
  })

  it('shows an empty state when the project has no roles', async () => {
    mockApi({ '/v1/agents': [] })
    renderWithProviders(<AgentsScreen />)
    await waitFor(() => expect(screen.getByText(/No roles yet/)).toBeTruthy())
  })
})

describe('AgentScreen', () => {
  afterEach(() => jest.restoreAllMocks())

  it('opens on the questions tab and renders the thread', async () => {
    mockApi()
    renderWithProviders(<AgentScreen />)
    await waitFor(() => expect(screen.getByText('status?')).toBeTruthy())
    // The user opened this thread, so the role owes the answer.
    expect(screen.getByText('waiting for sre')).toBeTruthy()
    expect(screen.getByText('Q1')).toBeTruthy()
  })

  it('hides the memory tab when the daemon has no memory endpoint', async () => {
    mockApi() // /v1/agents/sre/memory is absent → 404
    renderWithProviders(<AgentScreen />)
    await waitFor(() => expect(screen.getByText('Prompt')).toBeTruthy())
    await waitFor(() => expect(screen.queryByText('Memory')).toBeNull())
  })

  it('shows the memory tab when the endpoint answers', async () => {
    mockApi({
      '/v1/agents/sre/memory': {
        path: '/home/agents/sre/memory',
        index: '- [db](db.md) — postgres 16\n',
        files: [{ name: 'db.md', size: 12, updated_at: 1785622879, body: 'postgres 16\n' }],
      },
    })
    renderWithProviders(<AgentScreen />)
    await waitFor(() => expect(screen.getByText('Memory')).toBeTruthy())
  })
})
