/**
 * The task screen's Brainstorm tab (task #4901, spec §3.1) against the
 * daemon's JSON: counters, the Problem doc, the storm questions and the exit
 * gate with Go / Needs changes.
 */
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native'
import { SafeAreaProvider } from 'react-native-safe-area-context'
import { ToastProvider } from '../../src/components/Toast'
import { ServerProvider } from '../../src/servers/ServerContext'
import TaskScreen from '../../app/task/[id]'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

jest.mock('expo-router', () => ({
  router: { navigate: jest.fn(), push: jest.fn(), back: jest.fn() },
  useLocalSearchParams: () => ({ id: '12' }),
}))
jest.mock('expo-clipboard', () => ({ setStringAsync: jest.fn() }))

const TASK = {
  id: 12,
  title: 'storm me',
  project_id: 'platform',
  status: 'brainstorm',
  brainstorm_skill: 'orchestrator-brainstorming',
  created_by: 'user',
  created_at: 1785622872,
  updated_at: 1785622879,
  subtasks: [],
  open_questions: 1,
}

const BASE_Q = {
  task_id: 12,
  asked_by: 'task-12-orch',
  participants: ['human', 'task-12-orch'],
  asked_at: 1785622879,
  messages: [],
  chosen_option: null,
  answer_comment: '',
  answer_source: '',
  outcome: '',
  outcome_overridden: false,
}

const STORM_OPEN = {
  ...BASE_Q,
  id: 3,
  ordinal: 3,
  title: 'Second storm question',
  body: 'b',
  status: 'open',
  type: 'brainstorm',
  options: ['a', 'b'],
  recommended_option: 1,
  waiting_on: ['human'],
  your_turn: true,
}
const STORM_DONE = {
  ...BASE_Q,
  id: 1,
  ordinal: 1,
  title: 'First storm question',
  body: 'b',
  status: 'resolved',
  resolution: 'answered',
  type: 'brainstorm',
  options: ['x', 'y'],
  recommended_option: 2,
  chosen_option: 2,
  answer_source: 'terminal',
  outcome: 'accepted',
  answered_by: 'human',
  waiting_on: [],
  your_turn: false,
}
const PLAIN = {
  ...BASE_Q,
  id: 2,
  ordinal: 2,
  title: 'A plain decision',
  body: 'b',
  status: 'open',
  type: 'decision',
  waiting_on: ['human'],
  your_turn: true,
  recommended_option: null,
}

const STATS = {
  task_id: 12,
  title: 'storm me',
  project_id: 'platform',
  skill: 'orchestrator-brainstorming',
  questions: 5,
  answered: 4,
  accepted: 3,
  accepted_with_comment: 2,
  corrected: 1,
  wrong_turn: 1,
  spec_changes: 1,
  go_at: null,
}

const DOCS = [
  { id: 1, task_id: 12, kind: 'problem', title: 'Problem', body: 'old pain', version: 1, created_at: 1 },
  { id: 4, task_id: 12, kind: 'problem', title: 'Problem', body: 'Storms are **invisible**', version: 2, created_at: 4 },
  { id: 2, task_id: 12, kind: 'spec', title: 'Spec', body: 'pinned spec body', version: 2, created_at: 2 },
  { id: 5, task_id: 12, kind: 'spec', title: 'Spec', body: 'newer spec body', version: 3, created_at: 5 },
  { id: 3, task_id: 12, kind: 'plan', title: 'Plan', body: 'plan body', version: 1, created_at: 3 },
]

const gate = (p: Record<string, unknown>) => ({
  id: 1,
  task_id: 12,
  spec_version: 1,
  plan_version: null,
  status: 'pending',
  comment: '',
  decided_by: '',
  requested_by: 'task-12-orch',
  requested_at: 1785622890,
  decided_at: null,
  ...p,
})

const PENDING = gate({ id: 2, spec_version: 2, plan_version: 1 })
const CHANGES = gate({ id: 1, spec_version: 1, status: 'changes', comment: 'tighten the metric', decided_by: 'human', decided_at: 1785622885 })

type Reply = { status: number; body: unknown }
let posts: { path: string; body: unknown }[] = []

