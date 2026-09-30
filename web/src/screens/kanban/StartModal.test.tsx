// Start ▸ picks WHICH agent runs the orchestrator. The choice is a select
// backed by GET /v1/agent-kinds, not a free-text field the human has to
// spell correctly; agents whose executable is missing are offered but
// disabled.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
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

test('offers every registered agent, disabling the unavailable ones', async () => {
  renderModal()

  await screen.findByLabelText('Agent')
  await waitFor(() => expect(screen.getByRole('option', { name: /^codex/ })).toBeInTheDocument())

  expect(screen.getByRole('option', { name: /Default \(claude-code\)/ })).toBeInTheDocument()
  expect(screen.getByRole('option', { name: 'claude-code' })).not.toBeDisabled()
  // fixtures mark codex as unavailable (no binary on this machine)
  expect(screen.getByRole('option', { name: /^codex/ })).toBeDisabled()
})

test('sends the picked agent to POST /v1/tasks/{id}/start', async () => {
  const seen = vi.fn()
  server.use(
    http.post('/v1/tasks/:id/start', async ({ request }) => {
      seen(await request.json())
      return HttpResponse.json({ task_id: 10, feature_slug: 'f', session_id: 's' }, { status: 201 })
    }),
  )
  renderModal()

  const select = await screen.findByLabelText('Agent')
  await waitFor(() => expect(screen.getByRole('option', { name: 'claude-code' })).toBeInTheDocument())
  await userEvent.selectOptions(select, 'claude-code')
  await userEvent.click(screen.getByRole('button', { name: 'Start ▸' }))

  await waitFor(() => expect(seen).toHaveBeenCalledWith({ agent: 'claude-code' }))
})

test('sends no agent when the default is kept', async () => {
  const seen = vi.fn()
  server.use(
    http.post('/v1/tasks/:id/start', async ({ request }) => {
      seen(await request.text())
      return HttpResponse.json({ task_id: 10, feature_slug: 'f', session_id: 's' }, { status: 201 })
    }),
  )
  renderModal()

  await screen.findByLabelText('Agent')
  await userEvent.click(screen.getByRole('button', { name: 'Start ▸' }))

  await waitFor(() => expect(seen).toHaveBeenCalledWith(''))
})
