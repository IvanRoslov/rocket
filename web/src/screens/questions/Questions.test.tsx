import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { HttpResponse, http } from 'msw'
import { setupServer } from 'msw/node'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { handlers, resetQuestions, resetTasks } from '../../mocks/handlers'
import { QuestionsScreen } from './QuestionsScreen'

const server = setupServer(...handlers)

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => {
  server.resetHandlers()
  resetQuestions()
  resetTasks()
})
afterAll(() => server.close())

// The undo window is shortened rather than faked: vitest's fake timers
// deadlock msw's request handling, so the screen takes the delay as a prop.
function renderQuestions(undoMs = 5000) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/questions']}>
        <Routes>
          <Route path="/questions" element={<QuestionsScreen undoMs={undoMs} />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

// The screen reads GET /v1/threads — the unified inbox — so it covers task
// AND role threads. From the fixtures: task threads 12/Q3 (stale, your turn,
// two options) and 13/Q2 (your turn) plus 13/Q1 (waiting on the orchestrator),
// role thread sre/Q1 (your turn), and the resolved 12/Q4 fyi note.

describe('Decide mode', () => {
  test('opens on the queue of threads waiting on you, stale first', async () => {
    renderQuestions()

    // Wait for the inbox itself, not the static heading above it.
    await screen.findByRole('heading', { level: 2 })
    const rail = document.querySelector('.q__rail') as HTMLElement
    const refs = within(rail)
      .getAllByRole('button')
      .map((b) => b.textContent ?? '')

    // 12/Q3 is the stale one, so it leads however recently it moved.
    expect(refs[0]).toContain('12/Q3')
    expect(refs.join(' ')).toContain('sre/Q1')
    // A thread waiting on the orchestrator is not on you and stays out.
    expect(refs.join(' ')).not.toContain('13/Q1')
  })

  test('shows the leading thread as a card with its options as one-tap buttons', async () => {
    renderQuestions()

    expect(await screen.findByRole('heading', { level: 2 })).toHaveTextContent(
      'Prorated refunds for mid-cycle downgrades',
    )
    expect(screen.getByRole('button', { name: /Yes, prorate downgrades/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /No, keep next-cycle/ })).toBeInTheDocument()
    expect(screen.getByText('your turn')).toBeInTheDocument()
  })

  test('threads waiting on an agent stay out of the queue but are counted', async () => {
    renderQuestions()

    expect(await screen.findByText(/threads waiting on agents/)).toBeInTheDocument()
  })
})

// The undo window is the whole reason answering is safe: the server has no
// undo, so nothing may reach it until the window closes.
describe('closing a thread', () => {
  test('picking an option closes it after the undo window, with a 1-based choose', async () => {
    const user = userEvent.setup()
    const sent: Record<string, unknown>[] = []
    server.use(
      http.post('/v1/questions/:id/answer', async ({ request }) => {
        sent.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({ id: 3 })
      }),
    )
    renderQuestions(60)
    await screen.findByRole('button', { name: /No, keep next-cycle/ })

    await user.click(screen.getByRole('button', { name: /No, keep next-cycle/ }))

    expect(screen.getByText('12/Q3 closed · 2 No, keep next-cycle')).toBeInTheDocument()

    await waitFor(() => expect(sent).toHaveLength(1))
    expect(sent[0]).toEqual({ choose: 2 })
  })

  test('Undo cancels the call outright', async () => {
    const user = userEvent.setup()
    const sent: Record<string, unknown>[] = []
    server.use(
      http.post('/v1/questions/:id/answer', async ({ request }) => {
        sent.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({ id: 3 })
      }),
    )
    // A long window here: Undo must beat a timer that has not fired.
    renderQuestions(10_000)
    await screen.findByRole('button', { name: /Yes, prorate downgrades/ })

    await user.click(screen.getByRole('button', { name: /Yes, prorate downgrades/ }))
    await user.click(screen.getByRole('button', { name: /Undo/ }))

    await new Promise((r) => setTimeout(r, 120))
    expect(sent).toHaveLength(0)
    // The thread is back, answerable again.
    expect(screen.getByRole('button', { name: /Yes, prorate downgrades/ })).toBeInTheDocument()
  })

  // Navigating away is not an Undo. The pending call used to be dropped on
  // unmount, so the human came back to a thread they had already closed —
  // still open, still yellow, with nothing to say the click had been thrown
  // away.
  test('leaving the page commits a decision still inside the undo window', async () => {
    const user = userEvent.setup()
    const sent: Record<string, unknown>[] = []
    server.use(
      http.post('/v1/questions/:id/answer', async ({ request }) => {
        sent.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({ id: 3 })
      }),
    )
    // Long window: the decision must go out because of the unmount, not
    // because the timer beat us to it.
    const view = renderQuestions(10_000)
    await screen.findByRole('heading', { level: 2 })

    await user.keyboard('x')
    expect(screen.getByText('12/Q3 closed as not relevant')).toBeInTheDocument()
    expect(sent).toHaveLength(0)

    view.unmount()

    await waitFor(() => expect(sent).toHaveLength(1))
    expect(sent[0]).toEqual({ dismiss: true })
  })

  // A closed tab never unmounts: pagehide is the last moment the browser gives
  // us to get the decision out.
  test('pagehide commits a decision still inside the undo window', async () => {
    const user = userEvent.setup()
    const sent: Record<string, unknown>[] = []
    server.use(
      http.post('/v1/questions/:id/answer', async ({ request }) => {
        sent.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({ id: 3 })
      }),
    )
    renderQuestions(10_000)
    await screen.findByRole('heading', { level: 2 })

    await user.keyboard('x')
    expect(sent).toHaveLength(0)

    window.dispatchEvent(new Event('pagehide'))

    await waitFor(() => expect(sent).toHaveLength(1))
    expect(sent[0]).toEqual({ dismiss: true })
  })

  // Backgrounding a tab on mobile often fires visibilitychange and nothing
  // else — the tab may never come back.
  test('hiding the tab commits a decision still inside the undo window', async () => {
    const user = userEvent.setup()
    const sent: Record<string, unknown>[] = []
    server.use(
      http.post('/v1/questions/:id/answer', async ({ request }) => {
        sent.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({ id: 3 })
      }),
    )
    renderQuestions(10_000)
    await screen.findByRole('heading', { level: 2 })

    await user.keyboard('x')
    expect(sent).toHaveLength(0)

    const visibility = vi
      .spyOn(document, 'visibilityState', 'get')
      .mockReturnValue('hidden')
    try {
      document.dispatchEvent(new Event('visibilitychange'))
      await waitFor(() => expect(sent).toHaveLength(1))
    } finally {
      visibility.mockRestore()
    }
    expect(sent[0]).toEqual({ dismiss: true })
  })

  // Undo already ran: there is nothing left to commit, and leaving must not
  // resurrect the call the human explicitly took back.
  test('leaving after Undo sends nothing', async () => {
    const user = userEvent.setup()
    const sent: Record<string, unknown>[] = []
    server.use(
      http.post('/v1/questions/:id/answer', async ({ request }) => {
        sent.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({ id: 3 })
      }),
    )
    const view = renderQuestions(10_000)
    await screen.findByRole('heading', { level: 2 })

    await user.keyboard('x')
    await user.click(screen.getByRole('button', { name: /Undo/ }))
    view.unmount()

    await new Promise((r) => setTimeout(r, 120))
    expect(sent).toHaveLength(0)
  })

  test('"Answer & close" refuses to close on an empty draft', async () => {
    const user = userEvent.setup()
    renderQuestions()

    await user.click(await screen.findByRole('button', { name: /Answer & close/ }))

    expect(screen.getByText('Pick an option or write the resolution first')).toBeInTheDocument()
  })
})

describe('keyboard', () => {
  test('X dismisses the current thread and B toggles Browse', async () => {
    const user = userEvent.setup()
    renderQuestions()
    await screen.findByRole('heading', { level: 2 })

    await user.keyboard('x')
    expect(screen.getByText('12/Q3 closed as not relevant')).toBeInTheDocument()

    await user.keyboard('b')
    expect(screen.getByPlaceholderText(/Filter by ref/)).toBeInTheDocument()
  })

  // Typing an answer that contains "s" or "x" must not fire the shortcuts.
  test('shortcuts are inert while typing', async () => {
    const user = userEvent.setup()
    renderQuestions()
    await screen.findByRole('heading', { level: 2 })

    await user.click(screen.getByLabelText('Your answer'))
    await user.keyboard('sx')

    expect(screen.getByLabelText('Your answer')).toHaveValue('sx')
    expect(screen.queryByText(/closed as not relevant/)).not.toBeInTheDocument()
  })
})

describe('Browse mode', () => {
  test('groups threads by whose turn it is', async () => {
    const user = userEvent.setup()
    renderQuestions()
    await screen.findByRole('heading', { level: 2 })

    await user.click(screen.getByRole('button', { name: /Browse/ }))

    // "Your turn" is also a filter chip, so assert on the group headings.
    const headings = Array.from(document.querySelectorAll('.q__group-label')).map(
      (el) => el.textContent,
    )
    expect(headings).toEqual(['Your turn', 'Waiting on agents'])
    expect(screen.getByText('13/Q1')).toBeInTheDocument()
  })

  test('the Closed filter reveals history, including the fyi note', async () => {
    const user = userEvent.setup()
    renderQuestions()
    await screen.findByRole('heading', { level: 2 })

    await user.click(screen.getByRole('button', { name: /Browse/ }))
    await user.click(screen.getByRole('button', { name: /^Closed/ }))

    expect(screen.getByText('Closed & notes')).toBeInTheDocument()
    expect(screen.getByText('12/Q4')).toBeInTheDocument()
  })

  test('the search narrows the list and says so when nothing matches', async () => {
    const user = userEvent.setup()
    renderQuestions()
    await screen.findByRole('heading', { level: 2 })

    await user.click(screen.getByRole('button', { name: /Browse/ }))
    await user.type(screen.getByLabelText('Filter threads'), 'refunds')
    expect(screen.getByText('12/Q3')).toBeInTheDocument()
    expect(screen.queryByText('13/Q1')).not.toBeInTheDocument()

    await user.clear(screen.getByLabelText('Filter threads'))
    await user.type(screen.getByLabelText('Filter threads'), 'zzzz')
    expect(screen.getByText('Nothing matches')).toBeInTheDocument()
  })

  test('a row CTA drops you back into Decide on that thread', async () => {
    const user = userEvent.setup()
    renderQuestions()
    await screen.findByRole('heading', { level: 2 })

    await user.click(screen.getByRole('button', { name: /Browse/ }))
    const row = screen.getByText('13/Q1').closest('.q__row') as HTMLElement
    await user.click(within(row).getByRole('button', { name: 'Open' }))

    expect(screen.getByRole('heading', { level: 2 })).toHaveTextContent(
      'Backfill existing rows, or new ones only?',
    )
  })
})

describe('Ask an agent', () => {
  test('opens a thread on a real orchestrator task', async () => {
    const user = userEvent.setup()
    const sent: Record<string, unknown>[] = []
    server.use(
      http.post('/v1/tasks/:id/questions', async ({ request }) => {
        sent.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({ id: 99 }, { status: 201 })
      }),
    )
    renderQuestions()
    await screen.findByRole('heading', { level: 2 })

    await user.click(screen.getByRole('button', { name: 'Ask an agent' }))
    await user.type(screen.getByLabelText('What to ask'), 'Which rounding mode?')
    await user.click(screen.getByRole('button', { name: 'Open question' }))

    await waitFor(() => expect(sent).toHaveLength(1))
    expect(sent[0]).toEqual({ body: 'Which rounding mode?' })
  })

  test('an FYI note posts as type=fyi', async () => {
    const user = userEvent.setup()
    const sent: Record<string, unknown>[] = []
    server.use(
      http.post('/v1/tasks/:id/questions', async ({ request }) => {
        sent.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({ id: 99 }, { status: 201 })
      }),
      http.post('/v1/agents/:id/questions', async ({ request }) => {
        sent.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({ id: 99 }, { status: 201 })
      }),
    )
    renderQuestions()
    await screen.findByRole('heading', { level: 2 })

    await user.click(screen.getByRole('button', { name: 'Ask an agent' }))
    await user.click(screen.getByRole('button', { name: 'FYI note' }))
    await user.type(screen.getByLabelText('What to ask'), 'Staging is back')
    await user.click(screen.getByRole('button', { name: 'Post note' }))

    await waitFor(() => expect(sent).toHaveLength(1))
    expect(sent[0]).toEqual({ body: 'Staging is back', type: 'fyi' })
  })

  // Task #1264: the dashboard shows the title as the primary line, so this
  // form must be able to set one — like the task screen's ask form does.
  test('sends the optional heading with the question', async () => {
    const user = userEvent.setup()
    const sent: Record<string, unknown>[] = []
    server.use(
      http.post('/v1/tasks/:id/questions', async ({ request }) => {
        sent.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({ id: 99 }, { status: 201 })
      }),
    )
    renderQuestions()
    await screen.findByRole('heading', { level: 2 })

    await user.click(screen.getByRole('button', { name: 'Ask an agent' }))
    await user.type(screen.getByLabelText('Question heading (optional)'), 'Deploy plan')
    await user.type(screen.getByLabelText('What to ask'), 'What is the deploy plan?')
    await user.click(screen.getByRole('button', { name: 'Open question' }))

    await waitFor(() => expect(sent).toHaveLength(1))
    expect(sent[0]).toEqual({ body: 'What is the deploy plan?', title: 'Deploy plan' })
  })
})
