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
import { handlers, resetSettings } from '../../mocks/handlers'
import { WizardScreen } from './WizardScreen'

const server = setupServer(...handlers)

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => {
  server.resetHandlers()
  resetSettings()
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
  it('shows the "Connect GitHub" placeholder instead of a repo list', async () => {
    const user = userEvent.setup()
    renderWizard()

    const nameInput = await screen.findByLabelText('Name')
    await user.type(nameInput, 'Some Project')
    await user.click(screen.getByRole('button', { name: /Continue/ }))

    // GitHub is the default active tab in Step 2.
    expect(await screen.findByText('Connect GitHub')).toBeInTheDocument()
  })
})
