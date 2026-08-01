// Create/edit role form: the subscription mini-syntax and the create flow.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { setupServer } from 'msw/node'
import { MemoryRouter } from 'react-router-dom'
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { handlers, resetAgents } from '../../mocks/handlers'
import { AgentFormModal, formatSubscriptions, parseSubscriptions } from './AgentFormModal'

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

describe('parseSubscriptions', () => {
  it('parses one repo per line with optional labels and mention-only', () => {
    expect(parseSubscriptions('acme/platform label=bug,ops mention-only\nacme/web')).toEqual([
      { repo: 'acme/platform', labels: ['bug', 'ops'], mention_only: true },
      { repo: 'acme/web', labels: [], mention_only: false },
    ])
  })

  it('ignores blank lines and surrounding whitespace', () => {
    expect(parseSubscriptions('  \n acme/web \n')).toEqual([
      { repo: 'acme/web', labels: [], mention_only: false },
    ])
  })

  it('round-trips through formatSubscriptions', () => {
    const text = 'acme/platform label=bug mention-only\nacme/web'
    expect(formatSubscriptions(parseSubscriptions(text))).toBe(text)
  })
})

describe('AgentFormModal', () => {
  it('creates a role and reports its id', async () => {
    const onCreated = vi.fn()
    renderForm({ onCreated })

    await userEvent.type(screen.getByLabelText('Role id'), 'ops')
    await userEvent.type(screen.getByLabelText('Role prompt'), '# Ops')
    await userEvent.click(screen.getByRole('button', { name: 'Create role' }))

    await waitFor(() => expect(onCreated).toHaveBeenCalledWith('ops'))
  })

  it('rejects an id that is not [a-z0-9-]', async () => {
    renderForm()

    await userEvent.type(screen.getByLabelText('Role id'), 'Ops Team')

    expect(screen.getByText('Use lowercase letters, digits and dashes')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Create role' })).toBeDisabled()
  })

  it('surfaces a duplicate id from the daemon', async () => {
    renderForm()

    await userEvent.type(screen.getByLabelText('Role id'), 'sre')
    await userEvent.click(screen.getByRole('button', { name: 'Create role' }))

    expect(await screen.findByText('agent id already exists')).toBeInTheDocument()
  })

  it('previews the prompt as markdown', async () => {
    renderForm()

    await userEvent.type(screen.getByLabelText('Role prompt'), '# SRE role')
    await userEvent.click(screen.getByRole('button', { name: 'Preview' }))

    expect(screen.getByRole('heading', { name: 'SRE role' })).toBeInTheDocument()
  })

  it('prefills from an existing role and locks its id when editing', async () => {
    renderForm({
      agent: {
        id: 'sre',
        project: 'billing',
        prompt_path: '/p/role.md',
        prompt: '# SRE',
        subscriptions: [{ repo: 'acme/platform', labels: ['bug'], mention_only: true }],
        cron: '0 * * * *',
        agent: 'claude',
        enabled: true,
        inbox_queued: 0,
        items: 0,
        open_questions: 0,
        awaiting_user: 0,
        created_at: 0,
        updated_at: 0,
      },
    })

    expect(screen.getByLabelText('Role id')).toBeDisabled()
    expect(screen.getByLabelText('GitHub subscriptions')).toHaveValue(
      'acme/platform label=bug mention-only',
    )
    expect(screen.getByLabelText('Cron')).toHaveValue('0 * * * *')
    expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled()
  })
})
