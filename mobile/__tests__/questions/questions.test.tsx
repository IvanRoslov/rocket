/**
 * Renders the Questions tab against the daemon's inbox JSON: what is listed,
 * and that an answer waits out the 5 s Undo window before it is POSTed.
 */
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react-native'
import { AppState, ScrollView, type AppStateStatus } from 'react-native'
import { SafeAreaProvider } from 'react-native-safe-area-context'
import { ToastProvider } from '../../src/components/Toast'
import { ServerProvider } from '../../src/servers/ServerContext'
import QuestionsScreen from '../../app/(tabs)/questions'


jest.mock('expo-router', () => ({
  router: { navigate: jest.fn(), push: jest.fn(), back: jest.fn() },
}))

const MINE = {
  local_ref: '1023/Q2', kind: 'task', task_id: 1023, task_title: 'Ship it', subject: 'task #1023', id: 7, ordinal: 2,
  asked_by: 'orch', title: 'Which DB?', body: 'Pick **one**', status: 'open', type: 'decision', options: ['Postgres', 'SQLite'],
  participants: ['human', 'orch'], attention: ['human'], waiting_on: ['human'], your_turn: true, asked_at: 1, updated_at: 1,
}
const OTHER = {
  local_ref: 'cto/Q1', kind: 'role', role_id: 'cto', subject: 'role cto', id: 9, ordinal: 1, asked_by: 'cto',
  title: 'Other', body: 'x', status: 'open', type: 'decision', participants: ['cto', 'w1'], attention: ['w1'],
  waiting_on: ['w1'], your_turn: false, asked_at: 1, updated_at: 2,
}

let posts: { path: string; body: unknown }[] = []

/** Serves the inbox and records POSTs; `postStatus` lets a test make them fail. */
function mockApi(
  opts: { threads?: unknown[]; postStatus?: number; postBody?: unknown; failPath?: string } = {},
) {
  posts = []
  const threads = opts.threads ?? [MINE, OTHER]
  globalThis.fetch = jest.fn(async (url: string, init?: RequestInit) => {
    const path = String(url).replace(/^https?:\/\/[^/]+/, '')
    if (init?.method === 'POST') {
      posts.push({ path, body: JSON.parse(String(init.body)) })
      const status = opts.failPath && opts.failPath !== path ? 200 : (opts.postStatus ?? 200)
      return { ok: status < 300, status, json: async () => opts.postBody ?? {} }
    }
    if (path === '/v1/threads') return { ok: true, status: 200, json: async () => ({ threads }) }
    return { ok: false, status: 404, json: async () => ({ error: { code: 'not_found' } }) }
  }) as unknown as typeof fetch
}

async function renderScreen() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } })
  const metrics = {
    frame: { x: 0, y: 0, width: 390, height: 844 },
    insets: { top: 47, left: 0, right: 0, bottom: 34 },
  }
  return await render(
    <SafeAreaProvider initialMetrics={metrics}>
      <QueryClientProvider client={qc}>
        <ServerProvider>
          <ToastProvider>
            <QuestionsScreen />
          </ToastProvider>
        </ServerProvider>
      </QueryClientProvider>
    </SafeAreaProvider>,
  )
}

const elapse = (ms: number) => act(async () => { jest.advanceTimersByTime(ms) })

