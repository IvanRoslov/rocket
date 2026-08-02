/**
 * Renders the task screen's Q&A tab against the daemon's participant-shaped
 * JSON, so a contract drift or a crashing branch fails here, not in the app.
 */
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native'
import { SafeAreaProvider } from 'react-native-safe-area-context'
import { ToastProvider } from '../../src/components/Toast'
import { ServerProvider } from '../../src/servers/ServerContext'
import TaskScreen from '../../app/task/[id]'

// react-query's polling re-renders outside act(); mark the environment so
// React reports them as handled instead of warning on every tick.
;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

jest.mock('expo-router', () => ({
  router: { navigate: jest.fn(), push: jest.fn(), back: jest.fn() },
  useLocalSearchParams: () => ({ id: '12' }),
}))

jest.mock('expo-clipboard', () => ({ setStringAsync: jest.fn() }))

const TASK = {
  id: 12,
  title: 'ship the thing',
  project_id: 'platform',
  status: 'in_progress',
  created_by: 'user',
  created_at: 1785622872,
  updated_at: 1785622879,
  subtasks: [],
  open_questions: 1,
}

const THREAD = {
  id: 1,
  task_id: 12,
  ordinal: 1,
  asked_by: 'reply-answer-orch',
  body: 'which repo?',
  status: 'open',
  participants: ['human', 'reply-answer-orch', 'cto'],
  waiting_on: ['human'],
  your_turn: true,
  whose_turn: 'user',
  asked_at: 1785622879,
  messages: [
    // The legacy empty form and the canonical one must render identically.
    { id: 11, author: '', kind: 'reply', body: 'checking', addressed_to: [], created_at: 1785622880 },
    { id: 12, author: 'human', kind: 'reply', body: 'still checking', addressed_to: ['cto'], created_at: 1785622881 },
    { id: 13, author: 'cto', kind: 'reply', body: 'your call', addressed_to: ['human'], created_at: 1785622882 },
  ],
}

/** Routes a request path to a canned body; unknown paths 404 like the daemon. */
function mockApi(overrides: Record<string, unknown> = {}) {
  const bodies: Record<string, unknown> = {
    '/v1/tasks/12': TASK,
    '/v1/tasks/12/questions': { questions: [THREAD] },
    '/v1/sessions': [],
    '/v1/sessions?project=platform': [],
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

/** Opens the Questions tab, which is not the screen's default. */
async function openQuestions() {
  await waitFor(() => expect(screen.getByText('Questions')).toBeTruthy())
  fireEvent.press(screen.getByText('Questions'))
  await waitFor(() => expect(screen.getByText('which repo?')).toBeTruthy())
}

describe('task Q&A thread', () => {
  afterEach(() => jest.restoreAllMocks())

  it('lists the thread participants', async () => {
    mockApi()
    renderWithProviders(<TaskScreen />)
    await openQuestions()
    expect(screen.getByText('you, reply-answer-orch, cto')).toBeTruthy()
  })

  it('flags our turn from your_turn', async () => {
    mockApi()
    renderWithProviders(<TaskScreen />)
    await openQuestions()
    expect(screen.getByText('awaiting you')).toBeTruthy()
  })

  it('does not flag our turn when your_turn is false', async () => {
    // whose_turn stays "user" so a regression back onto the compat field fails here.
    mockApi({
      '/v1/tasks/12/questions': {
        questions: [{ ...THREAD, your_turn: false, waiting_on: ['cto'], whose_turn: 'user' }],
      },
    })
    renderWithProviders(<TaskScreen />)
    await openQuestions()
    expect(screen.queryByText('awaiting you')).toBeNull()
    expect(screen.getByText('waiting for cto')).toBeTruthy()
  })

  it('renders a human message as us for both the empty and canonical author', async () => {
    mockApi()
    renderWithProviders(<TaskScreen />)
    await openQuestions()
    expect(screen.getAllByText('you').length).toBe(2)
  })

  it('shows who a message was addressed to', async () => {
    mockApi()
    renderWithProviders(<TaskScreen />)
    await openQuestions()
    expect(screen.getByText('→ cto')).toBeTruthy()
    expect(screen.getByText('→ you')).toBeTruthy()
  })

  it('sends the picked addressee with the reply', async () => {
    mockApi()
    renderWithProviders(<TaskScreen />)
    await openQuestions()

    fireEvent.changeText(screen.getByPlaceholderText(/Write a reply/), 'the monorepo')
    // Each event's state must land before the next one reads it.
    await waitFor(() =>
      expect(screen.getByPlaceholderText(/Write a reply/).props.value).toBe('the monorepo'),
    )
    fireEvent.press(screen.getByTestId('to-cto'))
    await waitFor(() => expect(screen.getByTestId('to-cto').props.accessibilityState?.selected).toBe(true))
    fireEvent.press(screen.getByText('Clarify'))

    await waitFor(() => {
      const call = (fetch as jest.Mock).mock.calls.find(([u]: [string]) =>
        String(u).includes('/v1/questions/1/reply'),
      )
      expect(call).toBeTruthy()
      expect(JSON.parse(call[1].body)).toEqual({ body: 'the monorepo', to: ['cto'] })
    })
  })

  it('omits the to key when no addressee is picked', async () => {
    mockApi()
    renderWithProviders(<TaskScreen />)
    await openQuestions()

    fireEvent.changeText(screen.getByPlaceholderText(/Write a reply/), 'the monorepo')
    await waitFor(() =>
      expect(screen.getByPlaceholderText(/Write a reply/).props.value).toBe('the monorepo'),
    )
    fireEvent.press(screen.getByText('Clarify'))

    await waitFor(() => {
      const call = (fetch as jest.Mock).mock.calls.find(([u]: [string]) =>
        String(u).includes('/v1/questions/1/reply'),
      )
      expect(call).toBeTruthy()
      expect(JSON.parse(call[1].body)).toEqual({ body: 'the monorepo' })
    })
  })

  it('keeps the final answer available to us', async () => {
    mockApi()
    renderWithProviders(<TaskScreen />)
    await openQuestions()
    expect(screen.getByText('Answer & close')).toBeTruthy()
  })
})
