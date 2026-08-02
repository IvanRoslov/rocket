// Agents list screen: one card per registered agent with its signals.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { setupServer } from 'msw/node'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest'
import { handlers, resetAgents } from '../../mocks/handlers'
import type { Agent } from '../../lib/types'
import { AgentsScreen } from './AgentsScreen'
import { agentStats } from './AgentCard'

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
  it('lists the project agents with their description and signals', async () => {
    renderScreen()

    expect(await screen.findByText('sre')).toBeInTheDocument()
    expect(screen.getByText('triage')).toBeInTheDocument()
    expect(screen.getByText(/Platform SRE/)).toBeInTheDocument()
    expect(screen.getByText('2 unread')).toBeInTheDocument()
    expect(screen.getByText('awaiting you')).toBeInTheDocument()
    expect(screen.getByText('disabled')).toBeInTheDocument()
  })

  it('marks a live session and links to the agent card', async () => {
    renderScreen()

    expect(await screen.findByText('● live')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /sre/ })).toHaveAttribute(
      'href',
      '/p/billing/agents/sre',
    )
  })

  it('has no dossier, cron or subscription copy left', async () => {
    renderScreen()

    await screen.findByText('sre')
    expect(screen.queryByText(/dossier/i)).not.toBeInTheDocument()
    expect(screen.queryByText(/cron/i)).not.toBeInTheDocument()
    expect(screen.queryByText(/subscription/i)).not.toBeInTheDocument()
  })
})

function makeAgent(over: Partial<Agent> = {}): Agent {
  return {
    id: 'sre',
    description: '',
    project: 'billing',
    dir: '',
    command: '',
    enabled: true,
    session_alive: false,
    unread: 0,
    open_questions: 0,
    awaiting_user: 0,
    created_at: 0,
    updated_at: 0,
    ...over,
  }
}

describe('agentStats', () => {
  it('leads with what stops the agent working', () => {
    expect(agentStats(makeAgent({ enabled: false })).map((s) => s.label)).toEqual(['disabled'])
  })

  it('shows liveness, unread and the thread waiting on you', () => {
    const stats = agentStats(
      makeAgent({ session_alive: true, unread: 3, open_questions: 2, awaiting_user: 1 }),
    ).map((s) => s.label)
    expect(stats).toEqual(['● live', '3 unread', 'awaiting you'])
  })

  it('falls back to the open thread count when none awaits you', () => {
    expect(agentStats(makeAgent({ open_questions: 2 })).map((s) => s.label)).toEqual(['2 open Q'])
  })

  it('says idle when there is no signal at all', () => {
    expect(agentStats(makeAgent()).map((s) => s.label)).toEqual(['idle'])
  })
})
