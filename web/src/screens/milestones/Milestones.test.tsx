// The Milestones page (task #1023, spec v2 «Дашборд и mobile»): the same
// kanban look as a project board, but the cards are milestones — root tasks
// outside every project, held by a persistent agent.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import { MemoryRouter } from 'react-router-dom'
import { handlers, resetAgents, resetTasks } from '../../mocks/handlers'
import { MilestonesScreen } from './MilestonesScreen'

const server = setupServer(...handlers)

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => {
  server.resetHandlers()
  resetTasks()
  resetAgents()
})
afterAll(() => server.close())

function renderScreen() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/milestones']}>
        <MilestonesScreen />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

function card(title: string): HTMLElement {
  return screen.getByText(title).closest('.kanban-card') as HTMLElement
}

// Fixture milestones (web/src/mocks/fixtures.ts): #40 backlog (nobody),
// #41 in_progress held by `sre` and quiet, #42 review held by `librarian`.

test('renders the milestone board with the holding agent on each card', async () => {
  renderScreen()

  await waitFor(() => expect(screen.getByText('Cut the on-call pager noise in half')).toBeInTheDocument())

  expect(within(card('Own the incident review ritual')).getByText('◆ sre')).toBeInTheDocument()
  expect(within(card('Docs pass over every public README')).getByText('◆ librarian')).toBeInTheDocument()
  // Nobody has taken #40 — that is the point of the page, so say it.
  expect(within(card('Cut the on-call pager noise in half')).getByText('not taken')).toBeInTheDocument()
})

test('a quiet milestone and its open questions are badged', async () => {
  renderScreen()
  await waitFor(() => expect(screen.getByText('Own the incident review ritual')).toBeInTheDocument())

  const quiet = card('Own the incident review ritual')
  expect(within(quiet).getByText(/quiet/)).toBeInTheDocument()
  expect(within(quiet).getByText(/1 awaiting you/)).toBeInTheDocument()
})

test('project tasks never show up here', async () => {
  renderScreen()
  await waitFor(() => expect(screen.getByText('Cut the on-call pager noise in half')).toBeInTheDocument())

  expect(screen.queryByText('Billing v2')).not.toBeInTheDocument()
})

test('assigning an agent from the card moves the milestone to it', async () => {
  const user = userEvent.setup()
  renderScreen()
  await waitFor(() => expect(screen.getByText('Cut the on-call pager noise in half')).toBeInTheDocument())

  await user.click(within(card('Cut the on-call pager noise in half')).getByRole('button', { name: /assign/i }))
  await user.click(await screen.findByRole('button', { name: 'librarian' }))

  await waitFor(() =>
    expect(within(card('Cut the on-call pager noise in half')).getByText('◆ librarian')).toBeInTheDocument(),
  )
})

test('unassigning hands the milestone back', async () => {
  const user = userEvent.setup()
  renderScreen()
  await waitFor(() => expect(screen.getByText('Own the incident review ritual')).toBeInTheDocument())

  await user.click(within(card('Own the incident review ritual')).getByRole('button', { name: /assign/i }))
  await user.click(await screen.findByRole('button', { name: /unassign/i }))

  await waitFor(() =>
    expect(within(card('Own the incident review ritual')).getByText('not taken')).toBeInTheDocument(),
  )
})

test('a refused move is reported and the card stays put', async () => {
  // The review gate (internal/api/milestones.go): a milestone with nothing to
  // show cannot go to review. The board must not pretend it moved.
  server.use(
    http.patch('/v1/tasks/:id', () =>
      HttpResponse.json(
        { error: { code: 'milestone_empty', message: 'nothing to review: put the result in a doc' } },
        { status: 422 },
      ),
    ),
  )
  renderScreen()
  await waitFor(() => expect(screen.getByText('Cut the on-call pager noise in half')).toBeInTheDocument())

  const backlogCard = card('Cut the on-call pager noise in half')
  const review = screen.getByText('Review').closest('.kanban-col') as HTMLElement
  const dataTransfer = { setData: () => {}, getData: () => '' }

  fireEvent.dragStart(backlogCard, { dataTransfer })
  fireEvent.dragOver(review, { dataTransfer })
  fireEvent.drop(review, { dataTransfer })

  expect(await screen.findByText(/nothing to review/)).toBeInTheDocument()
  expect(within(review).queryByText('Cut the on-call pager noise in half')).not.toBeInTheDocument()
})

test('term and chat point at the holding agent, not at a task session', async () => {
  renderScreen()
  await waitFor(() => expect(screen.getByText('Own the incident review ritual')).toBeInTheDocument())

  // `sre` is session_alive in the fixtures; its session id IS the agent id.
  const live = card('Own the incident review ritual')
  expect(within(live).getByRole('link', { name: /term/ })).toHaveAttribute('href', '/term/sre')
  expect(within(live).getByRole('link', { name: /chat/ })).toHaveAttribute('href', '/chat/sre')

  // `librarian` is not alive — no dead links into a session that isn't there.
  const idle = card('Docs pass over every public README')
  expect(within(idle).queryByRole('link', { name: /term/ })).not.toBeInTheDocument()
})
