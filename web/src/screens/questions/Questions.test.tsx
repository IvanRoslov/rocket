import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
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

// Fixture questions (web/src/mocks/fixtures.ts): Q3 (task #12, billing) and
// Q5 (task #13, billing) are both open with your_turn true — they land under
// "Awaiting you". Q4 (task #13) is open but waits on the orchestrator — it
// lands under "Awaiting others".

test('groups open questions and links each to its task', async () => {
  renderQuestions()

  expect(await screen.findByText('Awaiting you')).toBeInTheDocument()
  expect(screen.getByText('Awaiting others')).toBeInTheDocument()

  // Fixture questions carry task_id -> the context row links to the task page.
  const link = screen.getAllByRole('link', { name: /#\d+/ })[0]
  expect(link).toHaveAttribute('href', expect.stringMatching(/\/p\/.+\/tasks\/\d+/))
})

// The filter is an explicit product decision: OFF by default, so the human
// always sees every thread until they narrow it themselves.
test('the "waiting on me" filter is off by default and hides nothing', async () => {
  renderQuestions()

  expect(await screen.findByText('Awaiting you')).toBeInTheDocument()
  expect(screen.getByRole('checkbox', { name: /waiting on me/i })).not.toBeChecked()
  expect(screen.getByText('Awaiting others')).toBeInTheDocument()
})

test('checking it narrows the list to the threads that are your turn', async () => {
  renderQuestions()
  await screen.findByText('Awaiting you')

  await userEvent.click(screen.getByRole('checkbox', { name: /waiting on me/i }))

  expect(screen.queryByText('Awaiting others')).not.toBeInTheDocument()
  expect(screen.getByText('Awaiting you')).toBeInTheDocument()
  // Q4 (task #13) waits on the orchestrator, so its text is gone.
  expect(
    screen.queryByText('Should we backfill existing rows or only handle new ones going forward?'),
  ).not.toBeInTheDocument()
})

test('unchecking it brings the hidden threads back', async () => {
  renderQuestions()
  await screen.findByText('Awaiting you')
  const filter = screen.getByRole('checkbox', { name: /waiting on me/i })

  await userEvent.click(filter)
  await userEvent.click(filter)

  expect(screen.getByText('Awaiting others')).toBeInTheDocument()
})
