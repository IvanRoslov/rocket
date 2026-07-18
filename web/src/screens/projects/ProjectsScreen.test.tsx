// Covers the Projects grid against the msw fixtures (docs/design/Projects.dc.html):
// card contents (mono id badge, main+linked repo line, stat badges from
// tasks{}/live_sessions), the dashed "create project" card, the "New project"
// button routing to /projects/new, and the empty-state fallback when the
// project list is empty.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import { MemoryRouter } from 'react-router-dom'
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest'
import { handlers } from '../../mocks/handlers'
import { ProjectsScreen } from './ProjectsScreen'

const server = setupServer(...handlers)

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => server.resetHandlers())
afterAll(() => server.close())

function renderScreen() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/']}>
        <ProjectsScreen />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('ProjectsScreen', () => {
  it('renders a project card with mono id, repo line, and stat badges', async () => {
    renderScreen()

    const heading = await screen.findByText('Billing')
    const card = heading.closest('a')!
    expect(card).toHaveAttribute('href', '/p/billing')

    expect(screen.getByText('billing')).toBeInTheDocument()
    expect(screen.getByText('⌂ api')).toBeInTheDocument()
    expect(screen.getByText('web, infra')).toBeInTheDocument()

    expect(screen.getByText('1 in progress')).toBeInTheDocument()
    expect(screen.getByText('1 review')).toBeInTheDocument()
    expect(screen.getByText('● 3 live')).toBeInTheDocument()
  })

  it('renders the dashed "Create project" card and the header "New project" button, both linking to /projects/new', async () => {
    renderScreen()

    await screen.findByText('Billing')

    const createCard = screen.getByText('Create project').closest('a')
    expect(createCard).toHaveAttribute('href', '/projects/new')

    const newProjectButton = screen.getByRole('link', { name: /New project/ })
    expect(newProjectButton).toHaveAttribute('href', '/projects/new')
  })

  it('shows an idle badge when a project has no active tasks or live sessions', async () => {
    server.use(
      http.get('/v1/projects', () =>
        HttpResponse.json([
          {
            id: 'platform',
            name: 'Infra Platform',
            main: 'infra',
            linked: [],
            live_sessions: 0,
            tasks: { backlog: 0, in_progress: 0, review: 0, done: 0 },
            created_at: 1_800_000_000 - 2 * 24 * 60 * 60,
          },
        ]),
      ),
      http.get('/v1/sessions', () => HttpResponse.json([])),
    )

    renderScreen()

    await screen.findByText('Infra Platform')
    expect(screen.getByText('idle')).toBeInTheDocument()
    expect(screen.getByText('⌂ infra')).toBeInTheDocument()
    // No linked repos: no "+" separator line rendered.
    expect(screen.queryByText('web, infra')).not.toBeInTheDocument()
  })

  it('shows the "awaiting you" signal only when awaiting_questions is set', async () => {
    server.use(
      http.get('/v1/projects', () =>
        HttpResponse.json([
          {
            id: 'billing',
            name: 'Billing',
            main: 'api',
            linked: ['web', 'infra'],
            live_sessions: 3,
            tasks: { backlog: 1, in_progress: 1, review: 1, done: 1 },
            created_at: 1_800_000_000 - 150 * 24 * 60 * 60,
            awaiting_questions: 1,
          },
        ]),
      ),
    )

    renderScreen()

    await screen.findByText('Billing')
    expect(screen.getByText('? awaiting you')).toBeInTheDocument()
  })

  it('renders an empty state with a "Create project" action when there are no projects', async () => {
    server.use(
      http.get('/v1/projects', () => HttpResponse.json([])),
      http.get('/v1/sessions', () => HttpResponse.json([])),
    )

    renderScreen()

    expect(await screen.findByText(/No projects yet/i)).toBeInTheDocument()
    const action = screen.getByRole('link', { name: /Create project/ })
    expect(action).toHaveAttribute('href', '/projects/new')
  })
})
