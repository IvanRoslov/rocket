import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { setupServer } from 'msw/node'
import { http, HttpResponse } from 'msw'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { handlers, resetTasks } from '../../mocks/handlers'
import { KanbanScreen } from './KanbanScreen'

const server = setupServer(...handlers)

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => {
  server.resetHandlers()
  resetTasks()
})
afterAll(() => server.close())

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
// #10 backlog, #12 in_progress (session s-billing-v2-orch, 2 workers), #11 review, #9 done.

test('renders columns with fixtures distributed by status', async () => {
  renderKanban()

  await waitFor(() => expect(screen.getByText('Invoice PDF export')).toBeInTheDocument())

  expect(screen.getByText('Billing v2').closest('.kanban-card')).toBeInTheDocument()
  expect(screen.getByText('Webhook retry backoff')).toBeInTheDocument()
  expect(screen.getByText('Legacy invoice migration')).toBeInTheDocument()

  // #12 shows orchestrator liveness with 3 workers (s-billing-v2-w1/w2/w3).
  expect(screen.getByText(/orch:/)).toBeInTheDocument()
  expect(screen.getByText(/3 workers/)).toBeInTheDocument()
})

test('PR badges aggregate worker sessions: 1 PR open + CI ✔ for #12 (s-billing-v2-w2)', async () => {
  renderKanban()
  await waitFor(() => expect(screen.getByText('Billing v2')).toBeInTheDocument())

  const card = screen.getByText('Billing v2').closest('.kanban-card') as HTMLElement
  expect(within(card).getByText('1 PR open')).toBeInTheDocument()
  expect(within(card).getByText('CI ✔')).toBeInTheDocument()
  expect(within(card).queryByText(/merged/)).not.toBeInTheDocument()
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
