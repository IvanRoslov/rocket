/**
 * Renders the two agent screens against the daemon's real JSON shapes
 * (captured from `/v1/agents*` on a live rocketd), so a contract drift or a
 * crashing branch fails here rather than in the app.
 */
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native'
import * as Clipboard from 'expo-clipboard'
import { router } from 'expo-router'
import { SafeAreaProvider } from 'react-native-safe-area-context'
import { ToastProvider } from '../../src/components/Toast'
import { ServerProvider } from '../../src/servers/ServerContext'
import AgentsScreen from '../../app/(tabs)/agents'
import AgentScreen from '../../app/agent/[id]'

// react-query's polling re-renders outside act(); mark the environment so
// React reports them as handled instead of warning on every tick.
;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

jest.mock('expo-router', () => ({
  router: { navigate: jest.fn(), push: jest.fn(), back: jest.fn() },
  useLocalSearchParams: () => ({ id: 'sre' }),
}))

jest.mock('expo-clipboard', () => ({ setStringAsync: jest.fn() }))

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

// Registered with no project: the project-filtered list could never show it,
// which is what the global list exists for.
const ORPHAN = {
  ...AGENT,
  id: 'librarian',
  description: 'keeps the docs honest',
  project: '',
  unread: 0,
  open_questions: 0,
  awaiting_user: 0,
}

