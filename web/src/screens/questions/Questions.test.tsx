import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
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
// Q5 (task #13, billing) are both open with whose_turn "user" — they land
// under "Awaiting you". Q4 (task #13) is open but whose_turn "orchestrator"
// — it lands under "Awaiting orchestrator".

test('groups open questions and links each to its task', async () => {
  renderQuestions()

  expect(await screen.findByText('Awaiting you')).toBeInTheDocument()
  expect(screen.getByText('Awaiting orchestrator')).toBeInTheDocument()

  // Fixture questions carry task_id -> the context row links to the task page.
  const link = screen.getAllByRole('link', { name: /#\d+/ })[0]
  expect(link).toHaveAttribute('href', expect.stringMatching(/\/p\/.+\/tasks\/\d+/))
})
