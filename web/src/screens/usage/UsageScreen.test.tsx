// Usage screen (task #5138 spec §5): period presets and a custom range kept
// in the query string, a project filter, total cards, By model and By task.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { usageStats } from '../../mocks/fixtures'
import { handlers } from '../../mocks/handlers'
import { UsageScreen } from './UsageScreen'

const server = setupServer(...handlers)
let requests: URL[] = []
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
beforeEach(() => {
  requests = []
  server.events.on('request:start', ({ request }) => {
    const url = new URL(request.url)
    if (url.pathname === '/v1/stats/usage') requests.push(url)
  })
  // Only Date is faked: "today" is 2026-10-03 while timers and fetch run for real.
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 9, 3, 12))
})
afterEach(() => {
  server.resetHandlers()
  server.events.removeAllListeners()
  vi.useRealTimers()
})
afterAll(() => server.close())

function Location() {
  const loc = useLocation()
  return <div data-testid="location">{loc.pathname + loc.search}</div>
}

function renderScreen(path = '/usage') {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/usage" element={<UsageScreen />} />
          <Route path="*" element={null} />
        </Routes>
        <Location />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

const lastRequest = () => requests[requests.length - 1]
const location = () => screen.getByTestId('location').textContent

describe('UsageScreen — period', () => {
  it('defaults to the last 30 days', async () => {
    renderScreen()
    await screen.findByRole('table', { name: 'By model' })
    expect(lastRequest().searchParams.get('from')).toBe('2026-09-04')
    expect(lastRequest().searchParams.get('to')).toBe('2026-10-03')
    const period = screen.getByRole('group', { name: 'Period' })
    expect(within(period).getByRole('button', { name: '30d' })).toHaveAttribute('aria-pressed', 'true')
  })

  it('a preset changes from/to in the request and in the URL', async () => {
    renderScreen()
    await screen.findByRole('table', { name: 'By model' })
    fireEvent.click(within(screen.getByRole('group', { name: 'Period' })).getByRole('button', { name: '7d' }))
    await waitFor(() => expect(lastRequest().searchParams.get('from')).toBe('2026-09-27'))
    expect(lastRequest().searchParams.get('to')).toBe('2026-10-03')
    expect(location()).toBe('/usage?from=2026-09-27&to=2026-10-03')
    expect(within(screen.getByRole('group', { name: 'Period' })).getByRole('button', { name: '7d' })).toHaveAttribute(
      'aria-pressed',
      'true',
    )
  })

  it('reads a custom range from the URL and lets the dates change it', async () => {
    renderScreen('/usage?from=2026-08-01&to=2026-08-31')
    await screen.findByRole('table', { name: 'By model' })
    expect(lastRequest().searchParams.get('from')).toBe('2026-08-01')
    const period = screen.getByRole('group', { name: 'Period' })
    for (const b of within(period).getAllByRole('button')) expect(b).toHaveAttribute('aria-pressed', 'false')
    expect(screen.getByLabelText('From')).toHaveValue('2026-08-01')

    fireEvent.change(screen.getByLabelText('To'), { target: { value: '2026-08-15' } })
    await waitFor(() => expect(lastRequest().searchParams.get('to')).toBe('2026-08-15'))
    expect(location()).toBe('/usage?from=2026-08-01&to=2026-08-15')
  })

  it('does not ask for a range that ends before it starts', async () => {
    renderScreen('/usage?from=2026-08-01&to=2026-08-31')
    await screen.findByRole('table', { name: 'By model' })
    const before = requests.length
    fireEvent.change(screen.getByLabelText('From'), { target: { value: '2026-09-10' } })
    expect(await screen.findByText(/start date on or before the end date/)).toBeInTheDocument()
    expect(requests.length).toBe(before)
  })

  it('filters by project and keeps it in the URL', async () => {
    renderScreen()
    await screen.findByRole('table', { name: 'By model' })
    const select = screen.getByLabelText('Project')
    await within(select).findByRole('option', { name: 'Analytics' })
    fireEvent.change(select, { target: { value: 'analytics' } })
    await waitFor(() => expect(lastRequest().searchParams.get('project')).toBe('analytics'))
    expect(location()).toContain('project=analytics')
    expect(await screen.findByText('No usage in this period')).toBeInTheDocument()
  })
})

describe('UsageScreen — numbers', () => {
  it('shows the totals, partial cost included', async () => {
    renderScreen()
    const cards = await screen.findByRole('list', { name: 'Totals' })
    const card = (name: string) => within(cards).getByText(name).closest('li') as HTMLElement
    expect(card('Tokens')).toHaveTextContent('4.26M')
    expect(card('Cache read')).toHaveTextContent('33.9M')
    expect(card('≈ Cost')).toHaveTextContent('$35.61 (partial)')
    expect(card('Sessions')).toHaveTextContent('8')
  })

  it('lists models with the token breakdown; a model without a price links to Settings', async () => {
    renderScreen()
    const table = await screen.findByRole('table', { name: 'By model' })
    const headers = within(table).getAllByRole('columnheader').map((h) => h.textContent)
    expect(headers).toEqual(['Model', 'Agent', 'Sessions', 'Tokens', 'Input', 'Cache write', 'Cache read', 'Output', '≈ $'])
    const rows = within(table).getAllByRole('row').slice(1)
    expect(within(rows[0]).getAllByRole('cell').map((c) => c.textContent)).toEqual([
      'claude-opus-5-5', 'claude', '4', '2.95M', '162K', '2.28M', '30.1M', '514K', '$35.35',
    ])
    const sol = within(rows[1]).getAllByRole('cell')
    expect(sol[0]).toHaveTextContent('gpt-6-sol')
    expect(sol[8]).toHaveTextContent('—')
    expect(within(sol[8]).getByRole('link', { name: 'Set price' })).toHaveAttribute('href', '/settings?section=prices')
  })

  it('lists tasks with a link to the task Usage tab and a No task row', async () => {
    renderScreen()
    const table = await screen.findByRole('table', { name: 'By task' })
    expect(within(table).getAllByRole('columnheader').map((h) => h.textContent)).toEqual([
      'Task', 'Project', 'Status', 'Sessions', 'Tokens', '≈ $',
    ])
    expect(within(table).getByRole('link', { name: '#12 Billing v2' })).toHaveAttribute(
      'href',
      '/p/billing/tasks/12?tab=usage',
    )
    const billing = within(table).getByRole('link', { name: '#12 Billing v2' }).closest('tr') as HTMLElement
    expect(within(billing).getAllByRole('cell').map((c) => c.textContent)).toEqual([
      '#12 Billing v2', 'billing', 'In Progress', '6', '4.16M', '$34.86 (partial)',
    ])
    const none = within(table).getByText('No task').closest('tr') as HTMLElement
    expect(within(none).queryByRole('link')).not.toBeInTheDocument()
  })

  it('shows a banner while the history is being counted', async () => {
    server.use(http.get('/v1/stats/usage', () => HttpResponse.json({ ...usageStats, pending: 120 })))
    renderScreen()
    expect(await screen.findByRole('status')).toHaveTextContent('Counting history… 120 sessions left')
  })

  it('shows an empty state for a period without usage', async () => {
    server.use(
      http.get('/v1/stats/usage', () =>
        HttpResponse.json({
          ...usageStats,
          models: [],
          tasks: [],
          totals: { ...usageStats.totals, sessions: 0, cost_usd: 0, cost_partial: false },
        }),
      ),
    )
    renderScreen()
    expect(await screen.findByText('No usage in this period')).toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('says so when the daemon has no usage endpoint', async () => {
    server.use(
      http.get('/v1/stats/usage', () => HttpResponse.json({ error: { code: 'not_found', message: 'nope' } }, { status: 404 })),
    )
    renderScreen()
    expect(await screen.findByRole('alert')).toHaveTextContent(/Could not load usage/)
  })
})