const QUESTIONS = {
  questions: [
    {
      id: 1,
      role_id: 'sre',
      ordinal: 1,
      asked_by: 'sre',
      body: 'status?',
      context: 'from mobile',
      status: 'open',
      participants: ['human', 'sre', 'cto'],
      waiting_on: ['human'],
      your_turn: true,
      whose_turn: 'user',
      asked_at: 1785622879,
      messages: [
        // The legacy empty form and the canonical one must render identically.
        { id: 11, author: '', kind: 'reply', body: 'looking', addressed_to: [], created_at: 1785622880 },
        { id: 12, author: 'human', kind: 'reply', body: 'still looking', addressed_to: ['cto'], created_at: 1785622881 },
        { id: 13, author: 'cto', kind: 'reply', body: 'over to you', addressed_to: ['human'], created_at: 1785622882 },
      ],
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
    '/v1/agents': [AGENT, ORPHAN],
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

  it('shows an empty state when nothing is registered', async () => {
    mockApi({ '/v1/agents': [] })
    renderWithProviders(<AgentsScreen />)
    await waitFor(() => expect(screen.getByText(/No agents yet/)).toBeTruthy())
  })

  it('lists project-less agents too, under «No project»', async () => {
    mockApi()
    renderWithProviders(<AgentsScreen />)
    await waitFor(() => expect(screen.getByText('librarian')).toBeTruthy())
    // The chip for the project-less bucket; the card's own pill reads lowercase.
    expect(screen.getByText('No project')).toBeTruthy()
    expect(screen.getByText('no project')).toBeTruthy()
  })

  it('filters by project, «No project» included', async () => {
    mockApi()
    renderWithProviders(<AgentsScreen />)
    await waitFor(() => expect(screen.getByText('librarian')).toBeTruthy())

    fireEvent.press(screen.getByText('No project'))
    await waitFor(() => expect(screen.queryByText('sre')).toBeNull())
    expect(screen.getByText('librarian')).toBeTruthy()

    fireEvent.press(screen.getByText('All'))
    await waitFor(() => expect(screen.getByText('sre')).toBeTruthy())
  })

  it('opens a chat with the agent from the card, live session or not', async () => {
    mockApi()
    renderWithProviders(<AgentsScreen />)
    await waitFor(() => expect(screen.getByText('librarian')).toBeTruthy())

    // librarian's session is down; chat must still open — what you send there
    // waits in its inbox.
    fireEvent.press(screen.getAllByText('💬 Chat')[1])
    await waitFor(() =>
      expect(router.navigate).toHaveBeenCalledWith('/chat/librarian?agent=1'),
    )
  })

  it('copies the attach command for the agent', async () => {
    mockApi()
    renderWithProviders(<AgentsScreen />)
    await waitFor(() => expect(screen.getByText('librarian')).toBeTruthy())

    fireEvent.press(screen.getAllByText('⧉ Attach')[1])
    await waitFor(() =>
      expect(Clipboard.setStringAsync).toHaveBeenCalledWith('rocket agent attach librarian'),
    )
  })
})

describe('AgentScreen', () => {
  afterEach(() => jest.restoreAllMocks())

  it('opens on the questions tab and renders the thread', async () => {
    mockApi()
    renderWithProviders(<AgentScreen />)
    await waitFor(() => expect(screen.getByText('status?')).toBeTruthy())
    // The agent opened this thread and waiting_on is us, so we owe the answer.
    expect(screen.getByText('awaiting you')).toBeTruthy()
    expect(screen.getByText('Q1')).toBeTruthy()
  })

  it('lists the thread participants', async () => {
    mockApi()
    renderWithProviders(<AgentScreen />)
    await waitFor(() => expect(screen.getByText('status?')).toBeTruthy())
    expect(screen.getByText('you, sre, cto')).toBeTruthy()
  })

  it('does not flag our turn when your_turn is false', async () => {
    // whose_turn stays "user" so a regression back onto the compat field fails here.
    const q = QUESTIONS.questions[0]
    mockApi({
      '/v1/agents/sre/questions': {
        questions: [{ ...q, your_turn: false, waiting_on: ['cto'], whose_turn: 'user' }],
      },
    })
    renderWithProviders(<AgentScreen />)
    await waitFor(() => expect(screen.getByText('status?')).toBeTruthy())
    expect(screen.queryByText('awaiting you')).toBeNull()
    expect(screen.getByText('waiting for cto')).toBeTruthy()
  })

  it('renders a human message as us for both the empty and canonical author', async () => {
    mockApi()
    renderWithProviders(<AgentScreen />)
    await waitFor(() => expect(screen.getByText('looking')).toBeTruthy())
    // Two human-authored messages: author "" and author "human".
    expect(screen.getAllByText('you').length).toBe(2)
  })

  it('shows who a message was addressed to', async () => {
    mockApi()
    renderWithProviders(<AgentScreen />)
    await waitFor(() => expect(screen.getByText('still looking')).toBeTruthy())
    expect(screen.getByText('→ cto')).toBeTruthy()
    expect(screen.getByText('→ you')).toBeTruthy()
  })

  it('sends the picked addressee with the reply', async () => {
    mockApi()
    renderWithProviders(<AgentScreen />)
    await waitFor(() => expect(screen.getByText('status?')).toBeTruthy())

    fireEvent.changeText(screen.getByPlaceholderText(/Write a reply/), 'ack')
    // Each event's state must land before the next one reads it.
    await waitFor(() => expect(screen.getByPlaceholderText(/Write a reply/).props.value).toBe('ack'))
    fireEvent.press(screen.getByTestId('to-cto'))
    await waitFor(() => expect(screen.getByTestId('to-cto').props.accessibilityState?.selected).toBe(true))
    fireEvent.press(screen.getByText('Clarify'))

    await waitFor(() => {
      const call = (fetch as jest.Mock).mock.calls.find(([u]: [string]) =>
        String(u).includes('/v1/agent-questions/1/reply'),
      )
      expect(call).toBeTruthy()
      expect(JSON.parse(call[1].body)).toEqual({ body: 'ack', to: ['cto'] })
    })
  })

  it('omits the to key when no addressee is picked', async () => {
    mockApi()
    renderWithProviders(<AgentScreen />)
    await waitFor(() => expect(screen.getByText('status?')).toBeTruthy())

    fireEvent.changeText(screen.getByPlaceholderText(/Write a reply/), 'ack')
    await waitFor(() => expect(screen.getByPlaceholderText(/Write a reply/).props.value).toBe('ack'))
    fireEvent.press(screen.getByText('Clarify'))

    await waitFor(() => {
      const call = (fetch as jest.Mock).mock.calls.find(([u]: [string]) =>
        String(u).includes('/v1/agent-questions/1/reply'),
      )
      expect(call).toBeTruthy()
      expect(JSON.parse(call[1].body)).toEqual({ body: 'ack' })
    })
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