/** GETs come from `bodies` (a value or a {status, body} reply); POSTs answer `postReply`. */
function mockApi(overrides: Record<string, unknown> = {}, postReply: Reply = { status: 200, body: {} }) {
  posts = []
  const bodies: Record<string, unknown> = {
    '/v1/tasks/12': TASK,
    '/v1/tasks/12/questions': { questions: [STORM_OPEN, PLAIN, STORM_DONE] },
    '/v1/tasks/12/gates': { gates: [PENDING, CHANGES] },
    '/v1/tasks/12/brainstorm/stats': STATS,
    '/v1/tasks/12/docs?history=true': { docs: DOCS },
    '/v1/sessions?project=platform': [],
    ...overrides,
  }
  globalThis.fetch = jest.fn(async (url: string, init?: RequestInit) => {
    const path = String(url).replace(/^https?:\/\/[^/]+/, '')
    if (init?.method === 'POST' || init?.method === 'PATCH') {
      posts.push({ path, body: JSON.parse(String(init.body)) })
      return { ok: postReply.status < 300, status: postReply.status, json: async () => postReply.body }
    }
    const b = bodies[path]
    if (b === undefined) return { ok: false, status: 404, json: async () => ({ error: { code: 'not_found' } }) }
    if (b && typeof b === 'object' && 'status' in b && 'body' in b && typeof (b as Reply).status === 'number') {
      const r = b as Reply
      return { ok: r.status < 300, status: r.status, json: async () => r.body }
    }
    return { ok: true, status: 200, json: async () => b }
  }) as unknown as typeof fetch
}

async function renderScreen() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } })
  const metrics = { frame: { x: 0, y: 0, width: 390, height: 844 }, insets: { top: 47, left: 0, right: 0, bottom: 34 } }
  return await render(
    <SafeAreaProvider initialMetrics={metrics}>
      <QueryClientProvider client={qc}>
        <ServerProvider>
          <ToastProvider>
            <TaskScreen />
          </ToastProvider>
        </ServerProvider>
      </QueryClientProvider>
    </SafeAreaProvider>,
  )
}

const stat = (key: string) => screen.getByTestId(`stat-${key}`)
const textOf = (el: { props: { children?: unknown } }): string => {
  const c = el.props.children
  return Array.isArray(c) ? c.join('') : String(c)
}

