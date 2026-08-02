// Agent card: header signals, the send/terminal/Start actions and the two
// tabs over what rocket actually keeps — Q&A threads and the inbox.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
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

function renderScreen(agentId = 'sre') {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[`/p/billing/agents/${agentId}`]}>
        <Routes>
          <Route path="/p/:projectId/agents/:roleId" element={<AgentScreen />} />
          <Route path="/p/:projectId/agents" element={<div>agents list</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

/** The same screen reached from the global list, where the URL carries no
 *  project — the only route a project-less agent has. */
function renderGlobal(agentId = 'librarian') {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[`/agents/${agentId}`]}>
        <Routes>
          <Route path="/agents/:roleId" element={<AgentScreen />} />
          <Route path="/agents" element={<div>global agents list</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('AgentScreen without a project in the URL', () => {
  it('renders a project-less agent and links back to the global list', async () => {
    renderGlobal()

    expect(await screen.findByRole('heading', { name: 'librarian' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '← all agents' })).toHaveAttribute('href', '/agents')
    expect(screen.getByText('No project')).toBeInTheDocument()
  })

  it('names the project of an agent that has one', async () => {
    renderGlobal('sre')

    expect(await screen.findByRole('heading', { name: 'sre' })).toBeInTheDocument()
    expect(screen.getByText('Billing')).toBeInTheDocument()
  })
})

describe('AgentScreen header', () => {
  it('shows the agent, its live session and its launcher pair', async () => {
    renderScreen()

    expect(await screen.findByRole('heading', { name: 'sre' })).toBeInTheDocument()
    expect(screen.getByText('● session live')).toBeInTheDocument()
    expect(screen.getByText('enabled')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Disable' })).toBeInTheDocument()
    expect(screen.getByText(/Platform SRE/)).toBeInTheDocument()
    expect(screen.getByText('/home/dev/agents/sre')).toBeInTheDocument()
    expect(screen.getByText('claude')).toBeInTheDocument()
  })

  it('sends a message and clears the field', async () => {
    renderScreen()

    const field = await screen.findByLabelText('Message the agent')
    await userEvent.type(field, 'status?')
    await userEvent.click(screen.getByRole('button', { name: 'Send' }))

    await waitFor(() => expect(field).toHaveValue(''))
  })

  it('attaches to the session named after the agent', async () => {
    renderScreen()

    await userEvent.click(await screen.findByRole('button', { name: 'Terminal' }))

    expect(await screen.findByText('term:sre')).toBeInTheDocument()
  })

  it('offers Stop while the session is live, and no Start', async () => {
    renderScreen()

    expect(await screen.findByRole('button', { name: 'Stop' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Start' })).not.toBeInTheDocument()
  })

  it('offers Start when the session is down, and opens the terminal once it is up', async () => {
    renderScreen('triage')

    expect(screen.queryByRole('button', { name: 'Terminal' })).not.toBeInTheDocument()
    await userEvent.click(await screen.findByRole('button', { name: 'Start' }))

    // The mock start flips session_alive; the card re-reads the agent and the
    // terminal becomes reachable.
    expect(await screen.findByRole('button', { name: 'Terminal' })).toBeInTheDocument()
  })

  it('surfaces a launcher error instead of pretending the agent started', async () => {
    server.use(
      http.post('/v1/agents/:id/start', () =>
        HttpResponse.json(
          {
            error: {
              code: 'agent_no_dir',
              message: 'agent triage has no dir: set one or create the tmux session yourself',
            },
          },
          { status: 400 },
        ),
      ),
    )
    renderScreen('triage')

    await userEvent.click(await screen.findByRole('button', { name: 'Start' }))

    expect(await screen.findByText(/has no dir/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Start' })).toBeInTheDocument()
  })
})

describe('AgentScreen tabs', () => {
  it('opens on the Q&A threads with the awaiting-you chip', async () => {
    renderScreen()

    expect(await screen.findByText('Should I close acme/platform#42 now?')).toBeInTheDocument()
    expect(screen.getByText('awaiting you')).toBeInTheDocument()
    expect(screen.getByText('sre asked')).toBeInTheDocument()
  })

  it('shows the inbox messages and filters them by status', async () => {
    renderScreen()

    await userEvent.click(await screen.findByRole('tab', { name: /Inbox/ }))

    expect(await screen.findByText('blocked by the platform migration')).toBeInTheDocument()
    expect(screen.getByText('migration is done, thanks')).toBeInTheDocument()

    await userEvent.selectOptions(screen.getByLabelText('Status'), 'unread')

    await waitFor(() =>
      expect(screen.queryByText('migration is done, thanks')).not.toBeInTheDocument(),
    )
    expect(screen.getByText('blocked by the platform migration')).toBeInTheDocument()
  })

  it('has no Dossier, Memory or Runs tab', async () => {
    renderScreen()

    await screen.findByRole('tab', { name: /Inbox/ })
    expect(screen.getAllByRole('tab').map((t) => t.textContent)).toEqual([
      'Questions1',
      'Inbox2',
    ])
  })
})
