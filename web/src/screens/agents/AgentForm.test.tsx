// Create/edit agent form: registration fields only — id, description and the
// optional launcher pair.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { setupServer } from 'msw/node'
import { MemoryRouter } from 'react-router-dom'
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { handlers, lastCreatedAgent, resetAgents } from '../../mocks/handlers'
import { AgentFormModal } from './AgentFormModal'

const server = setupServer(...handlers)

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => {
  server.resetHandlers()
  resetAgents()
})
afterAll(() => server.close())

function renderForm(props: Partial<Parameters<typeof AgentFormModal>[0]> = {}) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <AgentFormModal projectId="billing" onClose={() => {}} {...props} />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('AgentFormModal', () => {
  it('registers an agent and reports its id', async () => {
    const onCreated = vi.fn()
    renderForm({ onCreated })

    await userEvent.type(screen.getByLabelText('Agent id'), 'ops')
    await userEvent.type(screen.getByLabelText('Description'), 'On-call ops')
    await userEvent.click(screen.getByRole('button', { name: 'Register agent' }))

    await waitFor(() => expect(onCreated).toHaveBeenCalledWith('ops'))
  })

  it('rejects an id that is not [a-z0-9-]', async () => {
    renderForm()

    await userEvent.type(screen.getByLabelText('Agent id'), 'Ops Team')

    expect(screen.getByText('Use lowercase letters, digits and dashes')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Register agent' })).toBeDisabled()
  })

  it('surfaces a duplicate id from the daemon', async () => {
    renderForm()

    await userEvent.type(screen.getByLabelText('Agent id'), 'sre')
    await userEvent.click(screen.getByRole('button', { name: 'Register agent' }))

    expect(await screen.findByText('agent id already exists')).toBeInTheDocument()
  })

  it('offers no project picker inside a project', () => {
    renderForm()

    expect(screen.queryByLabelText('Project')).not.toBeInTheDocument()
  })

  it('registers a project-less agent from the global view', async () => {
    const onCreated = vi.fn()
    render(
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <MemoryRouter>
          <AgentFormModal onClose={() => {}} onCreated={onCreated} />
        </MemoryRouter>
      </QueryClientProvider>,
    )

    // «no project» is the default — an agent needs none.
    expect(screen.getByLabelText('Project')).toHaveValue('')
    await userEvent.type(screen.getByLabelText('Agent id'), 'librarian-2')
    await userEvent.click(screen.getByRole('button', { name: 'Register agent' }))

    await waitFor(() => expect(onCreated).toHaveBeenCalledWith('librarian-2'))
    expect(lastCreatedAgent()).toMatchObject({ id: 'librarian-2', project: '' })
  })

  it('can pick a project in the global view', async () => {
    render(
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <MemoryRouter>
          <AgentFormModal onClose={() => {}} />
        </MemoryRouter>
      </QueryClientProvider>,
    )

    await userEvent.type(screen.getByLabelText('Agent id'), 'ops-2')
    await waitFor(() => expect(screen.getByRole('option', { name: 'Billing' })).toBeInTheDocument())
    await userEvent.selectOptions(screen.getByLabelText('Project'), 'billing')
    await userEvent.click(screen.getByRole('button', { name: 'Register agent' }))

    await waitFor(() => expect(lastCreatedAgent()).toMatchObject({ id: 'ops-2', project: 'billing' }))
  })

  it('offers no prompt, subscription or cron field', () => {
    renderForm()

    expect(screen.queryByLabelText(/prompt/i)).not.toBeInTheDocument()
    expect(screen.queryByLabelText(/subscription/i)).not.toBeInTheDocument()
    expect(screen.queryByLabelText(/cron/i)).not.toBeInTheDocument()
  })

  it('prefills from an existing agent and locks its id when editing', () => {
    renderForm({
      agent: {
        id: 'sre',
        description: 'Platform SRE',
        project: 'billing',
        dir: '/home/dev/agents/sre',
        command: 'claude',
        enabled: true,
        session_alive: true,
        unread: 0,
        open_questions: 0,
        awaiting_user: 0,
        created_at: 0,
        updated_at: 0,
      },
    })

    expect(screen.getByLabelText('Agent id')).toBeDisabled()
    expect(screen.getByLabelText('Description')).toHaveValue('Platform SRE')
    expect(screen.getByLabelText('Directory')).toHaveValue('/home/dev/agents/sre')
    expect(screen.getByLabelText('Command')).toHaveValue('claude')
    expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled()
  })
})
