// Role card: header signals, the wake/terminal actions and the tabs over the
// role's durable state (inbox, dossier, memory, runs, Q&A threads).

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { setupServer } from 'msw/node'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { handlers, resetAgents } from '../../mocks/handlers'
import { AgentScreen } from './AgentScreen'

// xterm needs a real canvas; the overlay's identity is all this screen owes.
vi.mock('../task/TermOverlay', () => ({
  TermOverlay: ({ session }: { session: { id: string } }) => <div>term:{session.id}</div>,
}))

const server = setupServer(...handlers)

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => {
  server.resetHandlers()
  resetAgents()
})
afterAll(() => server.close())

function renderScreen(roleId = 'sre') {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[`/p/billing/agents/${roleId}`]}>
        <Routes>
          <Route path="/p/:projectId/agents/:roleId" element={<AgentScreen />} />
          <Route path="/p/:projectId/agents" element={<div>agents list</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('AgentScreen header', () => {
  it('shows the role, its live instance and the enabled state', async () => {
    renderScreen()

    expect(await screen.findByRole('heading', { name: 'sre' })).toBeInTheDocument()
    expect(screen.getByText('● sre-run-3')).toBeInTheDocument()
    expect(screen.getByText('enabled')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Disable' })).toBeInTheDocument()
    expect(screen.getByText('/home/dev/.rocket/agents/sre/role.md')).toBeInTheDocument()
  })

  it('wakes the role with a ping and clears the field', async () => {
    renderScreen()

    const ping = await screen.findByLabelText('Ping the role')
    await userEvent.type(ping, 'status?')
    await userEvent.click(screen.getByRole('button', { name: 'Wake' }))

    await waitFor(() => expect(ping).toHaveValue(''))
  })

  it('attaches straight to the live instance from the terminal button', async () => {
    renderScreen()

    await userEvent.click(await screen.findByRole('button', { name: 'Terminal' }))

    expect(await screen.findByText('term:sre-run-3')).toBeInTheDocument()
  })

  it('offers wake-and-open when no instance is live', async () => {
    renderScreen('triage')

    const button = await screen.findByRole('button', { name: 'Wake & open terminal' })
    await userEvent.click(button)

    expect(await screen.findByText(/waking…/)).toBeInTheDocument()
    // No instance appeared, so no terminal — the pending state stays visible
    // until the wake engine spawns one (or the user cancels).
    expect(screen.queryByText(/^term:/)).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByText(/waking…/)).not.toBeInTheDocument()
  })
})

describe('AgentScreen tabs', () => {
  it('opens on the Q&A threads with the awaiting-you chip', async () => {
    renderScreen()

    expect(await screen.findByText('Should I close acme/platform#42 now?')).toBeInTheDocument()
    expect(screen.getByText('awaiting you')).toBeInTheDocument()
    expect(screen.getByText('sre asked')).toBeInTheDocument()
  })

  it('shows the inbox with human-readable event summaries', async () => {
    renderScreen()

    await userEvent.click(await screen.findByRole('tab', { name: /Inbox/ }))

    expect(await screen.findByText('billing-v2-orch: blocked by the platform migration')).toBeInTheDocument()
    expect(screen.getByText('acme/platform#42 — DB migration stuck')).toBeInTheDocument()
    expect(screen.getByText('#12 Billing v2: in_progress → review')).toBeInTheDocument()
  })

  it('filters the dossier by state and links tasks to the board', async () => {
    renderScreen()

    await userEvent.click(await screen.findByRole('tab', { name: /Dossier/ }))

    expect(await screen.findByText('acme/platform#42')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '#12' })).toHaveAttribute('href', '/p/billing/tasks/12')

    await userEvent.selectOptions(screen.getByLabelText('State'), 'deferred')

    await waitFor(() => expect(screen.queryByText('acme/platform#42')).not.toBeInTheDocument())
    expect(screen.getByText('acme/platform#43')).toBeInTheDocument()
  })

  it('lists the runs newest first with a terminal link for the live one', async () => {
    renderScreen()

    await userEvent.click(await screen.findByRole('tab', { name: /Runs/ }))

    const rows = await screen.findAllByRole('row')
    expect(within(rows[1]).getByText('sre-run-3')).toBeInTheDocument()
    expect(within(rows[1]).getByText('running')).toBeInTheDocument()
    expect(within(rows[1]).getByRole('link', { name: /term/ })).toHaveAttribute(
      'href',
      '/term/sre-run-3',
    )
    expect(within(rows[2]).getByText('sre-run-2')).toBeInTheDocument()
    expect(within(rows[2]).queryByRole('link')).toBeNull()
  })
})
