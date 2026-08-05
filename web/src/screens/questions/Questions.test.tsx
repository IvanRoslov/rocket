import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
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

function renderQuestions() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/questions']}>
        <Routes>
          <Route path="/questions" element={<QuestionsScreen />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

// The screen reads GET /v1/threads — the unified inbox — so it covers task
// AND role threads. From the fixtures: task threads 12/Q3 (stale, your turn,
// two options) and 13/Q2 (your turn) plus 13/Q1 (waiting on the orchestrator),
// role thread sre/Q1 (your turn), and the resolved 12/Q4 fyi note.

test('lists task and role threads in one list, each under its local ref', async () => {
  renderQuestions()

  expect(await screen.findByText('12/Q3')).toBeInTheDocument()
  expect(screen.getByText('sre/Q1')).toBeInTheDocument()
  expect(screen.getByText('13/Q1')).toBeInTheDocument()
})

test('groups by whose turn it is and links a task thread to its task', async () => {
  renderQuestions()

  expect(await screen.findByText('Awaiting you')).toBeInTheDocument()
  expect(screen.getByText('Awaiting others')).toBeInTheDocument()

  const link = screen.getAllByRole('link')[0]
  expect(link).toHaveAttribute('href', expect.stringMatching(/\/p\/.+\/tasks\/\d+/))
})

// The filter is an explicit product decision: OFF by default, so the human
// always sees every thread until they narrow it themselves.
test('the "waiting on me" filter is off by default and hides nothing', async () => {
  renderQuestions()

  await screen.findByText('Awaiting you')
  expect(screen.getByRole('checkbox', { name: /waiting on me/i })).not.toBeChecked()
  expect(screen.getByText('Awaiting others')).toBeInTheDocument()
})

test('checking it narrows the list to the threads waiting on you', async () => {
  renderQuestions()
  await screen.findByText('Awaiting you')

  await userEvent.click(screen.getByRole('checkbox', { name: /waiting on me/i }))

  expect(screen.queryByText('Awaiting others')).not.toBeInTheDocument()
  // 13/Q1 waits on the orchestrator; sre/Q1 waits on you and stays.
  expect(screen.queryByText('13/Q1')).not.toBeInTheDocument()
  expect(screen.getByText('sre/Q1')).toBeInTheDocument()
})

test('unchecking it brings the hidden threads back', async () => {
  renderQuestions()
  await screen.findByText('Awaiting you')
  const filter = screen.getByRole('checkbox', { name: /waiting on me/i })

  await userEvent.click(filter)
  await userEvent.click(filter)

  expect(screen.getByText('Awaiting others')).toBeInTheDocument()
})

test('badges a stale thread', async () => {
  renderQuestions()

  const row = (await screen.findByText('12/Q3')).closest('.questions-screen__row') as HTMLElement
  expect(within(row).getByText('stale')).toBeInTheDocument()
})

// An fyi thread is a status note: it is born resolved and waits on nobody, so
// it belongs in the history and must never carry a turn badge or an open count
// (spec v1 §«Тип треда»).
test('keeps fyi threads out of the open list and badges them only as fyi', async () => {
  renderQuestions()
  await screen.findByText('12/Q3')

  expect(screen.queryByText('12/Q4')).not.toBeInTheDocument()

  await userEvent.click(screen.getByRole('checkbox', { name: /show resolved/i }))

  const row = (await screen.findByText('12/Q4')).closest('.questions-screen__row') as HTMLElement
  expect(within(row).getByText('fyi')).toBeInTheDocument()
  expect(within(row).queryByText(/awaiting/i)).not.toBeInTheDocument()
})

// The inbox row carries the question only. Opening it fetches the real thread
// — the conversation, the context and the reply box.
test('expanding a row opens the full thread', async () => {
  renderQuestions()

  await userEvent.click(await screen.findByRole('button', { name: /12\/Q3/ }))

  expect(await screen.findByLabelText('Discussion')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: /Answer & close/ })).toBeInTheDocument()
})
