// Global Agents screen: every agent regardless of project, grouped, with the
// project-less ones under «No project» — the gap the project-scoped list left.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { setupServer } from 'msw/node'
import { MemoryRouter } from 'react-router-dom'
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { handlers, resetAgents } from '../../mocks/handlers'
import type { Agent } from '../../lib/types'
import { GlobalAgentsScreen, NO_PROJECT, groupAgents } from './GlobalAgentsScreen'

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
      <MemoryRouter initialEntries={['/agents']}>
        <GlobalAgentsScreen />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('GlobalAgentsScreen', () => {
  it('lists the agents of every project plus the project-less ones', async () => {
    renderScreen()

    expect(await screen.findByText('librarian')).toBeInTheDocument()
    expect(screen.getByText('sre')).toBeInTheDocument()
    expect(screen.getByText('triage')).toBeInTheDocument()
    // Group headings name the project, and the orphans get their own section.
    expect(screen.getByRole('heading', { name: /Billing/ })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: /No project/ })).toBeInTheDocument()
  })

  it('links a project-less agent to the global card route', async () => {
    renderScreen()

    expect(await screen.findByRole('link', { name: 'librarian' })).toHaveAttribute(
      'href',
      '/agents/librarian',
    )
    expect(screen.getByRole('link', { name: 'sre' })).toHaveAttribute('href', '/agents/sre')
  })

  it('narrows the list to one project via the chips', async () => {
    renderScreen()

    await screen.findByText('librarian')
    await userEvent.click(screen.getByRole('button', { name: /No project/ }))

    expect(screen.getByText('librarian')).toBeInTheDocument()
    expect(screen.queryByText('sre')).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'All' }))
    expect(screen.getByText('sre')).toBeInTheDocument()
  })

  it('offers the terminal and chat pages for a live session only', async () => {
    renderScreen()

    await screen.findByText('librarian')
    // sre's session is alive in the fixture, librarian's is not.
    expect(screen.getByRole('link', { name: /▣ term/ })).toHaveAttribute('href', '/term/sre')
    expect(screen.getByRole('link', { name: /💬 chat/ })).toHaveAttribute('href', '/chat/sre')

    const dead = screen.getByText('librarian').closest('.agent-card') as HTMLElement
    expect(within(dead).queryByRole('link', { name: /term/ })).not.toBeInTheDocument()
    expect(within(dead).getAllByTitle(/Session is down/)).toHaveLength(2)
  })

  it('copies the attach command even for a dead session', async () => {
    const writeText = vi.fn(() => Promise.resolve())
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })

    renderScreen()
    const card = (await screen.findByText('librarian')).closest('.agent-card') as HTMLElement
    await userEvent.click(within(card).getByRole('button', { name: /attach/ }))

    expect(writeText).toHaveBeenCalledWith('rocket agent attach librarian')
    expect(await within(card).findByText('✓ copied')).toBeInTheDocument()
  })
})

function makeAgent(over: Partial<Agent> = {}): Agent {
  return {
    id: 'a',
    description: '',
    project: '',
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

describe('groupAgents', () => {
  it('orders by project name and puts the project-less group last', () => {
    const groups = groupAgents(
      [
        makeAgent({ id: 'orphan' }),
        makeAgent({ id: 'z', project: 'zeta' }),
        makeAgent({ id: 'a', project: 'alpha' }),
      ],
      new Map([
        ['zeta', 'Alpha reporting'],
        ['alpha', 'Zeta billing'],
      ]),
    )

    expect(groups.map((g) => g.key)).toEqual(['zeta', 'alpha', NO_PROJECT])
    expect(groups.map((g) => g.label)).toEqual(['Alpha reporting', 'Zeta billing', 'No project'])
  })

  it('falls back to the project id when the project is unknown and skips empty ones', () => {
    const groups = groupAgents([makeAgent({ id: 'a', project: 'ghost' })], new Map())

    expect(groups).toHaveLength(1)
    expect(groups[0]).toMatchObject({ key: 'ghost', label: 'ghost' })
  })
})