describe('Questions tab', () => {
  beforeEach(() => jest.useFakeTimers())
  afterEach(() => {
    jest.useRealTimers()
    jest.restoreAllMocks()
  })

  it('lists what waits on you and hides the rest until expanded', async () => {
    mockApi()
    await renderScreen()
    await waitFor(() => expect(screen.getByText('Which DB?')).toBeTruthy())
    expect(screen.getByText('#1023 · Ship it')).toBeTruthy()
    expect(screen.getByText('Postgres')).toBeTruthy()
    expect(screen.getByText('SQLite')).toBeTruthy()
    expect(screen.queryByText('Other')).toBeNull()
    await fireEvent.press(screen.getByText(/Other open \(1\)/))
    expect(screen.getByText('Other')).toBeTruthy()
  })

  describe('keyboard', () => {
    const layout = (y: number, height: number) => ({ nativeEvent: { layout: { x: 0, y, width: 390, height } } })

    it('lifts the list and the undo bar above the keyboard and keeps taps working', async () => {
      mockApi()
      await renderScreen()
      await waitFor(() => expect(screen.getByText('Which DB?')).toBeTruthy())
      // behavior="padding": KAV renders its host with a bottom padding (0 while no keyboard).
      const kav = screen.getByTestId('questions-keyboard')
      expect(kav).toHaveStyle({ paddingBottom: 0 })
      const list = screen.getByTestId('questions-list')
      expect(within(kav).getByTestId('questions-list')).toBe(list)
      expect(list.props.keyboardShouldPersistTaps).toBe('handled')
      await fireEvent.press(screen.getByText('Postgres'))
      expect(within(kav).getByText('Undo')).toBeTruthy()
    })

    it('scrolls a focused answer input and its Send button into view', async () => {
      mockApi()
      await renderScreen()
      await waitFor(() => expect(screen.getByPlaceholderText('Your answer…')).toBeTruthy())
      const scrollTo = ScrollView.prototype.scrollTo as jest.Mock
      scrollTo.mockClear()
      await fireEvent(screen.getByTestId('thread-7'), 'layout', layout(500, 250))
      // The keyboard opens: KAV shrinks the list to 300 px, the card is below it.
      await fireEvent(screen.getByTestId('questions-list'), 'layout', layout(0, 300))
      expect(scrollTo).not.toHaveBeenCalled()
      await fireEvent(screen.getByPlaceholderText('Your answer…'), 'focus')
      // 500 + 250 + 12 margin - 300 visible
      expect(scrollTo).toHaveBeenLastCalledWith({ y: 462, animated: true })
      // Re-layout after the keyboard animation settles reveals it again.
      scrollTo.mockClear()
      await fireEvent(screen.getByTestId('questions-list'), 'layout', layout(0, 280))
      expect(scrollTo).toHaveBeenLastCalledWith({ y: 482, animated: true })
      // Once the input is left, layout changes no longer move the list.
      await fireEvent(screen.getByPlaceholderText('Your answer…'), 'blur')
      scrollTo.mockClear()
      await fireEvent(screen.getByTestId('questions-list'), 'layout', layout(0, 200))
      expect(scrollTo).not.toHaveBeenCalled()
    })
  })

  describe('brief', () => {
    const BRIEF = '**Проблема:** база тормозит.\n\n**Рекомендация:** берите Postgres.'
    const withBrief = { ...MINE, brief: BRIEF, body: 'A long wall of **detail** about indexes' }

    /** Text of every rendered node in tree order — to check what comes first. */
    const order = (...texts: string[]) => {
      const out: string[] = []
      const walk = (n: unknown): void => {
        if (typeof n === 'string') out.push(n)
        else if (Array.isArray(n)) n.forEach(walk)
        else if (n && typeof n === 'object') walk((n as { children?: unknown }).children ?? [])
      }
      walk(screen.toJSON())
      const flat = out.join('\n')
      return texts.map((t) => flat.indexOf(t))
    }

    it('shows the brief first, then the options, with the body collapsed', async () => {
      mockApi({ threads: [withBrief] })
      await renderScreen()
      await waitFor(() => expect(screen.getByText('Which DB?')).toBeTruthy())
      expect(screen.getByText(/база тормозит/)).toBeTruthy()
      expect(screen.getByText(/берите Postgres/)).toBeTruthy()
      // Rendered as markdown: the bold label is its own node, no asterisks.
      expect(screen.getByText('Проблема:')).toBeTruthy()
      expect(screen.queryByText(/\*\*/)).toBeNull()
      expect(screen.getByText(/Подробности/)).toBeTruthy()
      expect(screen.queryByText(/A long wall of/)).toBeNull()
      const [title, brief, option] = order('Which DB?', 'база тормозит', 'SQLite')
      expect(title).toBeGreaterThanOrEqual(0)
      expect(title).toBeLessThan(brief)
      expect(brief).toBeLessThan(option)
    })

    it('the Подробности toggle reveals the full body and hides it again', async () => {
      mockApi({ threads: [withBrief] })
      await renderScreen()
      await waitFor(() => expect(screen.getByText(/Подробности/)).toBeTruthy())
      await fireEvent.press(screen.getByText(/Подробности/))
      expect(screen.getByText(/A long wall of/)).toBeTruthy()
      expect(screen.getByText('detail')).toBeTruthy()
      await fireEvent.press(screen.getByText(/Подробности/))
      expect(screen.queryByText(/A long wall of/)).toBeNull()
    })

    it('renders exactly as before when the brief is empty or missing', async () => {
      for (const thread of [{ ...MINE, brief: '' }, MINE]) {
        mockApi({ threads: [thread] })
        const view = await renderScreen()
        await waitFor(() => expect(screen.getByText('Which DB?')).toBeTruthy())
        // The body is shown in full, straight away, with no toggle.
        expect(screen.getByText('one')).toBeTruthy()
        expect(screen.queryByText(/Подробности/)).toBeNull()
        await view.unmount()
      }
    })
  })

  it('sends a tapped option only after the undo window', async () => {
    mockApi()
    await renderScreen()
    await waitFor(() => expect(screen.getByText('Postgres')).toBeTruthy())
    await fireEvent.press(screen.getByText('Postgres'))
    expect(screen.getByText('Answered: Postgres')).toBeTruthy()
    expect(screen.getByText('Undo')).toBeTruthy()
    expect(screen.queryByText('Which DB?')).toBeNull()
    expect(posts).toEqual([])
    await elapse(5000)
    await waitFor(() => expect(posts).toEqual([{ path: '/v1/questions/7/answer', body: { choose: 1 } }]))
  })

  describe('brainstorm thread', () => {
    const STORM = { ...MINE, type: 'brainstorm', recommended_option: 2, chosen_option: null, outcome: '' }

    it('stars the recommended option', async () => {
      mockApi({ threads: [STORM] })
      await renderScreen()
      await waitFor(() => expect(screen.getByText('SQLite')).toBeTruthy())
      expect(within(screen.getByTestId('option-2')).getByText('★ Recommended')).toBeTruthy()
      expect(within(screen.getByTestId('option-1')).queryByText('★ Recommended')).toBeNull()
    })

    it('sends the option with its comment after the undo window', async () => {
      mockApi({ threads: [STORM] })
      await renderScreen()
      await waitFor(() => expect(screen.getByPlaceholderText('Comment or your own answer…')).toBeTruthy())
      await fireEvent.changeText(screen.getByPlaceholderText('Comment or your own answer…'), 'WAL mode')
      await fireEvent.press(screen.getByText('SQLite'))
      expect(screen.getByText('Answered: SQLite — WAL mode')).toBeTruthy()
      await elapse(5000)
      await waitFor(() =>
        expect(posts).toEqual([{ path: '/v1/questions/7/answer', body: { choose: 2, body: 'WAL mode' } }]),
      )
    })

    it('has a single text box on the card', async () => {
      mockApi({ threads: [STORM] })
      await renderScreen()
      await waitFor(() => expect(screen.getByText('SQLite')).toBeTruthy())
      expect(screen.queryByPlaceholderText('Your answer…')).toBeNull()
    })

    it('sends a bare choose without a comment', async () => {
      mockApi({ threads: [STORM] })
      await renderScreen()
      await waitFor(() => expect(screen.getByText('Postgres')).toBeTruthy())
      await fireEvent.press(screen.getByText('Postgres'))
      await elapse(5000)
      await waitFor(() => expect(posts).toEqual([{ path: '/v1/questions/7/answer', body: { choose: 1 } }]))
    })
  })

  it('Undo cancels the answer and brings the card back', async () => {
    mockApi()
    await renderScreen()
    await waitFor(() => expect(screen.getByText('Postgres')).toBeTruthy())
    await fireEvent.press(screen.getByText('Postgres'))
    await fireEvent.press(screen.getByText('Undo'))
    await elapse(5000)
    expect(posts).toEqual([])
    expect(screen.getByText('Which DB?')).toBeTruthy()
    expect(screen.queryByText('Undo')).toBeNull()
  })

  it('Not relevant dismisses after the window', async () => {
    mockApi()
    await renderScreen()
    await waitFor(() => expect(screen.getByText('Not relevant')).toBeTruthy())
    await fireEvent.press(screen.getByText('Not relevant'))
    await elapse(5000)
    await waitFor(() => expect(posts).toEqual([{ path: '/v1/questions/7/answer', body: { dismiss: true } }]))
  })

  it('sends a free-text answer after the window', async () => {
    mockApi()
    await renderScreen()
    await waitFor(() => expect(screen.getByPlaceholderText('Your answer…')).toBeTruthy())
    await fireEvent.changeText(screen.getByPlaceholderText('Your answer…'), 'use sqlite')
    await fireEvent.press(screen.getByText('Send'))
    await elapse(5000)
    await waitFor(() => expect(posts).toEqual([{ path: '/v1/questions/7/answer', body: { body: 'use sqlite' } }]))
  })

  it('brings the card back and toasts when the daemon rejects the answer', async () => {
    mockApi({
      postStatus: 409,
      postBody: { error: { code: 'question_resolved', message: 'already resolved' } },
    })
    await renderScreen()
    await waitFor(() => expect(screen.getByText('Postgres')).toBeTruthy())
    await fireEvent.press(screen.getByText('Postgres'))
    await elapse(5000)
    await waitFor(() => expect(screen.getByText('already resolved')).toBeTruthy())
    expect(screen.getByText('Which DB?')).toBeTruthy()
  })

  it('reports a failed answer even when later answers were started before it settled', async () => {
    const t = (id: number, title: string, opt: string) => ({ ...MINE, id, title, options: [opt] })
    mockApi({
      threads: [t(7, 'Q-A', 'A1'), t(11, 'Q-B', 'B1'), t(12, 'Q-C', 'C1')],
      postStatus: 409,
      postBody: { error: { code: 'question_resolved', message: 'already resolved' } },
      failPath: '/v1/questions/7/answer',
    })
    await renderScreen()
    await waitFor(() => expect(screen.getByText('A1')).toBeTruthy())
    await fireEvent.press(screen.getByText('A1'))
    await fireEvent.press(screen.getByText('B1'))
    // Pressing B and C committed A; its 409 arrives while C is still pending.
    await fireEvent.press(screen.getByText('C1'))
    await waitFor(() => expect(screen.getByText('already resolved')).toBeTruthy())
    expect(screen.getByText('Q-A')).toBeTruthy()
  })

  it('keeps typed text when a failed answer brings the card back', async () => {
    mockApi({
      postStatus: 409,
      postBody: { error: { code: 'question_resolved', message: 'already resolved' } },
    })
    await renderScreen()
    await waitFor(() => expect(screen.getByPlaceholderText('Your answer…')).toBeTruthy())
    await fireEvent.changeText(screen.getByPlaceholderText('Your answer…'), 'use sqlite')
    await fireEvent.press(screen.getByText('Send'))
    await elapse(5000)
    await waitFor(() => expect(screen.getByText('already resolved')).toBeTruthy())
    expect(screen.getByDisplayValue('use sqlite')).toBeTruthy()
  })

  it('shows a thread again when the daemon reopens it after a successful answer', async () => {
    // The refetch after the answer still returns the same id, open and on you.
    mockApi()
    await renderScreen()
    await waitFor(() => expect(screen.getByText('Postgres')).toBeTruthy())
    await fireEvent.press(screen.getByText('Postgres'))
    expect(screen.queryByText('Which DB?')).toBeNull()
    await elapse(5000)
    await waitFor(() => expect(posts.length).toBe(1))
    await waitFor(() => expect(screen.getByText('Which DB?')).toBeTruthy())
  })

  it('commits a pending answer when the screen unmounts', async () => {
    mockApi()
    const view = await renderScreen()
    await waitFor(() => expect(screen.getByText('Postgres')).toBeTruthy())
    await fireEvent.press(screen.getByText('Postgres'))
    expect(posts).toEqual([])
    await view.unmount()
    await waitFor(() => expect(posts).toEqual([{ path: '/v1/questions/7/answer', body: { choose: 1 } }]))
  })

  it('answers an agent thread through the agent endpoint', async () => {
    mockApi({ threads: [{ ...OTHER, your_turn: true, waiting_on: ['human'], options: ['Yes'] }] })
    await renderScreen()
    await waitFor(() => expect(screen.getByText('Yes')).toBeTruthy())
    await fireEvent.press(screen.getByText('Yes'))
    await elapse(5000)
    await waitFor(() => expect(posts).toEqual([{ path: '/v1/agent-questions/9/answer', body: { choose: 1 } }]))
  })

  it('keeps the undo window on iOS inactive and commits on background', async () => {
    let onChange: ((s: AppStateStatus) => void) | undefined
    jest.spyOn(AppState, 'addEventListener').mockImplementation((_type, handler) => {
      onChange = handler as (s: AppStateStatus) => void
      return { remove: jest.fn() } as unknown as ReturnType<typeof AppState.addEventListener>
    })
    mockApi()
    await renderScreen()
    await waitFor(() => expect(screen.getByText('Postgres')).toBeTruthy())
    await fireEvent.press(screen.getByText('Postgres'))
    expect(onChange).toBeDefined()

    await act(async () => onChange!('inactive'))
    expect(posts).toEqual([])
    expect(screen.getByText('Undo')).toBeTruthy()

    await act(async () => onChange!('background'))
    await waitFor(() => expect(posts).toEqual([{ path: '/v1/questions/7/answer', body: { choose: 1 } }]))
  })
})
