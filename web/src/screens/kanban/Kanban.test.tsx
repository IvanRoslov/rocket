import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { setupServer } from 'msw/node'
import { http, HttpResponse } from 'msw'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { handlers, resetSettings, resetTasks } from '../../mocks/handlers'
import { KanbanScreen } from './KanbanScreen'

const server = setupServer(...handlers)

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => {
  server.resetHandlers()
  resetTasks()
  resetSettings()
})
afterAll(() => server.close())

/** Sets the fixture-backed GitHub token via the real PUT /v1/settings handler
 * (mocks/handlers.ts), so the GET /v1/github/issues no_token branch reflects
 * it — overriding just the GET /v1/settings response wouldn't touch the
 * module-level settingsState the issues handler actually checks. */
async function connectGithub() {
  await fetch('/v1/settings', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ github_token: 'ghp_1234567890' }),
  })
}

function renderKanban(projectId = 'billing') {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[`/p/${projectId}`]}>
        <Routes>
          <Route path="/p/:projectId" element={<KanbanScreen />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

// Fixture distribution (web/src/mocks/fixtures.ts), billing project:
// #10 backlog, #12 in_progress (session s-billing-v2-orch, 4 workers —
// w1/w2/w3 live plus done w4 whose PR merged), #11 review, #9 done.
// The kanban board fetches sessions with `all: true` so a done worker (PR
// merged) still counts toward the worker total and PR badges.

test('renders columns with fixtures distributed by status', async () => {
  renderKanban()

  await waitFor(() => expect(screen.getByText('Invoice PDF export')).toBeInTheDocument())

  expect(screen.getByText('Billing v2').closest('.kanban-card')).toBeInTheDocument()
  expect(screen.getByText('Webhook retry backoff')).toBeInTheDocument()
  expect(screen.getByText('Legacy invoice migration')).toBeInTheDocument()

  // #12 shows orchestrator liveness with 4 workers (s-billing-v2-w1/w2/w3/w4).
  expect(screen.getByText(/orch:/)).toBeInTheDocument()
  expect(screen.getByText(/4 workers/)).toBeInTheDocument()
})

test('PR badges aggregate worker sessions: 1 PR open + 1 merged + CI ✔ for #12', async () => {
  renderKanban()
  await waitFor(() => expect(screen.getByText('Billing v2')).toBeInTheDocument())

  const card = screen.getByText('Billing v2').closest('.kanban-card') as HTMLElement
  expect(within(card).getByText('1 PR open')).toBeInTheDocument()
  expect(within(card).getByText(/1.*merged/)).toBeInTheDocument()
  expect(within(card).getByText('CI ✔')).toBeInTheDocument()
})

test('question badge: warn "awaiting you" when questions_awaiting_user > 0', async () => {
  renderKanban()
  await waitFor(() => expect(screen.getByText('Billing v2')).toBeInTheDocument())

  // #12 "Billing v2" (fixtures.ts) is the showcase task with
  // open_questions: 2, questions_awaiting_user: 1 — the warn badge should
  // win over the neutral "open" badge.
  const card = screen.getByText('Billing v2').closest('.kanban-card') as HTMLElement
  expect(within(card).getByText('? 1 awaiting you')).toBeInTheDocument()
  expect(within(card).queryByText('? 2 open')).not.toBeInTheDocument()
})

test('question badge: neutral "open" when open_questions > 0 but nothing awaiting the user', async () => {
  renderKanban()
  await waitFor(() => expect(screen.getByText('Legacy invoice migration')).toBeInTheDocument())

  // #9 "Legacy invoice migration" (fixtures.ts) has open_questions: 1,
  // questions_awaiting_user: 0 — the neutral "open" badge, not the warn one.
  const card = screen.getByText('Legacy invoice migration').closest('.kanban-card') as HTMLElement
  expect(within(card).getByText('? 1 open')).toBeInTheDocument()
  expect(within(card).queryByText(/awaiting you/)).not.toBeInTheDocument()
})

test('cancelled column hidden by default, shown via checkbox', async () => {
  renderKanban()
  await waitFor(() => expect(screen.getByText('Invoice PDF export')).toBeInTheDocument())

  expect(screen.queryByText('Cancelled')).not.toBeInTheDocument()

  await userEvent.click(screen.getByRole('checkbox', { name: /show cancelled/i }))

  await waitFor(() => expect(screen.getByText('Cancelled')).toBeInTheDocument())
})

test('search filters cards by title', async () => {
  renderKanban()
  await waitFor(() => expect(screen.getByText('Invoice PDF export')).toBeInTheDocument())

  await userEvent.type(screen.getByPlaceholderText('Search tasks…'), 'webhook')

  expect(screen.getByText('Webhook retry backoff')).toBeInTheDocument()
  expect(screen.queryByText('Invoice PDF export')).not.toBeInTheDocument()
  expect(screen.queryByText('Billing v2')).not.toBeInTheDocument()
})

test('Start button on a backlog card opens the agent modal and posts /start', async () => {
  let capturedBody: unknown
  server.use(
    http.post('/v1/tasks/:id/start', async ({ request, params }) => {
      capturedBody = await request.json().catch(() => undefined)
      return HttpResponse.json(
        { task_id: Number(params.id), feature_slug: 'invoice-pdf-export', session_id: 's-invoice-pdf-export-orch' },
        { status: 201 },
      )
    }),
  )

  renderKanban()
  await waitFor(() => expect(screen.getByText('Invoice PDF export')).toBeInTheDocument())

  await userEvent.click(screen.getByRole('button', { name: 'Start ▸' }))

  const dialog = await screen.findByRole('dialog')
  await userEvent.type(within(dialog).getByLabelText('Agent'), 'claude')
  await userEvent.click(within(dialog).getByRole('button', { name: 'Start ▸' }))

  await waitFor(() => expect(capturedBody).toEqual({ agent: 'claude' }))
})

test('drop handler calls PATCH with the target column status', async () => {
  let capturedStatus: unknown
  server.use(
    http.patch('/v1/tasks/:id', async ({ request, params }) => {
      const body = (await request.json()) as { status?: string }
      capturedStatus = body.status
      return HttpResponse.json({
        id: Number(params.id),
        title: 'Webhook retry backoff',
        project_id: 'billing',
        status: body.status,
        created_by: 'orchestrator',
        created_at: 0,
        updated_at: 0,
      })
    }),
  )

  renderKanban()
  await waitFor(() => expect(screen.getByText('Webhook retry backoff')).toBeInTheDocument())

  const card = screen.getByText('Webhook retry backoff').closest('.kanban-card') as HTMLElement
  const doneColumn = screen.getByText('Done').closest('.kanban-col') as HTMLElement

  const dataTransfer = { setData: () => {}, getData: () => '' }
  fireEvent.dragStart(card, { dataTransfer })
  fireEvent.dragOver(doneColumn, { dataTransfer })
  fireEvent.drop(doneColumn, { dataTransfer })

  await waitFor(() => expect(capturedStatus).toBe('done'))
})

test('dropping a card back into its own column does not PATCH', async () => {
  let patchCalled = false
  server.use(
    http.patch('/v1/tasks/:id', async ({ request, params }) => {
      patchCalled = true
      const body = (await request.json()) as { status?: string }
      return HttpResponse.json({
        id: Number(params.id),
        title: 'Webhook retry backoff',
        project_id: 'billing',
        status: body.status,
        created_by: 'orchestrator',
        created_at: 0,
        updated_at: 0,
      })
    }),
  )

  renderKanban()
  await waitFor(() => expect(screen.getByText('Webhook retry backoff')).toBeInTheDocument())

  // #11 "Webhook retry backoff" is already in Review — dropping it back
  // into Review should be a no-op.
  const card = screen.getByText('Webhook retry backoff').closest('.kanban-card') as HTMLElement
  const reviewColumn = screen.getByText('Review').closest('.kanban-col') as HTMLElement

  const dataTransfer = { setData: () => {}, getData: () => '' }
  fireEvent.dragStart(card, { dataTransfer })
  fireEvent.dragOver(reviewColumn, { dataTransfer })
  fireEvent.drop(reviewColumn, { dataTransfer })

  // Give any (unwanted) async PATCH a chance to fire.
  await new Promise((r) => setTimeout(r, 0))
  expect(patchCalled).toBe(false)
})

test('POST /v1/tasks/{id}/start on an already-started task 409s with already_started (mirrors tasks.go)', async () => {
  // Task #12 "Billing v2" (fixtures.ts) is already in_progress with a
  // session_id — the Start button isn't rendered for it client-side, but
  // the mock handler should still mirror the daemon's guard for direct
  // API calls / future UI paths.
  const res = await fetch('/v1/tasks/12/start', { method: 'POST' })
  expect(res.status).toBe(409)
  const body = await res.json()
  expect(body.error.code).toBe('already_started')
})

test('creating a task posts title/description/project', async () => {
  let capturedBody: unknown
  server.use(
    http.post('/v1/tasks', async ({ request }) => {
      capturedBody = await request.json().catch(() => undefined)
      return HttpResponse.json(
        {
          id: 999,
          title: 'New thing',
          description: '# details',
          project_id: 'billing',
          status: 'backlog',
          created_by: 'user',
          created_at: 0,
          updated_at: 0,
        },
        { status: 201 },
      )
    }),
  )

  renderKanban()
  await waitFor(() => expect(screen.getByText('Invoice PDF export')).toBeInTheDocument())

  await userEvent.click(screen.getByRole('button', { name: 'Add task to Backlog' }))

  const dialog = await screen.findByRole('dialog')
  await userEvent.type(within(dialog).getByLabelText('Title'), 'New thing')
  await userEvent.type(within(dialog).getByLabelText('Description'), '# details')
  await userEvent.click(within(dialog).getByRole('button', { name: 'Create task' }))

  await waitFor(() =>
    expect(capturedBody).toEqual({ title: 'New thing', description: '# details', project: 'billing' }),
  )
})

// "From GitHub issue" mode (mocks/handlers.ts GET /v1/github/issues, fixture
// githubIssues in mocks/fixtures.ts). billing project: main `api` (3 open
// issues, #241/#238/#190), linked `web` (1 open issue, #55) + `infra` (no
// GitHub origin -> not_a_github_repo). Settings fixture starts with no
// GitHub token, so `no_token` is the default state until a test sets one.

test('switching to From GitHub issue shows the repo picker with the project\'s repos, defaulting to main', async () => {
  await connectGithub()

  renderKanban()
  await waitFor(() => expect(screen.getByText('Invoice PDF export')).toBeInTheDocument())
  await userEvent.click(screen.getByRole('button', { name: 'Add task to Backlog' }))
  const dialog = await screen.findByRole('dialog')

  await userEvent.click(within(dialog).getByRole('button', { name: 'From GitHub issue' }))

  const repoSelect = within(dialog).getByLabelText('Repository') as HTMLSelectElement
  expect(repoSelect.value).toBe('api')
  const optionValues = Array.from(repoSelect.options).map((o) => o.value)
  expect(optionValues).toEqual(['api', 'web', 'infra'])

  await waitFor(() => expect(within(dialog).getByText('Rate limit billing webhooks')).toBeInTheDocument())
  expect(within(dialog).getByText('#241')).toBeInTheDocument()
  expect(within(dialog).getByText('bug')).toBeInTheDocument()
})

test('search filters the issue list by number/title/label', async () => {
  await connectGithub()

  renderKanban()
  await waitFor(() => expect(screen.getByText('Invoice PDF export')).toBeInTheDocument())
  await userEvent.click(screen.getByRole('button', { name: 'Add task to Backlog' }))
  const dialog = await screen.findByRole('dialog')
  await userEvent.click(within(dialog).getByRole('button', { name: 'From GitHub issue' }))

  await waitFor(() => expect(within(dialog).getByText('Rate limit billing webhooks')).toBeInTheDocument())
  expect(within(dialog).getByText('Add prorated refund support')).toBeInTheDocument()

  await userEvent.type(within(dialog).getByPlaceholderText(/search issues/i), 'refund')

  expect(within(dialog).getByText('Add prorated refund support')).toBeInTheDocument()
  expect(within(dialog).queryByText('Rate limit billing webhooks')).not.toBeInTheDocument()
})

test('selecting an issue prefills title/description with a Source line, and Create posts it', async () => {
  await connectGithub()
  let capturedBody: unknown
  server.use(
    http.post('/v1/tasks', async ({ request }) => {
      capturedBody = await request.json().catch(() => undefined)
      return HttpResponse.json(
        { id: 999, title: 'x', project_id: 'billing', status: 'backlog', created_by: 'user', created_at: 0, updated_at: 0 },
        { status: 201 },
      )
    }),
  )

  renderKanban()
  await waitFor(() => expect(screen.getByText('Invoice PDF export')).toBeInTheDocument())
  await userEvent.click(screen.getByRole('button', { name: 'Add task to Backlog' }))
  const dialog = await screen.findByRole('dialog')
  await userEvent.click(within(dialog).getByRole('button', { name: 'From GitHub issue' }))

  await waitFor(() => expect(within(dialog).getByText('Rate limit billing webhooks')).toBeInTheDocument())
  await userEvent.click(within(dialog).getByText('Rate limit billing webhooks'))

  expect(within(dialog).getByText('#241', { selector: 'strong' })).toBeInTheDocument()
  const titleInput = within(dialog).getByLabelText('Title') as HTMLInputElement
  const descriptionInput = within(dialog).getByLabelText('Description') as HTMLTextAreaElement
  expect(titleInput.value).toBe('Rate limit billing webhooks')
  expect(descriptionInput.value).toContain('https://github.com/acme/api/issues/241')
  expect(descriptionInput.value).toMatch(/Source: https:\/\/github\.com\/acme\/api\/issues\/241$/)

  await userEvent.click(within(dialog).getByRole('button', { name: 'Create task' }))

  await waitFor(() =>
    expect(capturedBody).toMatchObject({
      title: 'Rate limit billing webhooks',
      project: 'billing',
    }),
  )
  expect((capturedBody as { description: string }).description).toContain(
    'Source: https://github.com/acme/api/issues/241',
  )
})

test('no GitHub token configured shows the Settings hint', async () => {
  // Default settings fixture has no token — no override needed.
  renderKanban()
  await waitFor(() => expect(screen.getByText('Invoice PDF export')).toBeInTheDocument())
  await userEvent.click(screen.getByRole('button', { name: 'Add task to Backlog' }))
  const dialog = await screen.findByRole('dialog')
  await userEvent.click(within(dialog).getByRole('button', { name: 'From GitHub issue' }))

  await waitFor(() => expect(within(dialog).getByText(/Settings/)).toBeInTheDocument())
  expect(within(dialog).getByRole('link', { name: 'Settings' })).toHaveAttribute('href', '/settings')
})

test('waiting badge: shown when the task\'s session is stalled on interactive input', async () => {
  renderKanban()
  await waitFor(() => expect(screen.getByText('Billing v2')).toBeInTheDocument())

  // #12 "Billing v2" (fixtures.ts) carries the derived waiting_terminal flag;
  // #10 "Invoice PDF export" does not.
  const stalled = screen.getByText('Billing v2').closest('.kanban-card') as HTMLElement
  expect(within(stalled).getByText(/waiting for input/)).toBeInTheDocument()

  const moving = screen.getByText('Invoice PDF export').closest('.kanban-card') as HTMLElement
  expect(within(moving).queryByText(/waiting for input/)).not.toBeInTheDocument()
})
