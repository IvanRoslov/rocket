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

  describe('author label', () => {
    function taskCreatedBy(createdBy: string) {
      server.use(
        http.get('/v1/tasks/:id', () =>
          HttpResponse.json({
            id: 12,
            title: 'Billing v2',
            project_id: 'billing',
            feature_slug: 'billing-v2',
            status: 'in_progress',
            created_by: createdBy,
            created_at: 1,
            updated_at: 2,
            subtasks: [],
            open_questions: 0,
          }),
        ),
      )
    }

    it('renders "you" for a task the user created', async () => {
      taskCreatedBy('user')
      renderTask()
      expect(await screen.findByText('Billing v2')).toBeInTheDocument()

      expect(screen.getByText(/^created by you ·/)).toBeInTheDocument()
    })

    it('renders "orchestrator" for a task the orchestrator created', async () => {
      taskCreatedBy('orchestrator')
      renderTask()
      expect(await screen.findByText('Billing v2')).toBeInTheDocument()

      expect(screen.getByText(/^created by orchestrator ·/)).toBeInTheDocument()
    })

    it('renders "agent" for a task a persistent agent created', async () => {
      taskCreatedBy('agent')
      renderTask()
      expect(await screen.findByText('Billing v2')).toBeInTheDocument()

      expect(screen.getByText(/^created by agent ·/)).toBeInTheDocument()
    })
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

  it('shows the merged PR on a done subtask whose worker session has been auto-cleaned', async () => {
    renderTask()
    expect(await screen.findByText('Billing v2')).toBeInTheDocument()

    // Subtask #16 "Retire legacy billing cron" is done; its worker session
    // s-billing-v2-w4 is state 'done' (auto-cleaned after PR #400 merged)
    // and only resolves via the Overview tab's all:true sessions query.
    const row = (await screen.findByText('Retire legacy billing cron')).closest('a') as HTMLElement
    expect(within(row).getByText('PR #400 merged')).toBeInTheDocument()
  })

  it('still shows the PR for an in-progress subtask with a live worker', async () => {
    renderTask()
    expect(await screen.findByText('Billing v2')).toBeInTheDocument()

    const row = (await screen.findByText('New billing UI')).closest('a') as HTMLElement
    expect(within(row).getByText('PR #14 ✔')).toBeInTheDocument()
  })

  it('shows "PR —" for a subtask whose worker has no PR', async () => {
    renderTask()
    expect(await screen.findByText('Billing v2')).toBeInTheDocument()

    const row = (await screen.findByText('Migrate billing schema')).closest('a') as HTMLElement
    expect(within(row).getByText('PR —')).toBeInTheDocument()
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

  describe('ask the orchestrator', () => {
    it('posts {body} to /v1/tasks/:id/questions with no X-Rocket-Session header', async () => {
      let capturedBody: unknown
      let capturedHeader: string | null = null
      server.use(
        http.post('/v1/tasks/:id/questions', async ({ request }) => {
          capturedBody = await request.json()
          capturedHeader = request.headers.get('X-Rocket-Session')
          return HttpResponse.json(
            { id: 99, task_id: 12, ordinal: 4, asked_by: '', body: (capturedBody as { body: string }).body, status: 'open', whose_turn: 'orchestrator', asked_at: 1, messages: [] },
            { status: 201 },
          )
        }),
      )

      renderTask()
      expect(await screen.findByText('Billing v2')).toBeInTheDocument()
      await userEvent.click(screen.getByRole('tab', { name: /^Questions/ }))

      await userEvent.click(await screen.findByRole('button', { name: '+ Ask the orchestrator' }))
      await userEvent.type(screen.getByLabelText('Ask the orchestrator'), 'What is the deploy plan?')
      await userEvent.click(screen.getByRole('button', { name: 'Ask' }))

      await waitFor(() => expect(capturedBody).toEqual({ body: 'What is the deploy plan?' }))
      expect(capturedHeader).toBeNull()
    })

    it('disables the toggle when the task has no live orchestrator session', async () => {
      server.use(
        http.get('/v1/sessions', () =>
          HttpResponse.json([
            {
              id: 's-billing-v2-orch',
              kind: 'orchestrator',
              project_id: 'billing',
              repo_id: 'api',
              feature_slug: 'billing-v2',
              agent: 'claude',
              branch: 'feature/billing-v2',
              worktree_path: '/home/dev/.rocket/worktrees/billing-v2-orch',
              tmux_name: 'billing-v2-orch',
              state: 'errored',
              created_at: 1,
              updated_at: 1,
            },
          ]),
        ),
      )

      renderTask()
      expect(await screen.findByText('Billing v2')).toBeInTheDocument()
      await userEvent.click(screen.getByRole('tab', { name: /^Questions/ }))

      const toggle = await screen.findByRole('button', { name: '+ Ask the orchestrator' })
      expect(toggle).toBeDisabled()
    })
  })

  describe('editing title and description', () => {
    it('shows an Edit button on the Overview tab', async () => {
      renderTask()
      expect(await screen.findByText('Billing v2')).toBeInTheDocument()

      expect(screen.getByRole('button', { name: 'Edit task' })).toBeInTheDocument()
    })

    it('clicking Edit reveals title and description fields prefilled with the current values', async () => {
      renderTask()
      expect(await screen.findByText('Billing v2')).toBeInTheDocument()

      await userEvent.click(screen.getByRole('button', { name: 'Edit task' }))

      expect(screen.getByLabelText('Task title')).toHaveValue('Billing v2')
      expect(screen.getByLabelText('Task description')).toHaveValue(
        'Rework the billing subsystem: new schema, prorated plan changes, and a redesigned billing UI.',
      )
    })

    it('Save posts PATCH /v1/tasks/:id with the edited {title, description} and exits edit mode', async () => {
      let capturedBody: unknown
      server.use(
        http.patch('/v1/tasks/:id', async ({ request, params }) => {
          capturedBody = await request.json()
          return HttpResponse.json({
            id: Number(params.id),
            title: (capturedBody as { title: string }).title,
            description: (capturedBody as { description: string }).description,
            project_id: 'billing',
            status: 'in_progress',
            created_by: 'user',
            created_at: 1,
            updated_at: 2,
          })
        }),
      )

      renderTask()
      expect(await screen.findByText('Billing v2')).toBeInTheDocument()
      await userEvent.click(screen.getByRole('button', { name: 'Edit task' }))

      const titleInput = screen.getByLabelText('Task title')
      await userEvent.clear(titleInput)
      await userEvent.type(titleInput, 'Billing v2 (renamed)')

      const descInput = screen.getByLabelText('Task description')
      await userEvent.clear(descInput)
      await userEvent.type(descInput, 'Updated description text.')

      await userEvent.click(screen.getByRole('button', { name: 'Save' }))

      await waitFor(() =>
        expect(capturedBody).toEqual({
          title: 'Billing v2 (renamed)',
          description: 'Updated description text.',
        }),
      )
      await waitFor(() => expect(screen.queryByLabelText('Task title')).not.toBeInTheDocument())
    })

    it('disables Save when the title is emptied', async () => {
      renderTask()
      expect(await screen.findByText('Billing v2')).toBeInTheDocument()
      await userEvent.click(screen.getByRole('button', { name: 'Edit task' }))

      const titleInput = screen.getByLabelText('Task title')
      await userEvent.clear(titleInput)

      expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
    })

    it('Cancel exits edit mode without saving', async () => {
      renderTask()
      expect(await screen.findByText('Billing v2')).toBeInTheDocument()
      await userEvent.click(screen.getByRole('button', { name: 'Edit task' }))

      const titleInput = screen.getByLabelText('Task title')
      await userEvent.clear(titleInput)
      await userEvent.type(titleInput, 'Should not be saved')

      await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))

      expect(screen.queryByLabelText('Task title')).not.toBeInTheDocument()
      expect(screen.getByText('Billing v2')).toBeInTheDocument()
    })

    it('shows a muted placeholder for a task with no description', async () => {
      server.use(
        http.get('/v1/tasks/:id', () =>
          HttpResponse.json({
            id: 20,
            title: 'Accidental empty task',
            description: '',
            project_id: 'billing',
            status: 'backlog',
            created_by: 'user',
            created_at: 1,
            updated_at: 1,
            subtasks: [],
            open_questions: 0,
          }),
        ),
      )

      renderTask('billing', 20)
      expect(await screen.findByText('Accidental empty task')).toBeInTheDocument()

      expect(screen.getByText('No description — click Edit to add one.')).toBeInTheDocument()
    })
  })

  describe('user-opened question threads', () => {
    it('renders a "you asked the orchestrator" header instead of the orchestrator name', async () => {
      renderTask('billing', 13)
      expect(await screen.findByText('Migrate billing schema')).toBeInTheDocument()
      await userEvent.click(screen.getByRole('tab', { name: /^Questions/ }))

      expect(await screen.findAllByText('you asked the orchestrator')).toHaveLength(2)
      expect(screen.queryByText('billing-v2-w1 asked')).not.toBeInTheDocument()
    })

    it('names the participant being waited on, and "you" when it is your turn', async () => {
      renderTask('billing', 13)
      await screen.findByText('Migrate billing schema')
      await userEvent.click(screen.getByRole('tab', { name: /^Questions/ }))

      const [firstAsker] = await screen.findAllByText('you asked the orchestrator')
      const tabPanel = firstAsker.closest('.questions-tab') as HTMLElement
      expect(within(tabPanel).getByText('Should we backfill existing rows or only handle new ones going forward?')).toBeInTheDocument()
      expect(within(tabPanel).getByText('Is the migration safe to run while the app is live, or does it need a maintenance window?')).toBeInTheDocument()

      // Q4 waits on the orchestrator session, now named rather than lumped
      // under the generic "awaiting orchestrator"; Q5 waits on the human.
      expect(within(tabPanel).getByText('awaiting billing-v2-w1')).toBeInTheDocument()
      expect(within(tabPanel).getByText('awaiting you')).toBeInTheDocument()
    })

    it('the user can Answer & close a user-opened thread', async () => {
      renderTask('billing', 13)
      await screen.findByText('Migrate billing schema')
      await userEvent.click(screen.getByRole('tab', { name: /^Questions/ }))

      const question = await screen.findByText('Is the migration safe to run while the app is live, or does it need a maintenance window?')
      const card = question.closest('.question-thread') as HTMLElement
      await userEvent.type(within(card).getByRole('textbox'), 'Go ahead, run it live.')
      await userEvent.click(within(card).getByRole('button', { name: 'Answer & close' }))

      await waitFor(() => expect(document.querySelectorAll('.question-thread')).toHaveLength(1))
      expect(document.querySelector('.questions-tab__resolved-row')).toBeInTheDocument()
    })
  })

  // Q3 on task #12 is the multi-participant showcase: human + the billing
  // orchestrator + the "cto" persistent agent, with the human speaking under
  // both wire spellings (see web/src/mocks/fixtures.ts).
  describe('multi-participant threads', () => {
    async function openQ3() {
      renderTask('billing', 12)
      await screen.findByText('Billing v2')
      await userEvent.click(screen.getByRole('tab', { name: /^Questions/ }))
      await screen.findByText('Discussion · 4 replies')
      // Task #12 has exactly one open thread; the banner above the tabs
      // repeats its text, so anchor on the card itself.
      return document.querySelector('.question-thread') as HTMLElement
    }

    it('renders a human-authored entry as "you" for both "" and "human"', async () => {
      const card = await openQ3()
      const discussion = within(card).getByLabelText('Discussion')

      // One entry per wire spelling of the human, both labelled "you".
      expect(within(discussion).getAllByText('you')).toHaveLength(2)
      expect(within(discussion).queryByText('human')).not.toBeInTheDocument()
    })

    it('lists the participants and the addressees of an addressed entry', async () => {
      const card = await openQ3()

      const row = within(card).getByLabelText('Participants')
      expect(within(row).getByText('you')).toBeInTheDocument()
      expect(within(row).getByText('cto')).toBeInTheDocument()
      expect(within(row).getByText('s-billing-v2-orch')).toBeInTheDocument()

      expect(within(card).getByText('\u2192 you')).toBeInTheDocument()
    })

    it('a picked addressee reaches the API as `to`', async () => {
      const sent: Record<string, unknown>[] = []
      server.use(
        http.post('/v1/questions/:id/reply', async ({ request }) => {
          sent.push((await request.json()) as Record<string, unknown>)
          return HttpResponse.json({}, { status: 201 })
        }),
      )
      const card = await openQ3()

      await userEvent.click(within(card).getByRole('checkbox', { name: 'cto' }))
      await userEvent.type(within(card).getByRole('textbox'), 'your call')
      await userEvent.click(within(card).getByRole('button', { name: 'Clarify — keep open' }))

      await waitFor(() => expect(sent).toHaveLength(1))
      expect(sent[0]).toEqual({ body: 'your call', to: ['cto'] })
    })
  })
})
