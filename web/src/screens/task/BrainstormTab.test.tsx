// The Brainstorm tab (task #4901 spec §3.1): counters, the problem, the storm
// questions in order and the exit gate. Fixture task #17 "Metering rewrite" is
// in brainstorm with a problem doc, two storm questions (17/Q1 answered from
// the terminal, 17/Q2 open), spec v1/v2, plan v1, gate v1 "changes" and gate
// v2 pending.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import { Link, MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { handlers, resetDocs, resetQuestions, resetSessions, resetTasks } from '../../mocks/handlers'
import { TaskScreen } from './TaskScreen'

vi.mock('../../components/TermPanel', () => ({
  TermPanel: () => <div data-testid="term-panel-stub" />,
}))

const server = setupServer(...handlers)

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => {
  server.resetHandlers()
  resetTasks()
  resetQuestions()
  resetSessions()
  resetDocs()
})
afterAll(() => server.close())

function renderTask(taskId = 17, search = '') {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[`/p/billing/tasks/${taskId}${search}`]}>
        <Routes>
          <Route path="/p/:projectId/tasks/:taskId" element={<TaskScreen />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

async function openStorm() {
  renderTask()
  expect(await screen.findByText('Metering rewrite')).toBeInTheDocument()
  return (await screen.findByRole('region', { name: 'Brainstorm' })) as HTMLElement
}

function counter(name: string): string {
  const item = screen.getByText(name, { selector: '.brainstorm-tab__counter-label' }).closest(
    '.brainstorm-tab__counter',
  ) as HTMLElement
  return within(item).getByTestId('value').textContent ?? ''
}

describe('Brainstorm tab — default', () => {
  it('opens by default while the task is in brainstorm', async () => {
    renderTask()
    expect(await screen.findByText('Metering rewrite')).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: /Brainstorm/ })).toHaveAttribute('aria-selected', 'true')
  })

  it('leaves other tasks on Overview, with the tab still available', async () => {
    renderTask(12)
    expect(await screen.findByText('Billing v2')).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: /Overview/ })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('tab', { name: /Brainstorm/ })).toHaveAttribute('aria-selected', 'false')
  })

  it('honours ?tab=brainstorm from a metric link', async () => {
    renderTask(12, '?tab=brainstorm')
    expect(await screen.findByText('Billing v2')).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: /Brainstorm/ })).toHaveAttribute('aria-selected', 'true')
  })

  // A subtask/parent link keeps the same TaskScreen instance: the next task
  // gets its own default, not the tab picked on the previous one.
  it('re-picks the default when the screen moves to another task', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/p/billing/tasks/12']}>
          <Link to="/p/billing/tasks/17">go to 17</Link>
          <Routes>
            <Route path="/p/:projectId/tasks/:taskId" element={<TaskScreen />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
    expect(await screen.findByText('Billing v2')).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: /Overview/ })).toHaveAttribute('aria-selected', 'true')

    await userEvent.click(screen.getByRole('link', { name: 'go to 17' }))
    expect(await screen.findByText('Metering rewrite')).toBeInTheDocument()
    await waitFor(() =>
      expect(screen.getByRole('tab', { name: /Brainstorm/ })).toHaveAttribute('aria-selected', 'true'),
    )
  })
})

describe('Brainstorm tab — content', () => {
  it('shows the storm counters', async () => {
    await openStorm()
    await waitFor(() => expect(counter('Questions')).toBe('2'))
    expect(counter('Accepted')).toBe('1 (with comment 1)')
    expect(counter('Corrected')).toBe('0')
    expect(counter('Wrong turn')).toBe('0')
    expect(counter('Spec changes')).toBe('1')
  })

  it('shows dashes when the counters cannot be read', async () => {
    server.use(
      http.get('/v1/tasks/:id/brainstorm/stats', () =>
        HttpResponse.json({ error: { code: 'internal_error', message: 'boom' } }, { status: 500 }),
      ),
    )
    await openStorm()
    await waitFor(() => expect(counter('Questions')).toBe('—'))
  })

  it('shows the latest problem doc, or says it is not written yet', async () => {
    const tab = await openStorm()
    expect(await within(tab).findByText(/pay for idle seats/)).toBeInTheDocument()

    server.use(http.get('/v1/tasks/:id/docs', () => HttpResponse.json({ docs: [] })))
    renderTask()
    expect(await screen.findAllByText('Problem not written yet')).not.toHaveLength(0)
  })

  it('lists only storm questions, in asked order, with ★ and outcomes', async () => {
    const tab = await openStorm()
    const titles = await within(tab).findAllByText(/Bill per seat or per event\?|Where do usage events land\?/)
    expect(titles.map((t) => t.textContent)).toEqual([
      'Bill per seat or per event?',
      'Where do usage events land?',
    ])
    expect(within(tab).getByRole('button', { name: /Ledger table in Postgres — recommended/ })).toBeInTheDocument()
    expect(within(tab).getByText('From terminal')).toBeInTheDocument()
    expect(within(tab).getByText('Accepted with comment', { selector: '.brainstorm-result__outcome' })).toBeInTheDocument()
  })
})

