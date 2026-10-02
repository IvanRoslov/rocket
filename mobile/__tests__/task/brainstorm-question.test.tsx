/**
 * A brainstorm thread in the full QuestionCard (task #4901, spec §2.2/§3.1):
 * the recommended option is starred, an option can carry a comment, and an
 * answered thread shows what was chosen, its outcome (which the human may
 * change) and whether the answer came from the terminal.
 */
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react-native'
import { SafeAreaProvider } from 'react-native-safe-area-context'
import type { Question } from '../../src/api/types'
import { QuestionCard } from '../../src/components/QuestionCard'
import { ToastProvider } from '../../src/components/Toast'
import { ServerProvider } from '../../src/servers/ServerContext'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

const STORM: Question = {
  id: 5,
  task_id: 12,
  ordinal: 2,
  asked_by: 'task-12-orch',
  title: 'Where do gates live?',
  body: 'details',
  status: 'open',
  type: 'brainstorm',
  options: ['own table', 'task column'],
  recommended_option: 1,
  chosen_option: null,
  answer_comment: '',
  answer_source: '',
  outcome: '',
  outcome_overridden: false,
  participants: ['human', 'task-12-orch'],
  waiting_on: ['human'],
  your_turn: true,
  asked_at: 1785622879,
  messages: [],
}

const ANSWERED: Question = {
  ...STORM,
  status: 'resolved',
  resolution: 'answered',
  your_turn: false,
  waiting_on: [],
  chosen_option: 1,
  answer_comment: 'keep history',
  answer_source: 'ui',
  outcome: 'accepted',
  answered_by: 'human',
  resolved_at: 1785622900,
}

function mockFetch(status = 200) {
  globalThis.fetch = jest.fn(async () => ({
    ok: status < 400,
    status,
    json: async () => (status < 400 ? {} : { error: { code: 'x', message: 'nope' } }),
  })) as unknown as typeof fetch
}

function renderCard(q: Question) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } })
  const metrics = { frame: { x: 0, y: 0, width: 390, height: 844 }, insets: { top: 0, left: 0, right: 0, bottom: 0 } }
  return render(
    <SafeAreaProvider initialMetrics={metrics}>
      <QueryClientProvider client={qc}>
        <ServerProvider>
          <ToastProvider>
            <QuestionCard q={q} />
          </ToastProvider>
        </ServerProvider>
      </QueryClientProvider>
    </SafeAreaProvider>,
  )
}

function callTo(path: string) {
  return (fetch as jest.Mock).mock.calls.find(([u]: [string]) => String(u).includes(path))
}

describe('brainstorm QuestionCard — open', () => {
  afterEach(() => jest.restoreAllMocks())

  it('stars the recommended option only', async () => {
    mockFetch()
    await renderCard(STORM)
    expect(within(screen.getByTestId('option-1')).getByText('★ Recommended')).toBeTruthy()
    expect(within(screen.getByTestId('option-2')).queryByText('★ Recommended')).toBeNull()
  })

  it('sends the comment along with the tapped option', async () => {
    mockFetch()
    await renderCard(STORM)
    await fireEvent.changeText(screen.getByPlaceholderText('Comment or your own answer…'), 'but keep it small')
    await waitFor(() => expect(screen.getByPlaceholderText('Comment or your own answer…').props.value).toBe('but keep it small'))
    await fireEvent.press(screen.getByTestId('option-2'))
    await waitFor(() => {
      const call = callTo('/v1/questions/5/answer')
      expect(call).toBeTruthy()
      expect(JSON.parse(call[1].body)).toEqual({ choose: 2, body: 'but keep it small' })
    })
  })

  it('sends a bare choose when there is no comment', async () => {
    mockFetch()
    await renderCard(STORM)
    await fireEvent.press(screen.getByTestId('option-1'))
    await waitFor(() => expect(JSON.parse(callTo('/v1/questions/5/answer')[1].body)).toEqual({ choose: 1 }))
  })

  it('answers in own words from the same box', async () => {
    mockFetch()
    await renderCard(STORM)
    await fireEvent.changeText(screen.getByPlaceholderText('Comment or your own answer…'), 'neither — use a view')
    await fireEvent.press(screen.getByText('Answer & close'))
    await waitFor(() =>
      expect(JSON.parse(callTo('/v1/questions/5/answer')[1].body)).toEqual({ body: 'neither — use a view' }),
    )
  })

  it('has a single text box, so nothing typed is lost on an option tap', async () => {
    mockFetch()
    await renderCard(STORM)
    expect(screen.queryByPlaceholderText(/Write a reply/)).toBeNull()
  })

  it('renders an old brainstorm thread without a recommendation and no star', async () => {
    mockFetch()
    await renderCard({ ...STORM, recommended_option: null })
    expect(screen.queryByText('★ Recommended')).toBeNull()
    expect(screen.getByText('own table')).toBeTruthy()
  })

  it('keeps a plain decision thread free of the comment field', async () => {
    mockFetch()
    await renderCard({ ...STORM, type: 'decision', recommended_option: null })
    expect(screen.queryByPlaceholderText('Comment or your own answer…')).toBeNull()
  })
})

