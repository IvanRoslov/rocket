// Agents list screen: role cards with their signals, and the liveInstance
// helper that binds a role to its running `<role>-run-<n>` session.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import { setupServer } from 'msw/node'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest'
import { handlers, resetAgents } from '../../mocks/handlers'
import type { Session } from '../../lib/types'
import { AgentsScreen } from './AgentsScreen'
import { liveInstance } from './AgentCard'

const server = setupServer(...handlers)

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => {
  server.resetHandlers()
  resetAgents()
})
afterAll(() => server.close())

function renderScreen() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/p/billing/agents']}>
        <Routes>
          <Route path="/p/:projectId/agents" element={<AgentsScreen />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('AgentsScreen', () => {
  it('lists the project roles with their signals', async () => {
    renderScreen()

    expect(await screen.findByText('sre')).toBeInTheDocument()
    expect(screen.getByText('triage')).toBeInTheDocument()
    expect(screen.getByText('2 queued')).toBeInTheDocument()
    expect(screen.getByText('2 in dossier')).toBeInTheDocument()
    expect(screen.getByText('awaiting you')).toBeInTheDocument()
    expect(screen.getByText('disabled')).toBeInTheDocument()
  })

  it('shows the live instance of a role and links to its card', async () => {
    renderScreen()

    await waitFor(() => expect(screen.getByText('● sre-run-3')).toBeInTheDocument())
    expect(screen.getByRole('link', { name: /sre/ })).toHaveAttribute(
      'href',
      '/p/billing/agents/sre',
    )
  })

  it('shows the role subscriptions and cron', async () => {
    renderScreen()

    expect(await screen.findByText(/acme\/platform/)).toBeInTheDocument()
    expect(screen.getByText(/0 \* \* \* \*/)).toBeInTheDocument()
    expect(screen.getByText('no GitHub subscriptions')).toBeInTheDocument()
  })
})

describe('liveInstance', () => {
  const sessions = [
    { id: 'sre-run-3', kind: 'agent', state: 'running' },
    { id: 'sre-run-2', kind: 'agent', state: 'done' },
    { id: 'triage-run-1', kind: 'agent', state: 'running' },
    { id: 'sre-x-run-1', kind: 'agent', state: 'running' },
  ] as unknown as Session[]

  it('matches only a live run of the exact role', () => {
    expect(liveInstance(sessions, 'sre')?.id).toBe('sre-run-3')
    expect(liveInstance(sessions, 'triage')?.id).toBe('triage-run-1')
  })

  it('does not match a prefix of another role id', () => {
    expect(liveInstance(sessions, 'sre-x')?.id).toBe('sre-x-run-1')
    expect(liveInstance(sessions, 'sr')).toBeUndefined()
  })

  it('ignores terminal runs and non-agent sessions', () => {
    expect(liveInstance([{ id: 'sre-run-2', kind: 'agent', state: 'done' }] as unknown as Session[], 'sre')).toBeUndefined()
    expect(
      liveInstance([{ id: 'sre-run-9', kind: 'worker', state: 'running' }] as unknown as Session[], 'sre'),
    ).toBeUndefined()
  })
})