describe('Brainstorm tab — exit gate', () => {
  it('offers the pending gate with links to its spec and plan versions', async () => {
    const tab = await openStorm()
    const gate = (await within(tab).findByRole('region', { name: 'Storm exit' })) as HTMLElement
    await userEvent.click(within(gate).getByRole('button', { name: 'Spec v2' }))
    expect(within(gate).getByText(/Per-event billing, seats only as a floor/)).toBeInTheDocument()
    await userEvent.click(within(gate).getByRole('button', { name: 'Plan v1' }))
    expect(within(gate).getByText(/Event ledger/)).toBeInTheDocument()
  })

  it('Go decides the gate', async () => {
    const bodies: unknown[] = []
    server.use(
      http.post('/v1/gates/2/decide', async ({ request }) => {
        bodies.push(await request.json())
        return HttpResponse.json({ id: 2 })
      }),
    )
    const tab = await openStorm()
    const gate = (await within(tab).findByRole('region', { name: 'Storm exit' })) as HTMLElement
    await userEvent.click(within(gate).getByRole('button', { name: 'Go' }))
    await waitFor(() => expect(bodies).toEqual([{ decision: 'go', comment: '' }]))
  })

  // A Go moves the task out of brainstorm; the human must stay where they are.
  it('stays on the Brainstorm tab after Go moves the task to in progress', async () => {
    const tab = await openStorm()
    const gate = (await within(tab).findByRole('region', { name: 'Storm exit' })) as HTMLElement
    await userEvent.click(within(gate).getByRole('button', { name: 'Go' }))
    expect(await screen.findByText('In Progress')).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: /Brainstorm/ })).toHaveAttribute('aria-selected', 'true')
  })

  it('Needs changes requires a comment', async () => {
    const bodies: unknown[] = []
    server.use(
      http.post('/v1/gates/2/decide', async ({ request }) => {
        bodies.push(await request.json())
        return HttpResponse.json({ id: 2 })
      }),
    )
    const tab = await openStorm()
    const gate = (await within(tab).findByRole('region', { name: 'Storm exit' })) as HTMLElement
    await userEvent.click(within(gate).getByRole('button', { name: 'Needs changes' }))
    const send = within(gate).getByRole('button', { name: 'Send changes' })
    expect(send).toBeDisabled()
    await userEvent.type(within(gate).getByLabelText('What to change'), 'добавь лимиты')
    await userEvent.click(send)
    await waitFor(() => expect(bodies).toEqual([{ decision: 'changes', comment: 'добавь лимиты' }]))
  })

  it('explains a 409 and refetches the gates', async () => {
    let lists = 0
    server.use(
      http.get('/v1/tasks/:id/gates', () => {
        lists++
        return HttpResponse.json({
          gates: [
            {
              id: 2, task_id: 17, spec_version: 2, plan_version: 1, status: 'pending', comment: '',
              decided_by: '', requested_by: 'orch', requested_at: 1, decided_at: null,
            },
          ],
        })
      }),
      http.post('/v1/gates/2/decide', () =>
        HttpResponse.json(
          { error: { code: 'gate_superseded', message: 'the spec changed after this gate was requested' } },
          { status: 409 },
        ),
      ),
    )
    const tab = await openStorm()
    const gate = (await within(tab).findByRole('region', { name: 'Storm exit' })) as HTMLElement
    const before = lists
    await userEvent.click(within(gate).getByRole('button', { name: 'Go' }))
    expect(await within(gate).findByRole('alert')).toHaveTextContent(/spec changed/)
    await waitFor(() => expect(lists).toBeGreaterThan(before))
  })

  it('waits for a spec when no gate was ever requested', async () => {
    server.use(http.get('/v1/tasks/:id/gates', () => HttpResponse.json({ gates: [] })))
    const tab = await openStorm()
    const gate = (await within(tab).findByRole('region', { name: 'Storm exit' })) as HTMLElement
    expect(await within(gate).findByText('Waiting for spec')).toBeInTheDocument()
    expect(within(gate).queryByRole('button', { name: 'Go' })).not.toBeInTheDocument()
  })

  it('shows the Go and the gate history once decided', async () => {
    server.use(
      http.get('/v1/tasks/:id/gates', () =>
        HttpResponse.json({
          gates: [
            {
              id: 3, task_id: 17, spec_version: 2, plan_version: 1, status: 'go', comment: '',
              decided_by: 'human', requested_by: 'orch', requested_at: 2, decided_at: 3,
            },
            {
              id: 1, task_id: 17, spec_version: 1, plan_version: null, status: 'changes',
              comment: 'seats must stay as a floor', decided_by: 'human', requested_by: 'orch',
              requested_at: 1, decided_at: 2,
            },
          ],
        }),
      ),
    )
    const tab = await openStorm()
    const gate = (await within(tab).findByRole('region', { name: 'Storm exit' })) as HTMLElement
    expect(await within(gate).findByText('Go on spec v2 · plan v1')).toBeInTheDocument()
    expect(within(gate).queryByRole('button', { name: 'Go' })).not.toBeInTheDocument()
    expect(within(gate).getByText('v1 — changes: “seats must stay as a floor”')).toBeInTheDocument()
    expect(within(gate).getByText('v2 — Go')).toBeInTheDocument()
  })
})
