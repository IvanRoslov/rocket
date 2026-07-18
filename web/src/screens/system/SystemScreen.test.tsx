// Covers the System screen against the msw fixtures (docs/design/System.dc.html):
// stat tiles derived from GET /v1/system + GET /v1/sessions, the orphan row
// highlighted yellow, the failed-message plate in the queue panel, worktree
// sizes, daemon info, log tail, the header "Cleanup orphans" button posting
// to /v1/system/cleanup, and the per-row kill confirm flow posting to
// /v1/sessions/{id}/kill.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import { MemoryRouter } from 'react-router-dom'
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest'
import { handlers } from '../../mocks/handlers'
import { SystemScreen } from './SystemScreen'

const server = setupServer(...handlers)

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => server.resetHandlers())
afterAll(() => server.close())

function renderScreen() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/system']}>
        <SystemScreen />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('SystemScreen', () => {
  it('renders the four stat tiles from the fixture', async () => {
    renderScreen()

    await screen.findByText('billing-v2-orch')
    expect(screen.getByText('Agents running')).toBeInTheDocument()
    expect(screen.getByText('Orphans')).toBeInTheDocument()
    expect(screen.getByText('Queue depth')).toBeInTheDocument()

    // 3 non-orphan running tmux entries in the fixture.
    const liveTile = screen.getByText('Live sessions').closest('[data-testid="stat-tile"]') as HTMLElement
    expect(within(liveTile).getByText('3')).toBeInTheDocument()

    // 1 orphan tmux entry -> highlighted.
    const orphanTile = screen.getByText('Orphans').closest('[data-testid="stat-tile"]') as HTMLElement
    expect(within(orphanTile).getByText('1')).toBeInTheDocument()
    expect(orphanTile).toHaveAttribute('data-highlight', 'true')

    // queue.queued = 2
    const queueTile = screen.getByText('Queue depth').closest('[data-testid="stat-tile"]') as HTMLElement
    expect(within(queueTile).getByText('2')).toBeInTheDocument()
  })

  it('renders the sessions table with an orphan row highlighted', async () => {
    renderScreen()

    await screen.findByText('billing-v2-orch')
    expect(screen.getByText('billing-v2-w1')).toBeInTheDocument()
    expect(screen.getByText('billing-v2-w2')).toBeInTheDocument()

    const orphanRow = screen.getByText('webhook-retries-w1').closest('[data-testid="session-row"]') as HTMLElement
    expect(orphanRow).toHaveAttribute('data-orphan', 'true')
    expect(within(orphanRow).getByText('orphan')).toBeInTheDocument()
  })

  it('renders the queue panel with a red failed-message plate', async () => {
    renderScreen()

    const plate = await screen.findByText(/recipient busy/)
    expect(plate.closest('.queue-plate')).toHaveTextContent('msg#812')
  })

  it('renders worktrees with sizes and a total, and daemon info', async () => {
    renderScreen()

    await screen.findByText('/home/dev/.rocket/worktrees/billing-v2-orch')
    expect(screen.getByText('612 MB')).toBeInTheDocument()

    expect(screen.getByText('Daemon')).toBeInTheDocument()
    expect(screen.getByText('rocketd 0.4.1')).toBeInTheDocument()
    expect(screen.getByText('127.0.0.1:7420')).toBeInTheDocument()
    expect(screen.getByText('2d 4h')).toBeInTheDocument()
  })

  it('renders the dark log tail', async () => {
    renderScreen()

    await screen.findByText(/webhook-retries-w1\s+orphan: in tmux, not in db/)
  })

  it('clicking "Cleanup orphans" posts to /v1/system/cleanup', async () => {
    let called = false
    server.use(
      http.post('/v1/system/cleanup', () => {
        called = true
        return HttpResponse.json({ killed_tmux: ['webhook-retries-w1'], removed_worktrees: [] })
      }),
    )

    const user = userEvent.setup()
    renderScreen()

    const btn = await screen.findByRole('button', { name: /Cleanup orphans/ })
    await user.click(btn)

    await waitFor(() => expect(called).toBe(true))
  })

  it('kill button opens a confirm modal, and confirming posts to /v1/sessions/{id}/kill', async () => {
    let killedId: string | undefined
    server.use(
      http.post('/v1/sessions/:id/kill', ({ params }) => {
        killedId = params.id as string
        return HttpResponse.json({ status: 'killed' })
      }),
    )

    const user = userEvent.setup()
    renderScreen()

    await screen.findByText('billing-v2-w1')
    const row = screen.getByText('billing-v2-w1').closest('[data-testid="session-row"]') as HTMLElement
    const killBtn = within(row).getByRole('button', { name: /kill/i })
    await user.click(killBtn)

    const modal = await screen.findByRole('dialog')
    const confirmBtn = within(modal).getByRole('button', { name: /confirm/i })
    await user.click(confirmBtn)

    await waitFor(() => expect(killedId).toBe('s-billing-v2-w1'))
  })

  it('cancel button in kill modal closes without calling the handler', async () => {
    let handlerCalled = false
    server.use(
      http.post('/v1/sessions/:id/kill', () => {
        handlerCalled = true
        return HttpResponse.json({ status: 'killed' })
      }),
    )

    const user = userEvent.setup()
    renderScreen()

    await screen.findByText('billing-v2-w1')
    const row = screen.getByText('billing-v2-w1').closest('[data-testid="session-row"]') as HTMLElement
    const killBtn = within(row).getByRole('button', { name: /kill/i })
    await user.click(killBtn)

    const modal = await screen.findByRole('dialog')
    const cancelBtn = within(modal).getByRole('button', { name: /cancel/i })
    await user.click(cancelBtn)

    // Wait a short time to ensure the handler is not called
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(handlerCalled).toBe(false)
  })

  it('kill modal checkbox default is unchecked', async () => {
    const user = userEvent.setup()
    renderScreen()

    await screen.findByText('billing-v2-w1')
    const row = screen.getByText('billing-v2-w1').closest('[data-testid="session-row"]') as HTMLElement
    const killBtn = within(row).getByRole('button', { name: /kill/i })
    await user.click(killBtn)

    const modal = await screen.findByRole('dialog')
    const checkbox = within(modal).getByRole('checkbox') as HTMLInputElement
    expect(checkbox.checked).toBe(false)
  })
})
