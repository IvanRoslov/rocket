import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
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

test('с главной страницы Kanban и Agents ведут на запомненный проект', async () => {
  window.localStorage.setItem(LAST_PROJECT_STORAGE_KEY, 'analytics')
  renderShell('/')
  expect(tabHref('Kanban')).toBe('/p/analytics')
  expect(tabHref('Agents')).toBe('/p/analytics/agents')
})

test('без запомненного проекта табы ведут на первый проект из списка', async () => {
  renderShell('/')
  await waitFor(() => expect(tabHref('Kanban')).toBe('/p/billing'))
  expect(tabHref('Agents')).toBe('/p/billing/agents')
})

test('внутри проекта табы ведут на текущий проект и он запоминается', async () => {
  window.localStorage.setItem(LAST_PROJECT_STORAGE_KEY, 'analytics')
  renderShell('/p/billing/agents')
  expect(tabHref('Kanban')).toBe('/p/billing')
  expect(tabHref('Agents')).toBe('/p/billing/agents')
  await waitFor(() =>
    expect(window.localStorage.getItem(LAST_PROJECT_STORAGE_KEY)).toBe('billing'),
  )
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
