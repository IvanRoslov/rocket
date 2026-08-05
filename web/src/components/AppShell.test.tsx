import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { setupServer } from 'msw/node'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { LAST_PROJECT_STORAGE_KEY } from '../lib/lastProject'
import { handlers } from '../mocks/handlers'
import { AppShell } from './AppShell'

const server = setupServer(...handlers)

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => {
  server.resetHandlers()
  window.localStorage.clear()
})
afterAll(() => server.close())

function renderShell(initialPath = '/') {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initialPath]}>
        <Routes>
          <Route path="/*" element={<AppShell />} />
          <Route path="/p/:projectId/*" element={<AppShell />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

function tabHref(name: string) {
  return screen.getByRole('link', { name }).getAttribute('href')
}

test('шапка: лого и табы', () => {
  renderShell()
  expect(screen.getByText('rocket')).toBeInTheDocument()
  for (const tab of ['Projects', 'Kanban', 'System', 'Settings']) {
    expect(screen.getByRole('link', { name: tab })).toBeInTheDocument()
  }
})

test('с главной страницы Kanban ведёт на запомненный проект, Agents — на глобальный список', async () => {
  window.localStorage.setItem(LAST_PROJECT_STORAGE_KEY, 'analytics')
  renderShell('/')
  expect(tabHref('Kanban')).toBe('/p/analytics')
  // Агент может быть зарегистрирован вообще без проекта, поэтому таб Agents
  // всегда ведёт на глобальный /agents.
  expect(tabHref('Agents')).toBe('/agents')
})

test('без запомненного проекта Kanban ведёт на первый проект из списка', async () => {
  renderShell('/')
  await waitFor(() => expect(tabHref('Kanban')).toBe('/p/billing'))
  expect(tabHref('Agents')).toBe('/agents')
})

test('внутри проекта Kanban ведёт на текущий проект и он запоминается', async () => {
  window.localStorage.setItem(LAST_PROJECT_STORAGE_KEY, 'analytics')
  renderShell('/p/billing/agents')
  expect(tabHref('Kanban')).toBe('/p/billing')
  expect(tabHref('Agents')).toBe('/agents')
  await waitFor(() =>
    expect(window.localStorage.getItem(LAST_PROJECT_STORAGE_KEY)).toBe('billing'),
  )
})

test('на глобальном /agents активен таб Agents', () => {
  renderShell('/agents')
  expect(screen.getByRole('link', { name: 'Agents' })).toHaveAttribute('aria-current', 'page')
  expect(screen.getByRole('link', { name: 'Kanban' })).not.toHaveAttribute('aria-current')
})

test('на /p/:id/agents активен только таб Agents', () => {
  renderShell('/p/billing/agents')
  expect(screen.getByRole('link', { name: 'Agents' })).toHaveAttribute('aria-current', 'page')
  expect(screen.getByRole('link', { name: 'Kanban' })).not.toHaveAttribute('aria-current')
})

test('на /p/:id активен только таб Kanban', () => {
  renderShell('/p/billing')
  expect(screen.getByRole('link', { name: 'Kanban' })).toHaveAttribute('aria-current', 'page')
  expect(screen.getByRole('link', { name: 'Agents' })).not.toHaveAttribute('aria-current')
})

test('на главной ни Kanban, ни Agents не активны', () => {
  window.localStorage.setItem(LAST_PROJECT_STORAGE_KEY, 'analytics')
  renderShell('/')
  expect(screen.getByRole('link', { name: 'Kanban' })).not.toHaveAttribute('aria-current')
  expect(screen.getByRole('link', { name: 'Agents' })).not.toHaveAttribute('aria-current')
})

// The nav counter reads the unified inbox (GET /v1/threads), so a ROLE thread
// waiting on the human counts too — it used to read GET /v1/questions, which
// knows only task threads, and role threads never lit this badge.
//
// It is driven by `your_turn`, the caller-relative field, not by the legacy
// two-party `whose_turn` string. They agree on the real API, so the handler
// below deliberately makes them disagree to pin down which one wins.
test('the Questions tab counts the threads whose turn is yours', async () => {
  server.use(
    http.get('/v1/threads', () =>
      HttpResponse.json({
        threads: [
          { id: 1, kind: 'task', task_id: 12, local_ref: '12/Q1', ordinal: 1, your_turn: true, whose_turn: 'orchestrator', attention: ['human'], messages: [] },
          { id: 2, kind: 'task', task_id: 12, local_ref: '12/Q2', ordinal: 2, your_turn: true, whose_turn: '', attention: ['human'], messages: [] },
          { id: 3, kind: 'task', task_id: 12, local_ref: '12/Q3', ordinal: 3, your_turn: false, whose_turn: 'user', attention: ['cto'], messages: [] },
          { id: 4, kind: 'role', role_id: 'sre', local_ref: 'sre/Q1', ordinal: 1, your_turn: true, whose_turn: 'user', attention: ['human'], messages: [] },
        ],
      }),
    ),
  )
  renderShell()

  const questions = await screen.findByRole('link', { name: /Questions/ })
  // Two task threads plus the role thread — three, not two.
  await waitFor(() => expect(questions).toHaveTextContent('3'))
})
