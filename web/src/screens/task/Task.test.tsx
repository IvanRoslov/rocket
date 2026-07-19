import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { handlers, resetQuestions, resetSessions, resetTasks } from '../../mocks/handlers'
import { TaskScreen } from './TaskScreen'

vi.mock('../../components/TermPanel', () => ({
  TermPanel: ({ sessionId }: { sessionId: string }) => (
    <div data-testid="term-panel-stub">term panel for {sessionId}</div>
  ),
}))

const server = setupServer(...handlers)

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => {
  server.resetHandlers()
  resetTasks()
  resetQuestions()
  resetSessions()
})
afterAll(() => server.close())

function renderTask(projectId = 'billing', taskId = 12) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[`/p/${projectId}/tasks/${taskId}`]}>
        <Routes>
          <Route path="/p/:projectId/tasks/:taskId" element={<TaskScreen />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

// Fixture task #12 "Billing v2" (web/src/mocks/fixtures.ts): open_questions
// comes from Q3 (open, whose_turn "user"); Q1/Q2 are resolved.

describe('TaskScreen', () => {
  it('shows the "awaiting you" banner when an open question awaits the user', async () => {
    renderTask()
    expect(await screen.findByText('Billing v2')).toBeInTheDocument()

    expect(screen.getByText('? awaiting you')).toBeInTheDocument()
    expect(screen.getByText('Q3')).toBeInTheDocument()
  })

  it('hides the awaiting-you banner when there is no open question awaiting the user', async () => {
    server.use(
      http.get('/v1/tasks/:id/questions', () => HttpResponse.json({ questions: [] })),
    )

    renderTask()
    expect(await screen.findByText('Billing v2')).toBeInTheDocument()

    expect(screen.queryByText('? awaiting you')).not.toBeInTheDocument()
  })

  it('switches tabs on click', async () => {
    renderTask()
    expect(await screen.findByText('Billing v2')).toBeInTheDocument()

    // Overview is the default tab.
    expect(screen.getByText('Subtasks · decomposition')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('tab', { name: /^Docs/ }))
    expect(await screen.findByText('Billing v2 spec')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('tab', { name: /^Journal/ }))
    expect(await screen.findByText(/Orchestrator spawned/)).toBeInTheDocument()
  })

  it('the question banner jumps to the Questions tab', async () => {
    renderTask()
    await userEvent.click(await screen.findByText('? awaiting you'))

    expect(await screen.findByText((_, el) => el?.className === 'question-thread__question')).toBeInTheDocument()
  })

  it('"Answer & close" posts to /v1/questions/{id}/answer and removes the thread from the open list', async () => {
    renderTask()
    await userEvent.click(await screen.findByText('? awaiting you'))

    const question = await screen.findByText((_, el) => el?.className === 'question-thread__question')
    const card = question.closest('.question-thread') as HTMLElement
    await userEvent.type(within(card).getByRole('textbox'), 'Yes, credit immediately.')
    await userEvent.click(within(card).getByRole('button', { name: 'Answer & close' }))

    // The open thread card is gone; the question now shows collapsed in Resolved.
    await waitFor(() => expect(document.querySelector('.question-thread')).not.toBeInTheDocument())
    expect(document.querySelector('.questions-tab__resolved-row')).toBeInTheDocument()
    expect(screen.getByText(/prorated refunds for mid-cycle downgrades/)).toBeInTheDocument()
  })

  it('clicking a resolved thread expands it to show the full thread messages', async () => {
    renderTask()
    await screen.findByText('Billing v2')
    await userEvent.click(screen.getByRole('tab', { name: /^Questions/ }))

    const row = await screen.findByRole('button', {
      name: /Should the v2 flag default on for internal test accounts\?/,
    })
    expect(row).toHaveAttribute('aria-expanded', 'false')

    // Collapsed: the thread's reply/answer messages are not rendered yet.
    expect(screen.queryByText(/flip it on for @acme-internal accounts only/)).not.toBeInTheDocument()

    await userEvent.click(row)

    expect(row).toHaveAttribute('aria-expanded', 'true')
    expect(await screen.findByText(/flip it on for @acme-internal accounts only/)).toBeInTheDocument()

    // Clicking again collapses it back.
    await userEvent.click(row)
    expect(row).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText(/flip it on for @acme-internal accounts only/)).not.toBeInTheDocument()
  })

  it('sending a message posts /v1/messages with {to, body} and no from', async () => {
    let capturedBody: unknown
    server.use(
      http.post('/v1/messages', async ({ request }) => {
        capturedBody = await request.json()
        return HttpResponse.json({ id: 999, status: 'queued' }, { status: 201 })
      }),
    )

    renderTask()
    expect(await screen.findByText('Billing v2')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('tab', { name: /^Messages/ }))

    const input = await screen.findByLabelText('Message the orchestrator')
    await userEvent.type(input, 'Ping for a status update')
    await userEvent.click(screen.getByRole('button', { name: 'Send' }))

    await waitFor(() =>
      expect(capturedBody).toEqual({ to: 's-billing-v2-orch', body: 'Ping for a status update' }),
    )
  })

  it('own-message check is `!from`, not `to === session.id` — a relayed message with a `from` renders on the left', async () => {
    server.use(
      http.get('/v1/messages', () =>
        HttpResponse.json({
          messages: [
            // No `from`: user-authored -> own, renders right.
            { id: 1, to: 's-billing-v2-orch', body: 'from the user', status: 'delivered', attempts: 1, created_at: 1 },
            // Has `from` (a *different* session) but happens to also target
            // the orchestrator — must NOT be treated as "own" just because
            // `to === session.id`.
            { id: 2, from: 's-billing-v2-w1', to: 's-billing-v2-orch', body: 'relayed from a worker', status: 'delivered', attempts: 1, created_at: 2 },
          ],
        }),
      ),
    )

    renderTask()
    expect(await screen.findByText('Billing v2')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('tab', { name: /^Messages/ }))

    const ownRow = (await screen.findByText('from the user')).closest('.messages-tab__row') as HTMLElement
    expect(ownRow.className).toContain('messages-tab__row--own')

    const relayedRow = screen.getByText('relayed from a worker').closest('.messages-tab__row') as HTMLElement
    expect(relayedRow.className).not.toContain('messages-tab__row--own')
  })

  it('attach copies the tmux attach command to the clipboard', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      configurable: true,
    })

    renderTask()
    expect(await screen.findByText('billing-v2-orch')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'attach ⧉' }))

    expect(writeText).toHaveBeenCalledWith('rocket attach billing-v2-orch')
    expect(await screen.findByText('copied: rocket attach billing-v2-orch')).toBeInTheDocument()
  })

  it('Journal tab filters entries by kind', async () => {
    renderTask()
    expect(await screen.findByText('Billing v2')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('tab', { name: /^Journal/ }))

    expect(await screen.findByText(/Orchestrator spawned/)).toBeInTheDocument()
    expect(screen.getByText(/Prorated refund rounding/)).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'problem' }))

    expect(screen.queryByText(/Orchestrator spawned/)).not.toBeInTheDocument()
    expect(screen.getByText(/Prorated refund rounding/)).toBeInTheDocument()
  })

  it('opening a subtask shows a breadcrumb link back to the parent task', async () => {
    renderTask('billing', 13)
    expect(await screen.findByText('Migrate billing schema')).toBeInTheDocument()

    const parentLink = await screen.findByRole('link', { name: '← #12 Billing v2' })
    expect(parentLink).toHaveAttribute('href', '/p/billing/tasks/12')
  })

  it('a root task does not show a parent breadcrumb', async () => {
    renderTask()
    expect(await screen.findByText('Billing v2')).toBeInTheDocument()

    expect(screen.queryByText(/^← #\d+/)).not.toBeInTheDocument()
  })
})
