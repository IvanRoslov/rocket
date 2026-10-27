// The "Waiting on you" list and the /brainstorm screen's tabs.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import { MemoryRouter } from 'react-router-dom'
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest'
import type { BrainstormEntry } from '../../lib/types'
import { handlers } from '../../mocks/handlers'
import { BrainstormScreen } from './BrainstormScreen'

const server = setupServer(...handlers)
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => {
  server.resetHandlers()
  window.localStorage.clear()
})
afterAll(() => server.close())

const entries: BrainstormEntry[] = [
  {
    task_id: 17,
    project_id: 'billing',
    task_title: 'Metering',
    task_status: 'brainstorm',
    questions_waiting: 2,
    questions_stale: true,
    pending_gate: null,
    waiting_since: 1_760_000_000,
  },
  {
    task_id: 21,
    project_id: 'billing',
    task_title: 'Invoices export',
    task_status: 'in_progress',
    questions_waiting: 0,
    questions_stale: false,
    pending_gate: { id: 4, spec_version: 2, requested_at: 1_760_000_500 },
    waiting_since: 1_760_000_500,
  },
  {
    task_id: 30,
    project_id: 'web',
    task_title: 'Both waits',
    task_status: 'brainstorm',
    questions_waiting: 1,
    questions_stale: false,
    pending_gate: { id: 5, spec_version: 1, requested_at: 1_760_000_900 },
    waiting_since: 1_760_000_900,
  },
]

function useEntries(list: BrainstormEntry[]) {
  server.use(http.get('/v1/brainstorms', () => HttpResponse.json({ brainstorms: list })))
}

function renderScreen(url = '/brainstorm') {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[url]}>
        <BrainstormScreen />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('BrainstormScreen waiting list', () => {
  it('lists every waiting brainstorm by default and filters by what it waits on', async () => {
    useEntries(entries)
    renderScreen()
    expect(await screen.findAllByRole('link')).toHaveLength(3)

    fireEvent.click(screen.getByRole('button', { name: 'Needs answer (2)' }))
    expect(screen.getAllByRole('link')).toHaveLength(2)

    fireEvent.click(screen.getByRole('button', { name: 'Needs Go (2)' }))
    expect(screen.getAllByRole('link')).toHaveLength(2)

    fireEvent.click(screen.getByRole('button', { name: 'All' }))
    expect(screen.getAllByRole('link')).toHaveLength(3)
  })

  it('describes what each row waits on', async () => {
    useEntries(entries)
    renderScreen()
    await screen.findByText('#17 Metering')
    expect(screen.getByText('2 questions waiting')).toBeInTheDocument()
    expect(screen.getByText('1 question waiting')).toBeInTheDocument()
    expect(screen.getByText('STALE')).toBeInTheDocument()
    expect(screen.getByText('Needs Go · spec v2')).toBeInTheDocument()
    // Only the in-progress gate is a spec change mid-work.
    expect(screen.getAllByText('spec change mid-work')).toHaveLength(1)
  })

  it('links a row to the Brainstorm tab of its task', async () => {
    useEntries(entries)
    renderScreen()
    const link = await screen.findByRole('link', { name: /#17 Metering/ })
    expect(link).toHaveAttribute('href', '/p/billing/tasks/17?tab=brainstorm')
  })

  it('says so when nothing is waiting', async () => {
    useEntries([])
    renderScreen()
    expect(await screen.findByText('No brainstorms are waiting on you')).toBeInTheDocument()
  })
})

describe('BrainstormScreen tabs', () => {
  it('opens on Waiting on you and switches to Metrics', async () => {
    useEntries(entries)
    renderScreen()
    expect(screen.getByRole('tab', { name: 'Waiting on you' })).toHaveAttribute('aria-selected', 'true')
    await screen.findByText('#17 Metering')

    fireEvent.click(screen.getByRole('tab', { name: 'Metrics' }))
    expect(await screen.findByRole('figure', { name: /Accepted recommendations/ })).toBeInTheDocument()
  })

  it('renders the metrics for ?tab=metrics', async () => {
    renderScreen('/brainstorm?tab=metrics')
    expect(screen.getByRole('tab', { name: 'Metrics' })).toHaveAttribute('aria-selected', 'true')
    expect(await screen.findByRole('figure', { name: /Accepted recommendations/ })).toBeInTheDocument()
  })
})
