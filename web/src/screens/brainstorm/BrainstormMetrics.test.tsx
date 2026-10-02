// Brainstorm metrics screen (task #4901 spec §3.2, task #5019 spec §3.4):
// weekly share of accepted recommendations per storm skill for one chosen
// participant, and every storm in a table with who stormed and the gate.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import { MemoryRouter } from 'react-router-dom'
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest'
import { handlers } from '../../mocks/handlers'
import { BrainstormMetricsScreen } from './BrainstormMetricsScreen'

const server = setupServer(...handlers)
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => {
  server.resetHandlers()
  window.localStorage.clear()
})
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
    expect(cells).toEqual([
      '#17 Metering rewrite',
      'orchestrator-brainstorming',
      'you',
      '2',
      '1 / 1 / 0 / 0',
      '1',
      'awaiting Go (changes: 1)',
      '—',
    ])
  })

  it('shows who stormed and the gate state for every storm', async () => {
    renderScreen()
    const table = await screen.findByRole('table')
    const headers = within(table).getAllByRole('columnheader').map((h) => h.textContent)
    expect(headers).toEqual(expect.arrayContaining(['Who stormed', 'Spec changes before Go', 'Gate']))
    expect(headers).not.toContain('Spec changes')

    const cellsOf = (name: RegExp) =>
      within(within(table).getByRole('link', { name }).closest('tr') as HTMLElement)
        .getAllByRole('cell')
        .map((c) => c.textContent)
    const mixed = cellsOf(/#12 Billing v2/)
    expect(mixed[2]).toBe('you + cto')
    expect(mixed[6]).toBe('Go first try')
    const agent = cellsOf(/#18 Usage alerts/)
    expect(agent[2]).toBe('cto')
    expect(agent[5]).toBe('2')
    expect(agent[6]).toBe('Go after 2 changes')
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

describe('BrainstormMetricsScreen — participant switch', () => {
  const STORAGE_KEY = 'rocket.brainstormMetrics.answerer'

  async function participantSwitch() {
    return (await screen.findByRole('group', { name: 'Participant' })) as HTMLElement
  }

  function chart() {
    return screen.getByRole('figure', { name: /Accepted recommendations/ })
  }

  it('offers every participant with answers, the human selected by default', async () => {
    renderScreen()
    const group = await participantSwitch()
    const buttons = within(group).getAllByRole('button')
    expect(buttons.map((b) => b.textContent)).toEqual(['you', 'cto'])
    expect(within(group).getByRole('button', { name: 'you' })).toHaveAttribute('aria-pressed', 'true')
    expect(within(group).getByRole('button', { name: 'cto' })).toHaveAttribute('aria-pressed', 'false')
  })

  it('charts only the chosen participant\'s answers', async () => {
    renderScreen()
    const group = await participantSwitch()
    fireEvent.click(within(group).getByRole('button', { name: 'cto' }))

    expect(within(group).getByRole('button', { name: 'cto' })).toHaveAttribute('aria-pressed', 'true')
    // cto's W40: orchestrator 3/3 = 100%, superpowers 1/2 = 50%; cto has no W39.
    expect(within(chart()).getByLabelText('2026-W40 · orchestrator-brainstorming: 100% (3 of 3)')).toBeInTheDocument()
    expect(within(chart()).getByLabelText('2026-W40 · superpowers:brainstorming: 50% (1 of 2)')).toBeInTheDocument()
    expect(within(chart()).queryByText('2026-W39')).not.toBeInTheDocument()
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('cto')
  })

  it('selects the first participant when the human has no answers in the window', async () => {
    server.use(
      http.get('/v1/stats/brainstorm', () =>
        HttpResponse.json({
          weeks: [
            { week: '2026-W40', skill: 'unknown', answered_by: 'cto', answered: 2, accepted: 1, accepted_with_comment: 0, corrected: 1, wrong_turn: 0 },
            { week: '2026-W40', skill: 'unknown', answered_by: 'architect', answered: 1, accepted: 1, accepted_with_comment: 0, corrected: 0, wrong_turn: 0 },
          ],
          storms: [],
        }),
      ),
    )
    renderScreen()
    const group = await participantSwitch()
    expect(within(group).getAllByRole('button').map((b) => b.textContent)).toEqual(['architect', 'cto'])
    expect(within(group).getByRole('button', { name: 'architect' })).toHaveAttribute('aria-pressed', 'true')
    expect(within(chart()).getByLabelText('2026-W40 · unknown: 100% (1 of 1)')).toBeInTheDocument()
  })

  it('restores the stored choice when that participant is in the window', async () => {
    window.localStorage.setItem(STORAGE_KEY, 'cto')
    renderScreen()
    const group = await participantSwitch()
    expect(within(group).getByRole('button', { name: 'cto' })).toHaveAttribute('aria-pressed', 'true')
    expect(within(chart()).getByLabelText('2026-W40 · orchestrator-brainstorming: 100% (3 of 3)')).toBeInTheDocument()
  })

  it('falls back to the default rule when the stored participant is absent from the window', async () => {
    window.localStorage.setItem(STORAGE_KEY, 'architect')
    renderScreen()
    const group = await participantSwitch()
    expect(within(group).getByRole('button', { name: 'you' })).toHaveAttribute('aria-pressed', 'true')
    expect(within(chart()).getByLabelText('2026-W40 · orchestrator-brainstorming: 80% (4 of 5)')).toBeInTheDocument()
  })
})

describe('BrainstormMetricsScreen — older daemon', () => {
  it('reads weeks and storms without the participant fields as the human\'s', async () => {
    server.use(
      http.get('/v1/stats/brainstorm', () =>
        HttpResponse.json({
          weeks: [{ week: '2026-W40', skill: 'unknown', answered: 2, accepted: 1, accepted_with_comment: 0, corrected: 1, wrong_turn: 0 }],
          storms: [
            {
              task_id: 5, title: 'Old', project_id: 'billing', skill: 'unknown', questions: 1, answered: 1,
              accepted: 1, accepted_with_comment: 0, corrected: 0, wrong_turn: 0, spec_changes: 0, go_at: null,
            },
          ],
        }),
      ),
    )
    renderScreen()
    const group = (await screen.findByRole('group', { name: 'Participant' })) as HTMLElement
    expect(within(group).getByRole('button', { name: 'you' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByLabelText('2026-W40 · unknown: 50% (1 of 2)')).toBeInTheDocument()
    const cells = within(screen.getByRole('table')).getAllByRole('cell').map((c) => c.textContent)
    expect(cells[2]).toBe('—')
    expect(cells[6]).toBe('—')
  })
})

describe('BrainstormMetricsScreen — Go date', () => {
  it('formats the Go date in local time', async () => {
    const tz = process.env.TZ
    process.env.TZ = 'America/Los_Angeles'
    try {
      server.use(
        http.get('/v1/stats/brainstorm', () =>
          HttpResponse.json({
            weeks: [],
            storms: [
              {
                task_id: 5, title: 'Late Go', project_id: 'billing', skill: 'unknown', questions: 1, answered: 1,
                accepted: 1, accepted_with_comment: 0, corrected: 0, wrong_turn: 0, answered_by: ['human'],
                by_answerer: [], spec_changes: 0, first_try_go: true, has_gate: true,
                // 2026-10-01T03:00:00Z — still 30 September in Los Angeles.
                go_at: Date.UTC(2026, 9, 1, 3) / 1000,
              },
            ],
          }),
        ),
      )
      renderScreen()
      expect(await screen.findByText('2026-09-30')).toBeInTheDocument()
    } finally {
      process.env.TZ = tz
    }
  })
})