describe('brainstorm QuestionCard — answered', () => {
  afterEach(() => jest.restoreAllMocks())

  it('shows the chosen option, the comment and the outcome', async () => {
    mockFetch()
    await renderCard(ANSWERED)
    expect(screen.getByText('Chose: own table')).toBeTruthy()
    expect(screen.getByText('keep history')).toBeTruthy()
    expect(screen.getByText('Accepted with comment')).toBeTruthy()
    expect(screen.queryByText('From terminal')).toBeNull()
    // A closed thread takes no more answers.
    expect(screen.queryByText('Answer & close')).toBeNull()
  })

  it('labels an own-words answer and a terminal record', async () => {
    mockFetch()
    await renderCard({ ...ANSWERED, chosen_option: null, answer_comment: 'neither', outcome: 'wrong_turn', answer_source: 'terminal' })
    expect(screen.getByText('Own answer')).toBeTruthy()
    expect(screen.getByText('neither')).toBeTruthy()
    expect(screen.getByText('Wrong turn')).toBeTruthy()
    expect(screen.getByText('From terminal')).toBeTruthy()
  })

  it('names the agent who answered, and nobody for the human', async () => {
    mockFetch()
    await renderCard({ ...ANSWERED, answered_by: 'cto' })
    expect(screen.getByText('answered by cto')).toBeTruthy()
    await screen.rerender(
      <SafeAreaProvider initialMetrics={{ frame: { x: 0, y: 0, width: 390, height: 844 }, insets: { top: 0, left: 0, right: 0, bottom: 0 } }}>
        <QueryClientProvider client={new QueryClient()}>
          <ServerProvider>
            <ToastProvider>
              <QuestionCard q={ANSWERED} />
            </ToastProvider>
          </ServerProvider>
        </QueryClientProvider>
      </SafeAreaProvider>,
    )
    expect(screen.queryByText(/answered by/)).toBeNull()
  })

  it('marks an overridden outcome', async () => {
    mockFetch()
    await renderCard({ ...ANSWERED, outcome: 'corrected', outcome_overridden: true })
    expect(screen.getByText('Corrected')).toBeTruthy()
    expect(screen.getByText('changed by you')).toBeTruthy()
  })

  it('changes the outcome through PATCH', async () => {
    mockFetch()
    await renderCard(ANSWERED)
    await fireEvent.press(screen.getByTestId('outcome-change'))
    await fireEvent.press(await screen.findByText('Mark as Wrong turn'))
    await waitFor(() => {
      const call = callTo('/v1/questions/5/outcome')
      expect(call).toBeTruthy()
      expect(call[1].method).toBe('PATCH')
      expect(JSON.parse(call[1].body)).toEqual({ outcome: 'wrong_turn' })
    })
  })

  it('reports a rejected outcome change instead of silently reverting', async () => {
    mockFetch(409)
    await renderCard(ANSWERED)
    await fireEvent.press(screen.getByTestId('outcome-change'))
    await fireEvent.press(await screen.findByText('Mark as Corrected'))
    await waitFor(() => expect(screen.getByText('nope')).toBeTruthy())
    expect(screen.getByText('Accepted with comment')).toBeTruthy()
  })

  it('shows no outcome for a dismissed storm thread', async () => {
    mockFetch()
    await renderCard({ ...ANSWERED, resolution: 'dismissed', outcome: '', chosen_option: null, answer_comment: '' })
    expect(screen.queryByTestId('outcome-change')).toBeNull()
  })
})
