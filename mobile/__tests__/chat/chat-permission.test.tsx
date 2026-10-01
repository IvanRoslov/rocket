/**
 * Permission prompts on the chat screen (task #4881): any agent may need a
 * permission, so the card shows even on read-only worker chats, and resolved
 * prompts the daemon logs come back as role="permission" entries.
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
  useLocalSearchParams: () => ({ id: 'w1' }),
}))

function mockChat(chat: unknown) {
  const bodies: Record<string, unknown> = {
    '/v1/messages?session=w1&limit=50': { messages: [] },
    '/v1/sessions/w1/chat?limit=300': chat,
  }
  globalThis.fetch = jest.fn(async (url: string, init?: RequestInit) => {
    const path = String(url).replace(/^https?:\/\/[^/]+/, '')
    if (init?.method === 'POST') return { ok: true, status: 202, json: async () => ({ status: 'accepted' }) }
    if (path.startsWith('/v1/sessions/w1/chat?cursor=')) return { ok: true, status: 200, json: async () => bodies['/v1/sessions/w1/chat?limit=300'] }
    if (!(path in bodies)) {
      return { ok: false, status: 404, json: async () => ({ error: { code: 'not_found' } }) }
    }
    return { ok: true, status: 200, json: async () => bodies[path] }
  }) as unknown as typeof fetch
  return bodies
}

const permissionQuiz = (askedAt: number, title: string) => ({
  source: 'permission',
  asked_at: askedAt,
  raw: '',
  questions: [{ header: 'Разрешение', question: title, multi_select: false, options: [{ label: 'Yes' }, { label: 'No' }] }],
})

function renderChat() {
  return render(
    <SafeAreaProvider
      initialMetrics={{ frame: { x: 0, y: 0, width: 390, height: 844 }, insets: { top: 47, left: 0, right: 0, bottom: 34 } }}
    >
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <ServerProvider>
          <ToastProvider>
            <ChatScreen />
          </ToastProvider>
        </ServerProvider>
      </QueryClientProvider>
    </SafeAreaProvider>,
  )
}

const worker = (extra: object = {}) => ({ id: 'w1', kind: 'worker', state: 'running', activity: 'waiting_input', ...extra })

describe('ChatScreen with permission prompts', () => {
  afterEach(() => jest.restoreAllMocks())

  it('shows a pending permission on a read-only worker chat instead of the read-only bar', async () => {
    mockChat({
      session: worker({
        pending_quiz: {
          source: 'permission',
          asked_at: 1785622879,
          raw: '',
          questions: [
            {
              header: 'Разрешение',
              question: 'Do you want to run this command?\n\nrm -rf build/',
              multi_select: false,
              options: [{ label: 'Yes' }, { label: 'No, and tell Claude what to do differently (esc)' }],
            },
          ],
        },
      }),
      entries: [{ ts: 1785622870, role: 'assistant', text: 'cleaning up' }],
      next_cursor: '',
    })
    await renderChat()

    await waitFor(() => expect(screen.getByText('Do you want to run this command?')).toBeTruthy())
    expect(screen.getByRole('button', { name: 'Yes' })).toBeTruthy()
    expect(screen.queryByText(/read-only/)).toBeNull()
    expect(screen.queryByText('Send')).toBeNull()
  })

  it('unlocks the card when the agent moves on to a different dialog', async () => {
    const chat = { session: worker({ pending_quiz: permissionQuiz(100, 'First?') }), entries: [], next_cursor: 'c1' }
    const bodies = mockChat(chat)
    await renderChat()

    await waitFor(() => expect(screen.getByText('First?')).toBeTruthy())
    fireEvent.press(screen.getByRole('button', { name: 'Yes' }))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Yes' }).props.accessibilityState?.disabled).toBe(true))

    bodies['/v1/sessions/w1/chat?limit=300'] = { ...chat, session: worker({ pending_quiz: permissionQuiz(200, 'Second?') }) }
    await waitFor(() => expect(screen.getByText('Second?')).toBeTruthy(), { timeout: 6000 })
    expect(screen.getByRole('button', { name: 'Yes' }).props.accessibilityState?.disabled).toBe(false)
  }, 10000)

  it('renders resolved permission entries as compact cards', async () => {
    mockChat({
      session: worker(),
      entries: [
        { ts: 1785622870, role: 'assistant', text: 'editing settings' },
        {
          ts: 1785622875,
          role: 'permission',
          text: 'Do you want to make this edit to settings.json?',
          permission: {
            title: 'Do you want to make this edit to settings.json?',
            context: '.claude/settings.json',
            answer_label: 'Yes',
            answered_via: 'chat',
          },
        },
        {
          ts: 1785622880,
          role: 'permission',
          text: 'Do you want to proceed?',
          permission: { title: 'Do you want to proceed?', answered_via: 'terminal' },
        },
      ],
      next_cursor: '',
    })
    await renderChat()

    await waitFor(() =>
      expect(screen.getByText('Разрешение: Do you want to make this edit to settings.json? → Yes')).toBeTruthy(),
    )
    expect(screen.getByText('Разрешение: Do you want to proceed? → отвечено в терминале')).toBeTruthy()
  })

  it('keeps the read-only bar for a worker with nothing pending', async () => {
    mockChat({ session: worker(), entries: [{ ts: 1785622870, role: 'assistant', text: 'working' }], next_cursor: '' })
    await renderChat()

    await waitFor(() => expect(screen.getByText(/Workers are read-only/)).toBeTruthy())
    expect(screen.queryByText('Разрешение')).toBeNull()
  })
})
