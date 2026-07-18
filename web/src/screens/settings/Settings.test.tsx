// Covers the Settings screen against the msw fixtures (docs/design/Settings.dc.html):
// sticky nav switching between GitHub/Repositories/Project/Daemon, the repo
// registry's Remove button disabled for busy repos, PATCH /v1/projects/{id}
// sending the right body on rename, and the GitHub section degrading to a
// yellow note (instead of crashing) when GET /v1/settings 404s.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import { MemoryRouter } from 'react-router-dom'
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest'
import { handlers, resetProjects, resetRepos, resetSettings } from '../../mocks/handlers'
import { SettingsScreen } from './SettingsScreen'

const server = setupServer(...handlers)

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => {
  server.resetHandlers()
  resetSettings()
  resetRepos()
  resetProjects()
})
afterAll(() => server.close())

function renderScreen() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/settings']}>
        <SettingsScreen />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

async function gotoSection(user: ReturnType<typeof userEvent.setup>, label: string) {
  const btn = await screen.findByRole('button', { name: label })
  await user.click(btn)
}

describe('SettingsScreen', () => {
  it('renders the sticky nav and defaults to the GitHub section', async () => {
    renderScreen()
    expect(await screen.findByRole('heading', { name: 'GitHub' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'GitHub' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Repositories' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Project' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Daemon' })).toBeInTheDocument()
  })

  it('GitHub section survives a 404 from GET /v1/settings with a yellow note instead of crashing', async () => {
    server.use(
      http.get('/v1/settings', () =>
        HttpResponse.json({ error: { code: 'not_found', message: 'not implemented' } }, { status: 404 }),
      ),
    )
    renderScreen()

    expect(await screen.findByRole('heading', { name: 'GitHub' })).toBeInTheDocument()
    expect(await screen.findByText(/phase/i)).toBeInTheDocument()
  })

  // The base fixtures reference every registered repo from some project
  // (api/web/infra via billing, data via analytics), so these two tests add
  // an unreferenced "sdk" repo to the registry response — matching the
  // "— unused" row in docs/design/Settings.dc.html's own fixture.
  function withUnusedRepo() {
    server.use(
      http.get('/v1/repos', () =>
        HttpResponse.json([
          { id: 'api', path: '/home/dev/repos/api', default_branch: 'main', auto_cleanup: true, env: {}, symlinks: [], post_create: [], created_at: 0 },
          { id: 'web', path: '/home/dev/repos/web', default_branch: 'main', auto_cleanup: true, env: {}, symlinks: [], post_create: [], created_at: 0 },
          { id: 'sdk', path: '/home/dev/repos/sdk', default_branch: 'main', auto_cleanup: true, env: {}, symlinks: [], post_create: [], created_at: 0 },
        ]),
      ),
    )
  }

  it('Repositories: Remove is disabled for a repo used by a project, enabled for an unused one', async () => {
    withUnusedRepo()
    const user = userEvent.setup()
    renderScreen()
    await gotoSection(user, 'Repositories')

    const apiRow = (await screen.findByText('api')).closest('[data-testid="repo-row"]') as HTMLElement
    const apiRemove = within(apiRow).getByRole('button', { name: /remove/i })
    expect(apiRemove).toBeDisabled()

    const sdkRow = screen.getByText('sdk').closest('[data-testid="repo-row"]') as HTMLElement
    const sdkRemove = within(sdkRow).getByRole('button', { name: /remove/i })
    expect(sdkRemove).toBeEnabled()
  })

  it('Repositories: used-in reflects projects.main/linked', async () => {
    withUnusedRepo()
    const user = userEvent.setup()
    renderScreen()
    await gotoSection(user, 'Repositories')

    const webRow = (await screen.findByText('web')).closest('[data-testid="repo-row"]') as HTMLElement
    expect(within(webRow).getByText(/billing/)).toBeInTheDocument()

    const sdkRow = screen.getByText('sdk').closest('[data-testid="repo-row"]') as HTMLElement
    expect(within(sdkRow).getByText(/unused/i)).toBeInTheDocument()
  })

  it('Project: renaming and saving PATCHes /v1/projects/{id} with the right body', async () => {
    let received: unknown
    server.use(
      http.patch('/v1/projects/:id', async ({ params, request }) => {
        received = { id: params.id, body: await request.json() }
        return HttpResponse.json({
          id: params.id,
          name: 'Renamed',
          main: 'api',
          linked: ['web', 'infra'],
          live_sessions: 3,
          tasks: { backlog: 1, in_progress: 1, review: 1, done: 1 },
          created_at: 0,
        })
      }),
    )

    const user = userEvent.setup()
    renderScreen()
    await gotoSection(user, 'Project')

    const select = await screen.findByLabelText(/project/i)
    await user.selectOptions(select, 'billing')

    const nameInput = await screen.findByLabelText('Name')
    await user.clear(nameInput)
    await user.type(nameInput, 'Renamed')

    const saveBtn = screen.getByRole('button', { name: /save/i })
    await user.click(saveBtn)

    await waitFor(() =>
      expect(received).toEqual({ id: 'billing', body: { name: 'Renamed' } }),
    )
  })

  it('Project: main repo chip has no remove control; linked chips do, and removing one PATCHes linked', async () => {
    let received: unknown
    server.use(
      http.patch('/v1/projects/:id', async ({ params, request }) => {
        received = { id: params.id, body: await request.json() }
        return HttpResponse.json({
          id: params.id,
          name: 'Billing',
          main: 'api',
          linked: ['infra'],
          live_sessions: 3,
          tasks: { backlog: 1, in_progress: 1, review: 1, done: 1 },
          created_at: 0,
        })
      }),
    )

    const user = userEvent.setup()
    renderScreen()
    await gotoSection(user, 'Project')

    const select = await screen.findByLabelText(/project/i)
    await user.selectOptions(select, 'billing')

    const webChip = (await screen.findByText('web')).closest('[data-testid="repo-chip"]') as HTMLElement
    const removeBtn = within(webChip).getByRole('button', { name: /remove|✕/i })
    await user.click(removeBtn)

    await waitFor(() =>
      expect(received).toEqual({ id: 'billing', body: { linked: ['infra'] } }),
    )
  })

  it('Project: Delete is disabled while the project has open tasks or live sessions', async () => {
    const user = userEvent.setup()
    renderScreen()
    await gotoSection(user, 'Project')

    const select = await screen.findByLabelText(/project/i)
    await user.selectOptions(select, 'billing')

    const deleteBtn = await screen.findByRole('button', { name: /delete project/i })
    expect(deleteBtn).toBeDisabled()

    await user.selectOptions(select, 'analytics')
    const deleteBtn2 = await screen.findByRole('button', { name: /delete project/i })
    expect(deleteBtn2).toBeEnabled()
  })

  it('Daemon: renders read-only key/value rows from GET /v1/system', async () => {
    const user = userEvent.setup()
    renderScreen()
    await gotoSection(user, 'Daemon')

    expect(await screen.findByText('127.0.0.1:7420')).toBeInTheDocument()
    expect(screen.getByText('rocketd 0.4.1')).toBeInTheDocument()
    expect(screen.getByText('/home/dev/.rocket/rocket.db')).toBeInTheDocument()
    expect(screen.getByText('/home/dev/.rocket/config.toml')).toBeInTheDocument()
  })
})
