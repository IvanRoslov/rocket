// Covers the Projects grid against the msw fixtures (docs/design/Projects.dc.html):
// card contents (mono id badge, main+linked repo line, stat badges derived
// client-side from GET /v1/tasks?project=<id> + live_sessions), the dashed
// "create project" card, the "New project" button routing to /projects/new,
// and the empty-state fallback when the project list is empty.

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

    // Stat badges depend on a second query (GET /v1/tasks?project=billing),
    // so wait for them rather than asserting synchronously.
    expect(await screen.findByText('1 in progress')).toBeInTheDocument()
    expect(await screen.findByText('1 review')).toBeInTheDocument()
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
            created_at: 1_800_000_000 - 2 * 24 * 60 * 60,
          },
        ]),
      ),
      http.get('/v1/sessions', () => HttpResponse.json([])),
      http.get('/v1/tasks', () => HttpResponse.json({ tasks: [] })),
    )

    renderScreen()

    await screen.findByText('Infra Platform')
    expect(await screen.findByText('idle')).toBeInTheDocument()
    expect(screen.getByText('⌂ infra')).toBeInTheDocument()
    // No linked repos: no "+" separator line rendered.
    expect(screen.queryByText('web, infra')).not.toBeInTheDocument()
  })

  it('clamps the repo line to the first two linked repos + a "+N more" suffix when a project has many linked repos', async () => {
    const linked = Array.from({ length: 24 }, (_, i) => `repo-${i + 1}`)
    server.use(
      http.get('/v1/projects', () =>
        HttpResponse.json([
          {
            id: 'platform',
            name: 'Platform',
            main: 'platform',
            linked,
            live_sessions: 0,
            created_at: 1_800_000_000 - 2 * 24 * 60 * 60,
          },
        ]),
      ),
      http.get('/v1/sessions', () => HttpResponse.json([])),
      http.get('/v1/tasks', () => HttpResponse.json({ tasks: [] })),
    )

    renderScreen()

    await screen.findByText('Platform')
    expect(screen.getByText('⌂ platform')).toBeInTheDocument()
    expect(screen.getByText('repo-1, repo-2 +22 more')).toBeInTheDocument()

    const repoLine = screen.getByText('repo-1, repo-2 +22 more').closest('.project-card__repos')!
    expect(repoLine).toHaveAttribute('title', `platform + ${linked.join(', ')}`)
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

  it('flags roles of the project that are waiting on your answer', async () => {
    renderScreen()

    // Fixture role "sre" (project billing) has awaiting_user = 1.
    expect(await screen.findByText('？1 role awaiting you')).toBeInTheDocument()
  })
})
