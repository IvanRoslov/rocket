// Covers the New Project wizard (docs/design/NewProject.dc.html): slug
// generation from the name, inline id-taken feedback, the happy path
// through the Registered repo tab all the way to `POST /v1/projects`, and
// the GitHub tab's "Connect GitHub" placeholder when no token is configured.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest'
import { handlers, resetRepos, resetSettings } from '../../mocks/handlers'
import { WizardScreen } from './WizardScreen'

const server = setupServer(...handlers)

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => {
  server.resetHandlers()
  resetSettings()
  resetRepos()
})
afterAll(() => server.close())

function renderWizard() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/projects/new']}>
        <Routes>
          <Route path="/projects/new" element={<WizardScreen />} />
          <Route path="/p/:projectId" element={<ProjectStub />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

function ProjectStub() {
  return <div>project page created</div>
}

describe('WizardScreen — Step 1 (Name)', () => {
  it('generates a slug id from the name, transliterating Cyrillic', async () => {
    const user = userEvent.setup()
    renderWizard()

    const nameInput = await screen.findByLabelText('Name')
    await user.clear(nameInput)
    await user.type(nameInput, 'Биллинг v2')

    const idInput = screen.getByLabelText('Project id') as HTMLInputElement
    expect(idInput.value).toBe('billing-v2')
  })

  it('highlights an id that is already taken by an existing project', async () => {
    const user = userEvent.setup()
    renderWizard()

    const idInput = await screen.findByLabelText('Project id')
    await user.clear(idInput)
    await user.type(idInput, 'billing')

    expect(await screen.findByText('taken')).toBeInTheDocument()
    expect(screen.queryByText('available')).not.toBeInTheDocument()

    await user.clear(idInput)
    await user.type(idInput, 'brand-new-id')
    expect(await screen.findByText('available')).toBeInTheDocument()
  })
})

describe('WizardScreen — happy path via Registered repo', () => {
  it('walks Name → Main repo (Registered) → Linked (Skip) → Review → POST /v1/projects', async () => {
    const user = userEvent.setup()

    let capturedBody: unknown = null
    server.use(
      http.post('/v1/projects', async ({ request }) => {
        capturedBody = await request.json()
        return HttpResponse.json({
          id: 'new-product',
          name: 'New Product',
          main: 'web',
          linked: [],
          live_sessions: 0,
          tasks: { backlog: 0, in_progress: 0, review: 0, done: 0 },
          created_at: 1_800_000_000,
        })
      }),
    )

    renderWizard()

    const nameInput = await screen.findByLabelText('Name')
    await user.clear(nameInput)
    await user.type(nameInput, 'New Product')

    await user.click(screen.getByRole('button', { name: /Continue/ }))

    // Step 2: Registered tab, pick "web".
    await user.click(await screen.findByRole('button', { name: 'Registered' }))
    const webItem = await screen.findByText('web')
    await user.click(webItem.closest('button')!)

    await user.click(screen.getByRole('button', { name: /Continue/ }))

    // Step 3: Skip linked repos entirely.
    await user.click(await screen.findByRole('button', { name: 'Skip' }))

    // Step 4: Review → Create.
    await user.click(await screen.findByRole('button', { name: /Create project/ }))

    await waitFor(() => expect(capturedBody).toEqual({ id: 'new-product', name: 'New Product', main: 'web', linked: [] }))
    expect(await screen.findByText('project page created')).toBeInTheDocument()
  })
})

describe('WizardScreen — GitHub tab without a token', () => {
  it('shows the "Connect GitHub" placeholder instead of a repo list (400 no_token, not 404)', async () => {
    const user = userEvent.setup()
    renderWizard()

    const nameInput = await screen.findByLabelText('Name')
    await user.type(nameInput, 'Some Project')
    await user.click(screen.getByRole('button', { name: /Continue/ }))

    // GitHub is the default active tab in Step 2.
    expect(await screen.findByText('Connect GitHub')).toBeInTheDocument()
  })
})

describe('WizardScreen — GitHub tab with a token', () => {
  async function gotoGithubTabWithToken(user: ReturnType<typeof userEvent.setup>) {
    renderWizard()
    const nameInput = await screen.findByLabelText('Name')
    await user.type(nameInput, 'Some Project')
    await user.click(screen.getByRole('button', { name: /Continue/ }))

    expect(await screen.findByText('Connect GitHub')).toBeInTheDocument()
    await user.type(screen.getByPlaceholderText('ghp_…'), 'ghp_1234567890abcdef')
    await user.click(screen.getByRole('button', { name: /^Save$/ }))
    // Once the token is saved, github-repos is invalidated and refetched.
    await screen.findByText('acme/docs')
  }

  it('picking a repo registers it via POST /v1/repos {github} and selects it', async () => {
    let capturedBody: unknown
    server.use(
      http.post('/v1/repos', async ({ request }) => {
        capturedBody = await request.json()
        return HttpResponse.json(
          { id: 'docs', path: '/home/dev/.rocket/repos/docs', default_branch: 'main', auto_cleanup: true, env: {}, symlinks: [], post_create: [], created_at: 0 },
          { status: 201 },
        )
      }),
    )
    const user = userEvent.setup()
    await gotoGithubTabWithToken(user)

    const docsItem = screen.getByText('acme/docs').closest('button')!
    await user.click(docsItem)

    await waitFor(() => expect(capturedBody).toEqual({ github: 'acme/docs' }))
    // Selection is reflected once Step2Main re-renders with the new id checked.
    await waitFor(() => expect(docsItem.className).toContain('repo-picker__item--picked'))
  })

  it('picking an already-registered repo (409 repo_exists) selects it instead of erroring', async () => {
    server.use(
      http.post('/v1/repos', () =>
        HttpResponse.json({ error: { code: 'repo_exists', message: 'repo id api already exists' } }, { status: 409 }),
      ),
    )
    const user = userEvent.setup()
    await gotoGithubTabWithToken(user)

    const apiItem = screen.getByText('acme/api').closest('button')!
    await user.click(apiItem)

    // No "Clone failed" error surfaces for repo_exists.
    await waitFor(() => expect(screen.queryByText(/clone failed/i)).not.toBeInTheDocument())
    await waitFor(() => expect(apiItem.className).toContain('repo-picker__item--picked'))
  })

  // A dot in the GitHub repo name: the server normalizes the derived id
  // (`status.page` -> `status-page`), so the wizard must carry the id the
  // server returned, not the raw repo name — otherwise POST /v1/projects
  // fails with repo_not_found.
  it('carries the server-normalized id for a repo whose name contains a dot', async () => {
    let capturedProject: unknown
    server.use(
      http.post('/v1/projects', async ({ request }) => {
        capturedProject = await request.json()
        return HttpResponse.json({
          id: 'some-project',
          name: 'Some Project',
          main: 'status-page',
          linked: [],
          live_sessions: 0,
          tasks: { backlog: 0, in_progress: 0, review: 0, done: 0 },
          created_at: 1_800_000_000,
        })
      }),
    )
    const user = userEvent.setup()
    await gotoGithubTabWithToken(user)

    const item = screen.getByText('acme/status.page').closest('button')!
    await user.click(item)
    await waitFor(() => expect(item.className).toContain('repo-picker__item--picked'))

    await user.click(screen.getByRole('button', { name: /Continue/ }))
    await user.click(await screen.findByRole('button', { name: 'Skip' }))
    await user.click(await screen.findByRole('button', { name: /Create project/ }))

    await waitFor(() =>
      expect(capturedProject).toEqual({ id: 'some-project', name: 'Some Project', main: 'status-page', linked: [] }),
    )
  })

  it('selects the normalized id when a dotted repo is already registered (409 repo_exists)', async () => {
    server.use(
      http.post('/v1/repos', () =>
        HttpResponse.json({ error: { code: 'repo_exists', message: 'repo id status-page already exists' } }, { status: 409 }),
      ),
    )
    const user = userEvent.setup()
    await gotoGithubTabWithToken(user)

    let capturedProject: unknown
    server.use(
      http.post('/v1/projects', async ({ request }) => {
        capturedProject = await request.json()
        return HttpResponse.json({
          id: 'some-project',
          name: 'Some Project',
          main: 'status-page',
          linked: [],
          live_sessions: 0,
          tasks: { backlog: 0, in_progress: 0, review: 0, done: 0 },
          created_at: 1_800_000_000,
        })
      }),
    )

    const item = screen.getByText('acme/status.page').closest('button')!
    await user.click(item)
    await waitFor(() => expect(item.className).toContain('repo-picker__item--picked'))

    await user.click(screen.getByRole('button', { name: /Continue/ }))
    await user.click(await screen.findByRole('button', { name: 'Skip' }))
    await user.click(await screen.findByRole('button', { name: /Create project/ }))

    await waitFor(() => expect((capturedProject as { main?: string } | null)?.main).toBe('status-page'))
  })

  it('clone failure (502 clone_failed) shows a sanitized error with a retry control', async () => {
    server.use(
      http.post('/v1/repos', () =>
        HttpResponse.json({ error: { code: 'clone_failed', message: 'fatal: could not read from remote' } }, { status: 502 }),
      ),
    )
    const user = userEvent.setup()
    await gotoGithubTabWithToken(user)

    await user.click(screen.getByText('acme/billing-sdk').closest('button')!)

    expect(await screen.findByText(/clone failed: fatal: could not read from remote/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument()
  })
})
