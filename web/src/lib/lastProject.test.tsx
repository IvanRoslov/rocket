import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { setupServer } from 'msw/node'
import type { ReactNode } from 'react'
import { handlers } from '../mocks/handlers'
import { LAST_PROJECT_STORAGE_KEY, loadStoredProjectId, useLastProjectId } from './lastProject'

const server = setupServer(...handlers)

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => {
  server.resetHandlers()
  window.localStorage.clear()
})
afterAll(() => server.close())

function wrapper({ children }: { children: ReactNode }) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
}

test('loadStoredProjectId: пусто, пока ничего не сохранено', () => {
  expect(loadStoredProjectId()).toBeUndefined()
})

test('запоминает текущий проект в localStorage', async () => {
  renderHook(() => useLastProjectId('analytics'), { wrapper })
  await waitFor(() =>
    expect(window.localStorage.getItem(LAST_PROJECT_STORAGE_KEY)).toBe('analytics'),
  )
})

test('без :projectId в URL возвращает сохранённый проект', async () => {
  window.localStorage.setItem(LAST_PROJECT_STORAGE_KEY, 'analytics')
  const { result } = renderHook(() => useLastProjectId(undefined), { wrapper })
  expect(result.current).toBe('analytics')
})

test('без сохранённого проекта откатывается на первый из списка', async () => {
  const { result } = renderHook(() => useLastProjectId(undefined), { wrapper })
  await waitFor(() => expect(result.current).toBe('billing'))
})

test('текущий проект важнее сохранённого', () => {
  window.localStorage.setItem(LAST_PROJECT_STORAGE_KEY, 'analytics')
  const { result } = renderHook(() => useLastProjectId('billing'), { wrapper })
  expect(result.current).toBe('billing')
})
