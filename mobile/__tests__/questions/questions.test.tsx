/**
 * Renders the Questions tab against the daemon's inbox JSON: what is listed,
 * and that an answer waits out the 5 s Undo window before it is POSTed.
 */
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react-native'
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
function mockApi(opts: { threads?: unknown[]; postStatus?: number; postBody?: unknown } = {}) {
  posts = []
  const threads = opts.threads ?? [MINE, OTHER]
  globalThis.fetch = jest.fn(async (url: string, init?: RequestInit) => {
    const path = String(url).replace(/^https?:\/\/[^/]+/, '')
    if (init?.method === 'POST') {
      posts.push({ path, body: JSON.parse(String(init.body)) })
      const status = opts.postStatus ?? 200
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
})
