// Task Usage tab (task #5138 spec §5): the feature total and one row per
// agent session, a sub-row per model when a session used several.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import { MemoryRouter } from 'react-router-dom'
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest'
import { taskUsage } from '../../mocks/fixtures'
import { handlers } from '../../mocks/handlers'
import { UsageTab } from './UsageTab'

const server = setupServer(...handlers)
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => server.resetHandlers())
afterAll(() => server.close())

function renderTab(taskId = 12) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <UsageTab taskId={taskId} taskPath={(id) => `/p/billing/tasks/${id}`} />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

const rowOf = (session: string) => screen.getByTestId(`usage-session-${session}`)
const cells = (row: HTMLElement) => within(row).getAllByRole('cell').map((c) => c.textContent)

describe('UsageTab', () => {
  it('shows the feature total', async () => {
    renderTab()
    const cards = await screen.findByRole('list', { name: 'Totals' })
    expect(within(cards).getByText('Tokens').closest('li')).toHaveTextContent('4.16M')
    expect(within(cards).getByText('≈ Cost').closest('li')).toHaveTextContent('$34.86 (partial)')
  })

  it('has the session columns', async () => {
    renderTab()
    const table = await screen.findByRole('table', { name: 'Sessions' })
    expect(within(table).getAllByRole('columnheader').map((h) => h.textContent)).toEqual([
      'Session', 'Subtask / PR', 'Agent / Model', 'Duration', 'Tokens', 'Cache read', '≈ $',
    ])
  })

  it('renders the orchestrator snapshot with a sub-row per model', async () => {
    renderTab()
    await screen.findByRole('table', { name: 'Sessions' })
    expect(cells(rowOf('s-billing-v2-orch'))).toEqual([
      'Orchestratorlive snapshot', '', 'claudehigh', '—', '2.48M', '25.1M', '$32.11',
    ])
    const models = screen.getAllByTestId('usage-model-s-billing-v2-orch')
    expect(models.map(cells)).toEqual([
      ['', '', 'claude-opus-5-5', '', '2.33M', '24.5M', '$31.85'],
      ['', '', 'claude-haiku-4-5', '', '145K', '600K', '$0.26'],
    ])
  })

  it('renders a worker with its subtask, PR link and single model inline', async () => {
    renderTab()
    await screen.findByRole('table', { name: 'Sessions' })
    const row = rowOf('s-billing-v2-w4')
    expect(cells(row)).toEqual([
      'Worker', '#16 Retire legacy billing cronPR #400 merged', 'codexgpt-6-sol · medium', '42m', '1.16M', '3.2M',
      '—',
    ])
    expect(within(row).getByRole('link', { name: 'PR #400' })).toHaveAttribute('href', 'https://github.com/acme/infra/pull/400')
    expect(within(row).getByRole('link', { name: '#16 Retire legacy billing cron' })).toHaveAttribute(
      'href',
      '/p/billing/tasks/16',
    )
    expect(screen.queryByTestId('usage-model-s-billing-v2-w4')).not.toBeInTheDocument()
  })

  it('labels missing transcripts, errors and running sessions', async () => {
    renderTab()
    await screen.findByRole('table', { name: 'Sessions' })
    const status = (session: string) => within(rowOf(session)).queryByTestId('usage-status')?.textContent
    const missing = cells(rowOf('s-billing-v2-w3'))
    expect(status('s-billing-v2-w3')).toBe('no transcript')
    expect(status('s-billing-v2-w4')).toBeUndefined()
    expect(missing.slice(4, 7)).toEqual(['—', '—', '—'])
    const error = within(rowOf('s-billing-v2-w6')).getByText('error')
    expect(error).toHaveAttribute('title', 'read transcript: permission denied')
    const running = cells(rowOf('s-billing-v2-w2'))
    expect(status('s-billing-v2-w2')).toBe('running — counted when finished')
    expect(running.slice(4, 7)).toEqual(['—', '—', '—'])
    const pending = cells(rowOf('s-billing-v2-w7'))
    expect(status('s-billing-v2-w7')).toBe('pending count')
    expect(pending.slice(4, 7)).toEqual(['—', '—', '—'])
  })

  it('marks a session cost that leaves out an unpriced model', async () => {
    server.use(
      http.get('/v1/tasks/:id/usage', () =>
        HttpResponse.json({
          ...taskUsage,
          sessions: taskUsage.sessions.map((s) =>
            s.session_id === 's-billing-v2-orch' ? { ...s, cost_usd: 31.85, cost_partial: true } : s,
          ),
        }),
      ),
    )
    renderTab()
    await screen.findByRole('table', { name: 'Sessions' })
    expect(cells(rowOf('s-billing-v2-orch'))[6]).toBe('$31.85 (partial)')
  })

  it('shows an empty state for a feature with no sessions', async () => {
    renderTab(17)
    expect(await screen.findByText('No agent sessions yet')).toBeInTheDocument()
  })

  it('says so when the daemon has no usage endpoint', async () => {
    server.use(
      http.get('/v1/tasks/:id/usage', () => HttpResponse.json({ error: { code: 'not_found', message: 'nope' } }, { status: 404 })),
    )
    renderTab()
    expect(await screen.findByRole('alert')).toHaveTextContent(/Could not load usage/)
  })
})
