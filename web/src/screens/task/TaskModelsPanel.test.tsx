// The task screen's «Модели» panel (task #5026): which profile the
// orchestrator runs and which profiles its workers may use; the human edits
// the allowlist any time (PATCH /v1/tasks/{id} allowed_profiles), and the
// change applies to the next spawns.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest'
import { tasks } from '../../mocks/fixtures'
import { handlers, resetTasks } from '../../mocks/handlers'
import type { Task } from '../../lib/types'
import { TaskModelsPanel } from './TaskModelsPanel'

const server = setupServer(...handlers)

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => {
  server.resetHandlers()
  resetTasks()
})
afterAll(() => server.close())

const billing = tasks.find((t) => t.id === 12)!

function renderPanel(task: Task = billing) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={queryClient}>
      <TaskModelsPanel task={task} />
    </QueryClientProvider>,
  )
}

function capturePatch() {
  const bodies: unknown[] = []
  server.events.on('request:start', async ({ request }) => {
    if (request.method === 'PATCH') bodies.push(await request.clone().json())
  })
  return bodies
}

describe('TaskModelsPanel', () => {
  it('shows the orchestrator profile and the worker allowlist', async () => {
    renderPanel()
    const panel = await screen.findByRole('region', { name: 'Модели' })
    expect(within(panel).getByText('claude-opus')).toBeInTheDocument()
    const allowed = within(panel).getByRole('list', { name: 'Воркерам разрешены' })
    expect(within(allowed).getAllByRole('listitem').map((li) => li.textContent)).toEqual(['claude-sonnet', 'codex'])
  })

  it('an empty allowlist reads as every enabled profile', async () => {
    renderPanel({ ...billing, allowed_profiles: [], orchestrator_profile: '' })
    const panel = await screen.findByRole('region', { name: 'Модели' })
    expect(within(panel).getByText('все включённые профили')).toBeInTheDocument()
    expect(within(panel).getByText('—')).toBeInTheDocument()
  })

  it('edits the allowlist and PATCHes the ticked names in registry order', async () => {
    const bodies = capturePatch()
    const user = userEvent.setup()
    renderPanel()
    await user.click(await screen.findByRole('button', { name: 'Изменить список' }))

    const box = await screen.findByRole('group', { name: 'Разрешённые профили' })
    await waitFor(() => expect(within(box).getByRole('checkbox', { name: /claude-opus/ })).toBeInTheDocument())
    expect(within(box).getByRole('checkbox', { name: /claude-sonnet/ })).toBeChecked()
    await user.click(within(box).getByRole('checkbox', { name: /claude-opus/ }))
    await user.click(within(box).getByRole('checkbox', { name: /codex/ }))
    await user.click(screen.getByRole('button', { name: 'Сохранить' }))

    await waitFor(() => expect(bodies).toEqual([{ allowed_profiles: ['claude-opus', 'claude-sonnet'] }]))
    await waitFor(() => expect(screen.queryByRole('group', { name: 'Разрешённые профили' })).not.toBeInTheDocument())
  })

  it('unticking everything sends [] — every enabled profile', async () => {
    const bodies = capturePatch()
    const user = userEvent.setup()
    renderPanel()
    await user.click(await screen.findByRole('button', { name: 'Изменить список' }))
    const box = await screen.findByRole('group', { name: 'Разрешённые профили' })
    await waitFor(() => expect(within(box).getByRole('checkbox', { name: /codex/ })).toBeInTheDocument())
    await user.click(within(box).getByRole('checkbox', { name: /claude-sonnet/ }))
    await user.click(within(box).getByRole('checkbox', { name: /codex/ }))
    await user.click(screen.getByRole('button', { name: 'Сохранить' }))

    await waitFor(() => expect(bodies).toEqual([{ allowed_profiles: [] }]))
  })

  it('marks a name whose profile was deleted, and saving drops it', async () => {
    const bodies = capturePatch()
    const user = userEvent.setup()
    renderPanel({ ...billing, allowed_profiles: ['gone-profile', 'codex'] })
    const panel = await screen.findByRole('region', { name: 'Модели' })
    await waitFor(() => expect(within(panel).getByText('gone-profile')).toHaveAttribute('title', 'профиль удалён'))

    await user.click(within(panel).getByRole('button', { name: 'Изменить список' }))
    const box = await screen.findByRole('group', { name: 'Разрешённые профили' })
    expect(within(box).queryByRole('checkbox', { name: /gone-profile/ })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Сохранить' }))

    await waitFor(() => expect(bodies).toEqual([{ allowed_profiles: ['codex'] }]))
  })

  it('shows the daemon’s refusal', async () => {
    server.use(
      http.patch('/v1/tasks/:id', () =>
        HttpResponse.json({ error: { code: 'human_only', message: 'only the human sets allowed_profiles' } }, { status: 403 }),
      ),
    )
    const user = userEvent.setup()
    renderPanel()
    await user.click(await screen.findByRole('button', { name: 'Изменить список' }))
    await screen.findByRole('group', { name: 'Разрешённые профили' })
    await user.click(screen.getByRole('button', { name: 'Сохранить' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('only the human sets allowed_profiles')
  })
})