describe('Brainstorm tab', () => {
  afterEach(() => jest.restoreAllMocks())

  it('opens by default while the task is in brainstorm', async () => {
    mockApi()
    await renderScreen()
    await waitFor(() => expect(screen.getByText('PROBLEM')).toBeTruthy())
  })

  it('is absent on an old task without a storm', async () => {
    mockApi({
      '/v1/tasks/12': { ...TASK, status: 'in_progress', brainstorm_skill: '' },
      '/v1/tasks/12/questions': { questions: [PLAIN] },
      '/v1/tasks/12/gates': { gates: [] },
    })
    await renderScreen()
    await waitFor(() => expect(screen.getByText('Overview')).toBeTruthy())
    expect(screen.queryByText('Brainstorm')).toBeNull()
    expect(screen.queryByText('PROBLEM')).toBeNull()
  })

  it('stays available after the storm and opens on tap', async () => {
    mockApi({ '/v1/tasks/12': { ...TASK, status: 'in_progress' } })
    await renderScreen()
    await waitFor(() => expect(screen.getByText('Brainstorm')).toBeTruthy())
    expect(screen.queryByText('PROBLEM')).toBeNull()
    await fireEvent.press(screen.getByText('Brainstorm'))
    await waitFor(() => expect(screen.getByText('PROBLEM')).toBeTruthy())
  })

  it('shows the storm counters', async () => {
    mockApi()
    await renderScreen()
    await waitFor(() => expect(textOf(stat('questions'))).toBe('5'))
    expect(textOf(stat('accepted'))).toBe('3')
    expect(textOf(stat('accepted_with_comment'))).toBe('2')
    expect(textOf(stat('corrected'))).toBe('1')
    expect(textOf(stat('wrong_turn'))).toBe('1')
    expect(textOf(stat('spec_changes'))).toBe('1')
  })

  it('shows dashes when the counters cannot be loaded', async () => {
    mockApi({ '/v1/tasks/12/brainstorm/stats': undefined })
    await renderScreen()
    await waitFor(() => expect(screen.getByText('PROBLEM')).toBeTruthy())
    await waitFor(() => expect(textOf(stat('questions'))).toBe('—'))
  })

  it('renders the latest Problem version', async () => {
    mockApi()
    await renderScreen()
    await waitFor(() => expect(screen.getByText('invisible')).toBeTruthy())
    expect(screen.queryByText('old pain')).toBeNull()
  })

  it('says when the Problem is not written yet', async () => {
    mockApi({ '/v1/tasks/12/docs?history=true': { docs: [] }, '/v1/tasks/12/gates': { gates: [] } })
    await renderScreen()
    await waitFor(() => expect(screen.getByText('No problem statement yet.')).toBeTruthy())
  })

  it('lists only storm questions, in order', async () => {
    mockApi()
    await renderScreen()
    await waitFor(() => expect(screen.getByText('Second storm question')).toBeTruthy())
    expect(screen.queryByText('A plain decision')).toBeNull()
    const titles = screen.getAllByText(/storm question/).map(textOf)
    expect(titles).toEqual(['First storm question', 'Second storm question'])
    // The answered one carries its summary.
    expect(screen.getByText('From terminal')).toBeTruthy()
  })

  describe('exit gate', () => {
    it('waits for a spec when there is none', async () => {
      mockApi({ '/v1/tasks/12/docs?history=true': { docs: [] }, '/v1/tasks/12/gates': { gates: [] } })
      await renderScreen()
      await waitFor(() => expect(screen.getByText('Waiting for spec')).toBeTruthy())
    })

    it('waits for a gate request when the newest gate was superseded', async () => {
      mockApi({ '/v1/tasks/12/gates': { gates: [gate({ status: 'superseded' })] } })
      await renderScreen()
      await waitFor(() => expect(screen.getByText('Waiting for gate request')).toBeTruthy())
      expect(screen.queryByText('Go')).toBeNull()
    })

    it('shows an error, not a waiting state, when gates fail to load', async () => {
      mockApi({ '/v1/tasks/12/gates': { status: 500, body: { error: { code: 'internal_error', message: 'boom' } } } })
      await renderScreen()
      await waitFor(() => expect(screen.getByText(/Could not load gates/)).toBeTruthy())
      expect(screen.queryByText('Waiting for spec')).toBeNull()
    })

    it('names the pinned versions and opens the pinned spec', async () => {
      mockApi()
      await renderScreen()
      await waitFor(() => expect(screen.getByText('Spec v2')).toBeTruthy())
      expect(screen.getByText('Plan v1')).toBeTruthy()
      await fireEvent.press(screen.getByText('Spec v2'))
      await waitFor(() => expect(screen.getByText('pinned spec body')).toBeTruthy())
      expect(screen.queryByText('newer spec body')).toBeNull()
    })

    it('lists the gate history', async () => {
      mockApi()
      await renderScreen()
      await waitFor(() => expect(screen.getByText('v1 — Needs changes: “tighten the metric”')).toBeTruthy())
    })

    it('Go posts the decision', async () => {
      mockApi()
      await renderScreen()
      await waitFor(() => expect(screen.getByText('Go')).toBeTruthy())
      await fireEvent.press(screen.getByText('Go'))
      await waitFor(() => expect(posts).toEqual([{ path: '/v1/gates/2/decide', body: { decision: 'go', comment: '' } }]))
    })

    it('Needs changes requires a comment and posts it', async () => {
      mockApi()
      await renderScreen()
      await waitFor(() => expect(screen.getByText('Needs changes')).toBeTruthy())
      await fireEvent.press(screen.getByText('Needs changes'))
      expect(posts).toEqual([])
      await fireEvent.changeText(screen.getByPlaceholderText('What needs to change?'), 'split the plan')
      await fireEvent.press(screen.getByText('Needs changes'))
      await waitFor(() =>
        expect(posts).toEqual([{ path: '/v1/gates/2/decide', body: { decision: 'changes', comment: 'split the plan' } }]),
      )
    })

    it('explains a 409 and clears the comment', async () => {
      mockApi({}, { status: 409, body: { error: { code: 'gate_not_pending', message: 'gate is not pending' } } })
      await renderScreen()
      await waitFor(() => expect(screen.getByText('Needs changes')).toBeTruthy())
      await fireEvent.changeText(screen.getByPlaceholderText('What needs to change?'), 'split the plan')
      await fireEvent.press(screen.getByText('Needs changes'))
      await waitFor(() => expect(screen.getByText('Gate is no longer current')).toBeTruthy())
      expect(screen.getByPlaceholderText('What needs to change?').props.value).toBe('')
    })

    it('reports Go once passed', async () => {
      mockApi({ '/v1/tasks/12/gates': { gates: [gate({ status: 'go', spec_version: 2, decided_at: 1785622899 })] } })
      await renderScreen()
      await waitFor(() => expect(screen.getByText('Go given on spec v2')).toBeTruthy())
    })
  })
})
