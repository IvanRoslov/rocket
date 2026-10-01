// Brainstorm metrics screen (task #4901 spec §3.2): weekly share of accepted
// recommendations per storm skill, and every storm in a table.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import { MemoryRouter } from 'react-router-dom'
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest'
import { handlers } from '../../mocks/handlers'
import { BrainstormMetricsScreen } from './BrainstormMetricsScreen'

const server = setupServer(...handlers)
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => server.resetHandlers())
afterAll(() => server.close())

function renderScreen() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <BrainstormMetricsScreen />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('BrainstormMetricsScreen', () => {
  it('charts the weekly accepted share with one series per skill', async () => {
    renderScreen()
    const chart = (await screen.findByRole('figure', { name: /Accepted recommendations/ })) as HTMLElement

    const legend = within(chart).getByRole('list', { name: 'Legend' })
    expect(within(legend).getByText('orchestrator-brainstorming')).toBeInTheDocument()
    expect(within(legend).getByText('superpowers:brainstorming')).toBeInTheDocument()

    // W40: orchestrator 4/5 = 80%, superpowers 1/2 = 50%; W39 superpowers 2/4 = 50%.
    expect(within(chart).getByText('2026-W40')).toBeInTheDocument()
    expect(within(chart).getByLabelText('2026-W40 · orchestrator-brainstorming: 80% (4 of 5)')).toBeInTheDocument()
    expect(within(chart).getByLabelText('2026-W40 · superpowers:brainstorming: 50% (1 of 2)')).toBeInTheDocument()
    expect(within(chart).getByLabelText('2026-W39 · superpowers:brainstorming: 50% (2 of 4)')).toBeInTheDocument()
  })

  it('lists the storms with a link to each task Brainstorm tab', async () => {
    renderScreen()
    const table = await screen.findByRole('table')
    const row = within(table).getByRole('link', { name: /#17 Metering rewrite/ }).closest('tr') as HTMLElement
    expect(within(table).getByRole('link', { name: /#17 Metering rewrite/ })).toHaveAttribute(
      'href',
      '/p/billing/tasks/17?tab=brainstorm',
    )
    const cells = within(row).getAllByRole('cell').map((c) => c.textContent)
    expect(cells).toEqual(['#17 Metering rewrite', 'orchestrator-brainstorming', '2', '1 / 1 / 0 / 0', '1', '—'])
  })

  it('shows an empty state with no storms', async () => {
    server.use(http.get('/v1/stats/brainstorm', () => HttpResponse.json({ weeks: [], storms: [] })))
    renderScreen()
    expect(await screen.findByText('No storms yet')).toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('says so when the daemon has no metric endpoint', async () => {
    server.use(
      http.get('/v1/stats/brainstorm', () =>
        HttpResponse.json({ error: { code: 'not_found', message: 'nope' } }, { status: 404 }),
      ),
    )
    renderScreen()
    expect(await screen.findByRole('alert')).toHaveTextContent(/Could not load/)
  })
})
