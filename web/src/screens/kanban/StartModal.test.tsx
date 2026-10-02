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
import { handlers, resetModelProfiles, resetSettings, resetTasks } from '../../mocks/handlers'
import { StartModal } from './StartModal'

const server = setupServer(...handlers)

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => {
  server.resetHandlers()
  resetTasks()
  resetSettings()
  resetModelProfiles()
})
afterAll(() => server.close())

function renderModal(onClose = () => {}, taskId = 10) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={queryClient}>
      <StartModal taskId={taskId} onClose={onClose} />
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

  const select = await screen.findByLabelText('Profile')
  await waitFor(() => expect(within(select).getByRole('option', { name: 'claude-sonnet' })).toBeInTheDocument())

  expect(within(select).getByRole('option', { name: 'Default (claude-opus)' })).toBeInTheDocument()
  expect(within(select).queryByRole('option', { name: /claude-haiku/ })).not.toBeInTheDocument()
  // codex's binary is missing on the fixture machine
  expect(within(select).getByRole('option', { name: /^codex/ })).toBeDisabled()
  // the default's description shows until something else is picked
  expect(within(screen.getByRole('note', { name: 'Selected profile' })).getByText(/Сложные задачи/)).toBeInTheDocument()
})

test('shows the picked profile’s description', async () => {
  const user = userEvent.setup()
  renderModal()
  const select = await screen.findByLabelText('Profile')
  await waitFor(() => expect(within(select).getByRole('option', { name: 'claude-sonnet' })).toBeInTheDocument())

  await user.selectOptions(select, 'claude-sonnet')

  const note = screen.getByRole('note', { name: 'Selected profile' })
  expect(within(note).getByText(/Обычная разработка/)).toBeInTheDocument()
  expect(within(note).getByText('claude-code · sonnet')).toBeInTheDocument()
})

test('sends the picked profile and the worker allowlist', async () => {
  const seen = captureStart()
  const user = userEvent.setup()
  renderModal()
  const select = await screen.findByLabelText('Profile')
  await waitFor(() => expect(within(select).getByRole('option', { name: 'claude-sonnet' })).toBeInTheDocument())

  await user.selectOptions(select, 'claude-sonnet')
  const allow = screen.getByRole('group', { name: 'Models allowed for workers' })
  expect(within(allow).queryByRole('checkbox', { name: /claude-haiku/ })).not.toBeInTheDocument()
  await user.click(within(allow).getByRole('checkbox', { name: /claude-sonnet/ }))
  await user.click(within(allow).getByRole('checkbox', { name: /codex/ }))
  await user.click(screen.getByRole('button', { name: 'Start ▸' }))

  await waitFor(() =>
    expect(seen).toHaveBeenCalledWith({ profile: 'claude-sonnet', allowed_profiles: ['claude-sonnet', 'codex'] }),
  )
  expect(screen.getByRole('button', { name: 'Cancel' })).toBeInTheDocument()
})

test('keeping the defaults still sends the (empty) allowlist, so a stale one cannot linger', async () => {
  const seen = captureStart()
  const user = userEvent.setup()
  renderModal()

  const select = await screen.findByLabelText('Profile')
  await waitFor(() => expect(within(select).getByRole('option', { name: 'claude-sonnet' })).toBeInTheDocument())
  await user.click(screen.getByRole('button', { name: 'Start ▸' }))

  await waitFor(() => expect(seen).toHaveBeenCalledWith({ allowed_profiles: [] }))
})

test('starts from the allowlist already set on the backlog task', async () => {
  const user = userEvent.setup()
  const seen = captureStart()
  // Set before Start, e.g. from the task screen.
  server.use(
    http.get('/v1/tasks/:id', () =>
      HttpResponse.json({
        id: 10, title: 'Invoice PDF export', project_id: 'billing', status: 'backlog', created_by: 'user',
        created_at: 0, updated_at: 0, open_questions: 0, questions_awaiting_user: 0, subtasks: [],
        allowed_profiles: ['codex', 'gone-profile'], orchestrator_profile: '',
      }),
    ),
  )
  renderModal()

  const allow = await screen.findByRole('group', { name: 'Models allowed for workers' })
  await waitFor(() => expect(within(allow).getByRole('checkbox', { name: /codex/ })).toBeChecked())
  expect(within(allow).getByRole('checkbox', { name: /claude-sonnet/ })).not.toBeChecked()

  // Unticking the last one must clear the list on the server, not keep the old one.
  await user.click(within(allow).getByRole('checkbox', { name: /codex/ }))
  await user.click(screen.getByRole('button', { name: 'Start ▸' }))
  await waitFor(() => expect(seen).toHaveBeenCalledWith({ allowed_profiles: [] }))
})

test('a disabled default profile is not offered as the default; the fallback is named', async () => {
  server.use(
    http.get('/v1/model-profiles', () =>
      HttpResponse.json({
        profiles: [
          { name: 'claude-opus', agent: 'claude-code', model: 'opus', effort: '', description: 'Сложное', enabled: false, position: 0 },
          { name: 'claude-sonnet', agent: 'claude-code', model: 'sonnet', effort: '', description: 'Обычное', enabled: true, position: 1 },
        ],
      }),
    ),
  )
  renderModal()

  const select = await screen.findByLabelText('Profile')
  await waitFor(() => expect(within(select).getByRole('option', { name: 'Default (claude-sonnet)' })).toBeInTheDocument())
  expect(within(select).queryByRole('option', { name: /claude-opus/ })).not.toBeInTheDocument()
  expect(screen.getByText('Default profile claude-opus is disabled — the orchestrator gets claude-sonnet.')).toBeInTheDocument()
  expect(within(screen.getByRole('note', { name: 'Selected profile' })).getByText('Обычное')).toBeInTheDocument()
})

test('an empty registry falls back to picking the agent', async () => {
  server.use(http.get('/v1/model-profiles', () => HttpResponse.json({ profiles: [] })))
  const seen = captureStart()
  const user = userEvent.setup()
  renderModal()

  const select = await screen.findByLabelText('Agent')
  await waitFor(() => expect(within(select).getByRole('option', { name: 'claude-code' })).toBeInTheDocument())
  expect(within(select).getByRole('option', { name: 'Default (claude-code)' })).toBeInTheDocument()
  expect(within(select).getByRole('option', { name: 'codex — unavailable' })).toBeDisabled()
  expect(screen.queryByLabelText('Profile')).not.toBeInTheDocument()
  expect(screen.queryByRole('group', { name: 'Models allowed for workers' })).not.toBeInTheDocument()

  await user.selectOptions(select, 'claude-code')
  await user.click(screen.getByRole('button', { name: 'Start ▸' }))

  await waitFor(() => expect(seen).toHaveBeenCalledWith({ agent: 'claude-code' }))
})
