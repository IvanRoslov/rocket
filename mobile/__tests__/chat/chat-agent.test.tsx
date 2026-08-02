/**
 * Chat with a standing agent (`/chat/<agent>?agent=1`): the same screen the
 * app uses for orchestrator sessions, but the composer stays open whether or
 * not the agent's tmux session is up — `POST /v1/messages` delivers to the
 * live session or drops the message into the agent's inbox
 * (internal/api/agent_delivery.go).
 */
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native'
import { SafeAreaProvider } from 'react-native-safe-area-context'
import { ToastProvider } from '../../src/components/Toast'
import { ServerProvider } from '../../src/servers/ServerContext'
import ChatScreen from '../../app/chat/[id]'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

jest.mock('expo-router', () => ({
  router: { navigate: jest.fn(), push: jest.fn(), back: jest.fn() },
  useLocalSearchParams: () => ({ id: 'librarian', agent: '1' }),
}))

/** The daemon has no session row for an agent that was never started, so its
 *  chat feed 404s — the screen must still let you write. */
function mockApi(chat?: unknown) {
  const bodies: Record<string, unknown> = {
    '/v1/messages?session=librarian&limit=50': { messages: [] },
    ...(chat ? { '/v1/sessions/librarian/chat?limit=300': chat } : {}),
  }
  const posted: Array<{ path: string; body: unknown }> = []
  globalThis.fetch = jest.fn(async (url: string, init?: RequestInit) => {
    const path = String(url).replace(/^https?:\/\/[^/]+/, '')
    if (init?.method === 'POST') {
      posted.push({ path, body: JSON.parse(String(init.body)) })
      return { ok: true, status: 202, json: async () => ({ id: 1, to: 'librarian' }) }
    }
    if (!(path in bodies)) {
      return { ok: false, status: 404, json: async () => ({ error: { code: 'not_found' } }) }
    }
    return { ok: true, status: 200, json: async () => bodies[path] }
  }) as unknown as typeof fetch
  return posted
}

function renderChat() {
  return render(
    <SafeAreaProvider
      initialMetrics={{
        frame: { x: 0, y: 0, width: 390, height: 844 },
        insets: { top: 47, left: 0, right: 0, bottom: 34 },
      }}
    >
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <ServerProvider>
          <ToastProvider>
            <ChatScreen />
          </ToastProvider>
        </ServerProvider>
      </QueryClientProvider>
    </SafeAreaProvider>,
  )
}

describe('ChatScreen in agent mode', () => {
  afterEach(() => jest.restoreAllMocks())

  it('keeps the composer open with the session down and says where the message goes', async () => {
    mockApi()
    renderChat()

    await waitFor(() => expect(screen.getByText('Send')).toBeTruthy())
    expect(screen.getByPlaceholderText(/waits in its inbox/)).toBeTruthy()
    expect(screen.queryByText(/read-only/)).toBeNull()
  })

  it('sends through the agent delivery path', async () => {
    const posted = mockApi()
    renderChat()

    await waitFor(() => expect(screen.getByText('Send')).toBeTruthy())
    const input = screen.getByPlaceholderText(/waits in its inbox/)
    fireEvent.changeText(input, 'docs are stale')
    // Send stays disabled until the typed text lands in state.
    await waitFor(() => expect(input.props.value).toBe('docs are stale'))
    fireEvent.press(screen.getByText('Send'))

    await waitFor(() => expect(posted).toHaveLength(1))
    expect(posted[0]).toMatchObject({
      path: '/v1/messages',
      body: { to: 'librarian', body: 'docs are stale' },
    })
  })

  it('says the message goes straight in once the session is live', async () => {
    mockApi({
      session: { id: 'librarian', kind: 'agent', state: 'running' },
      entries: [{ ts: 1785622879, role: 'assistant', text: 'on it' }],
    })
    renderChat()

    await waitFor(() => expect(screen.getByText('on it')).toBeTruthy())
    expect(screen.getByPlaceholderText(/straight into its session/)).toBeTruthy()
  })
})
