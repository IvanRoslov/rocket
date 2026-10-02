// Start ▸ picks WHICH model profile runs the orchestrator (task #5026) and,
// optionally, which profiles its workers may use. The choice is a select over
// the enabled registry profiles, defaulting to the global orchestrator
// profile; with an empty registry it falls back to the old agent picker.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { setupServer } from 'msw/node'
import { http, HttpResponse } from 'msw'
import { afterAll, afterEach, beforeAll, expect, test, vi } from 'vitest'
import { handlers } from '../../mocks/handlers'
import { StartModal } from './StartModal'

const server = setupServer(...handlers)

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => server.resetHandlers())
afterAll(() => server.close())

function renderModal(onClose = () => {}) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={queryClient}>
      <StartModal taskId={10} onClose={onClose} />
    </QueryClientProvider>,
  )
}

function captureStart() {
  const seen = vi.fn()
  server.use(
    http.post('/v1/tasks/:id/start', async ({ request }) => {
      const text = await request.text()
      seen(text ? JSON.parse(text) : null)
      return HttpResponse.json({ task_id: 10, feature_slug: 'f', session_id: 's' }, { status: 201 })
    }),
  )
  return seen
}

test('offers the enabled profiles, defaulting to the global orchestrator profile', async () => {
  renderModal()

  const select = await screen.findByLabelText('Профиль')
  await waitFor(() => expect(within(select).getByRole('option', { name: 'claude-sonnet' })).toBeInTheDocument())

  expect(within(select).getByRole('option', { name: 'По умолчанию (claude-opus)' })).toBeInTheDocument()
  expect(within(select).queryByRole('option', { name: /claude-haiku/ })).not.toBeInTheDocument()
  // codex's binary is missing on the fixture machine
  expect(within(select).getByRole('option', { name: /^codex/ })).toBeDisabled()
  // the default's description shows until something else is picked
  expect(within(screen.getByRole('note', { name: 'Выбранный профиль' })).getByText(/Сложные задачи/)).toBeInTheDocument()
})

test('shows the picked profile’s description', async () => {
  const user = userEvent.setup()
  renderModal()
  const select = await screen.findByLabelText('Профиль')
  await waitFor(() => expect(within(select).getByRole('option', { name: 'claude-sonnet' })).toBeInTheDocument())

  await user.selectOptions(select, 'claude-sonnet')

  const note = screen.getByRole('note', { name: 'Выбранный профиль' })
  expect(within(note).getByText(/Обычная разработка/)).toBeInTheDocument()
  expect(within(note).getByText('claude-code · sonnet')).toBeInTheDocument()
})

test('sends the picked profile and the worker allowlist', async () => {
  const seen = captureStart()
  const user = userEvent.setup()
  renderModal()
  const select = await screen.findByLabelText('Профиль')
  await waitFor(() => expect(within(select).getByRole('option', { name: 'claude-sonnet' })).toBeInTheDocument())

  await user.selectOptions(select, 'claude-sonnet')
  const allow = screen.getByRole('group', { name: 'Разрешённые модели для воркеров' })
  expect(within(allow).queryByRole('checkbox', { name: /claude-haiku/ })).not.toBeInTheDocument()
  await user.click(within(allow).getByRole('checkbox', { name: /claude-sonnet/ }))
  await user.click(within(allow).getByRole('checkbox', { name: /codex/ }))
  await user.click(screen.getByRole('button', { name: 'Start ▸' }))

  await waitFor(() =>
    expect(seen).toHaveBeenCalledWith({ profile: 'claude-sonnet', allowed_profiles: ['claude-sonnet', 'codex'] }),
  )
})

test('sends no body when the defaults are kept', async () => {
  const seen = captureStart()
  const user = userEvent.setup()
  renderModal()

  const select = await screen.findByLabelText('Профиль')
  await waitFor(() => expect(within(select).getByRole('option', { name: 'claude-sonnet' })).toBeInTheDocument())
  await user.click(screen.getByRole('button', { name: 'Start ▸' }))

  await waitFor(() => expect(seen).toHaveBeenCalledWith(null))
})

test('an empty registry falls back to picking the agent', async () => {
  server.use(http.get('/v1/model-profiles', () => HttpResponse.json({ profiles: [] })))
  const seen = captureStart()
  const user = userEvent.setup()
  renderModal()

  const select = await screen.findByLabelText('Агент')
  await waitFor(() => expect(within(select).getByRole('option', { name: 'claude-code' })).toBeInTheDocument())
  expect(within(select).getByRole('option', { name: /Default \(claude-code\)/ })).toBeInTheDocument()
  expect(within(select).getByRole('option', { name: /^codex/ })).toBeDisabled()
  expect(screen.queryByLabelText('Профиль')).not.toBeInTheDocument()
  expect(screen.queryByRole('group', { name: 'Разрешённые модели для воркеров' })).not.toBeInTheDocument()

  await user.selectOptions(select, 'claude-code')
  await user.click(screen.getByRole('button', { name: 'Start ▸' }))

  await waitFor(() => expect(seen).toHaveBeenCalledWith({ agent: 'claude-code' }))
})
