// Memory tab (MEMORY.md + fact files, editable) and the role Q&A tab.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { setupServer } from 'msw/node'
import { MemoryRouter } from 'react-router-dom'
import type { ReactNode } from 'react'
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest'
import { handlers, resetAgents } from '../../mocks/handlers'
import { AgentQuestionsTab } from './AgentQuestionsTab'
import { MemoryTab } from './MemoryTab'

const server = setupServer(...handlers)

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => {
  server.resetHandlers()
  resetAgents()
})
afterAll(() => server.close())

function renderTab(ui: ReactNode) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('MemoryTab', () => {
  it('renders the index, the fact files and the memory dir path', async () => {
    renderTab(<MemoryTab roleId="sre" />)

    expect(await screen.findByText(/how the platform deploys/)).toBeInTheDocument()
    expect(screen.getByText('platform.md')).toBeInTheDocument()
    expect(screen.getByText('how it deploys')).toBeInTheDocument()
    expect(screen.getByText('/home/dev/.rocket/agents/sre/memory')).toBeInTheDocument()
  })

  it('saves an edited index', async () => {
    renderTab(<MemoryTab roleId="sre" />)

    await userEvent.click((await screen.findAllByRole('button', { name: 'Edit' }))[0])
    const box = screen.getByLabelText('Memory index')
    await userEvent.clear(box)
    // `[` is a userEvent keyboard descriptor, so keep the typed index plain.
    await userEvent.type(box, '- a fresh memory index')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(screen.queryByLabelText('Memory index')).not.toBeInTheDocument())
    expect(await screen.findByText('a fresh memory index')).toBeInTheDocument()
  })

  it('saves an edited fact file', async () => {
    renderTab(<MemoryTab roleId="sre" />)

    await userEvent.click((await screen.findAllByRole('button', { name: 'Edit' }))[1])
    const box = screen.getByLabelText('Edit platform.md')
    await userEvent.clear(box)
    await userEvent.type(box, 'now it deploys twice')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))

    expect(await screen.findByText('now it deploys twice')).toBeInTheDocument()
  })

  it('rejects a fact file name the daemon would refuse', async () => {
    renderTab(<MemoryTab roleId="sre" />)

    await userEvent.type(await screen.findByLabelText('New fact file'), '../role.md')
    await userEvent.click(screen.getByRole('button', { name: 'Create' }))

    expect(await screen.findByText(/plain \.md name/)).toBeInTheDocument()
  })
})

describe('AgentQuestionsTab', () => {
  it('renders the open role thread with its turn and asker labels', async () => {
    renderTab(<AgentQuestionsTab roleId="sre" />)

    expect(await screen.findByText('Should I close acme/platform#42 now?')).toBeInTheDocument()
    expect(screen.getByText('awaiting you')).toBeInTheDocument()
    expect(screen.getByText('sre asked')).toBeInTheDocument()
  })

  it('keeps resolved threads collapsed until asked for', async () => {
    renderTab(<AgentQuestionsTab roleId="sre" />)

    const toggle = await screen.findByRole('button', { name: /Resolved \(1\)/ })
    expect(screen.queryByText('What is blocking acme/platform#43?')).not.toBeInTheDocument()

    await userEvent.click(toggle)

    expect(screen.getByText('What is blocking acme/platform#43?')).toBeInTheDocument()
    expect(screen.getByText('you asked sre')).toBeInTheDocument()
  })

  it('opens a new thread to the role and clears the composer', async () => {
    renderTab(<AgentQuestionsTab roleId="sre" />)

    const box = await screen.findByLabelText('Ask the role')
    await userEvent.type(box, 'what is blocking #42?')
    await userEvent.click(screen.getByRole('button', { name: 'Ask' }))

    await waitFor(() => expect(box).toHaveValue(''))
  })

  it('replies into an open thread', async () => {
    renderTab(<AgentQuestionsTab roleId="sre" />)

    const box = await screen.findByLabelText('Reply to Q1')
    await userEvent.type(box, 'not yet, wait for the team')
    await userEvent.click(screen.getByRole('button', { name: /Clarify/ }))

    await waitFor(() => expect(box).toHaveValue(''))
  })
})
