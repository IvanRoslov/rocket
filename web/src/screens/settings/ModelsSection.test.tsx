// Settings › Модели (task #5026): the human's model-profile registry —
// list, global on/off switch, add/edit/delete with the effort picked from the
// agent's own levels — plus the two default profiles.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { handlers, resetModelProfiles, resetSettings } from '../../mocks/handlers'
import { ModelsSection } from './ModelsSection'

const server = setupServer(...handlers)

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => {
  server.resetHandlers()
  resetModelProfiles()
  resetSettings()
  vi.restoreAllMocks()
})
afterAll(() => server.close())

function renderSection() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={queryClient}>
      <ModelsSection />
    </QueryClientProvider>,
  )
}

function row(name: string) {
  return screen.getByRole('row', { name: new RegExp(`^${name}(\\s|$)`) })
}

function captureBodies(method: 'post' | 'patch' | 'put', path: string) {
  const bodies: unknown[] = []
  server.events.on('request:start', async ({ request }) => {
    if (request.method === method.toUpperCase() && new URL(request.url).pathname.startsWith(path)) {
      bodies.push(await request.clone().json())
    }
  })
  return bodies
}

describe('ModelsSection', () => {
  it('lists every registry profile with its agent, model, effort and description', async () => {
    renderSection()
    await screen.findByRole('row', { name: /^claude-opus\b/ })

    const opus = within(row('claude-opus'))
    expect(opus.getByText('claude-code')).toBeInTheDocument()
    expect(opus.getByText('opus')).toBeInTheDocument()
    expect(opus.getByText(/Сложные задачи/)).toBeInTheDocument()

    // codex runs the agent's default model and effort
    expect(within(row('codex')).getAllByText('по умолчанию')).toHaveLength(2)
    expect(within(row('claude-haiku')).getByRole('checkbox', { name: 'Включён' })).not.toBeChecked()
    expect(within(row('claude-opus')).getByRole('checkbox', { name: 'Включён' })).toBeChecked()
  })

  it('switching a profile off sends only {enabled:false}', async () => {
    const bodies = captureBodies('patch', '/v1/model-profiles/')
    const user = userEvent.setup()
    renderSection()
    await screen.findByRole('row', { name: /^codex\b/ })

    await user.click(within(row('codex')).getByRole('checkbox', { name: 'Включён' }))

    await waitFor(() => expect(bodies).toEqual([{ enabled: false }]))
    await waitFor(() => expect(within(row('codex')).getByRole('checkbox', { name: 'Включён' })).not.toBeChecked())
  })

  it('adds a profile, offering only the chosen agent’s effort levels', async () => {
    const bodies = captureBodies('post', '/v1/model-profiles')
    const user = userEvent.setup()
    renderSection()
    await screen.findByRole('row', { name: /^codex\b/ })

    await user.click(screen.getByRole('button', { name: 'Добавить профиль' }))
    const dialog = screen.getByRole('dialog')
    await user.type(within(dialog).getByLabelText('Имя'), 'codex-high')
    await user.selectOptions(within(dialog).getByLabelText('Агент'), 'codex')

    const effort = within(dialog).getByLabelText('Усилие')
    const levels = within(effort)
      .getAllByRole('option')
      .map((o) => o.getAttribute('value'))
    expect(levels).toEqual(['', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max', 'ultra'])

    await user.type(within(dialog).getByLabelText('Модель'), 'gpt-5')
    await user.selectOptions(effort, 'high')
    await user.type(within(dialog).getByLabelText('Для чего подходит'), 'Трудные тексты')
    await user.click(within(dialog).getByRole('button', { name: 'Сохранить' }))

    await waitFor(() =>
      expect(bodies).toEqual([
        { name: 'codex-high', agent: 'codex', model: 'gpt-5', effort: 'high', description: 'Трудные тексты' },
      ]),
    )
    expect(await screen.findByRole('row', { name: /^codex-high\b/ })).toBeInTheDocument()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('editing: switching the agent drops an effort the new agent does not have', async () => {
    const bodies = captureBodies('patch', '/v1/model-profiles/')
    const user = userEvent.setup()
    renderSection()
    await screen.findByRole('row', { name: /^codex\b/ })

    await user.click(within(row('codex')).getByRole('button', { name: 'Изменить' }))
    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByLabelText('Имя')).toHaveAttribute('readonly')

    await user.selectOptions(within(dialog).getByLabelText('Усилие'), 'ultra')
    await user.selectOptions(within(dialog).getByLabelText('Агент'), 'claude-code')
    expect(within(dialog).getByLabelText('Усилие')).toHaveValue('')

    await user.click(within(dialog).getByRole('button', { name: 'Сохранить' }))
    await waitFor(() =>
      expect(bodies).toEqual([
        {
          agent: 'claude-code',
          model: '',
          effort: '',
          description: 'Codex с моделью по умолчанию: тексты, доки, механические правки',
        },
      ]),
    )
  })

  it('shows the server error inside the editor (profile_exists)', async () => {
    const user = userEvent.setup()
    renderSection()
    await screen.findByRole('row', { name: /^codex\b/ })

    await user.click(screen.getByRole('button', { name: 'Добавить профиль' }))
    const dialog = screen.getByRole('dialog')
    await user.type(within(dialog).getByLabelText('Имя'), 'codex')
    await user.selectOptions(within(dialog).getByLabelText('Агент'), 'codex')
    await user.click(within(dialog).getByRole('button', { name: 'Сохранить' }))

    expect(await within(dialog).findByRole('alert')).toHaveTextContent('Профиль с таким именем уже есть')
  })

  it('deleting a default profile shows profile_in_use and keeps the row', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const user = userEvent.setup()
    renderSection()
    await screen.findByRole('row', { name: /^claude-opus\b/ })

    await user.click(within(row('claude-opus')).getByRole('button', { name: 'Удалить' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Профиль выбран по умолчанию — сначала смените дефолт')
    expect(row('claude-opus')).toBeInTheDocument()
  })

  it('shows the latest failure, not an older one from another action', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    server.use(
      http.patch('/v1/model-profiles/:name', () =>
        HttpResponse.json({ error: { code: 'internal_error', message: 'disk full' } }, { status: 500 }),
      ),
    )
    const user = userEvent.setup()
    renderSection()
    await screen.findByRole('row', { name: /^codex\s/ })

    await user.click(within(row('codex')).getByRole('checkbox', { name: 'Включён' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('disk full')

    await user.click(within(row('claude-opus')).getByRole('button', { name: 'Удалить' }))
    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent('Профиль выбран по умолчанию — сначала смените дефолт'),
    )
    expect(screen.getByRole('alert')).not.toHaveTextContent('disk full')
  })

  it('deletes a profile that is not a default', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const user = userEvent.setup()
    renderSection()
    await screen.findByRole('row', { name: /^codex\b/ })

    await user.click(within(row('codex')).getByRole('button', { name: 'Удалить' }))

    await waitFor(() => expect(screen.queryByRole('row', { name: /^codex\b/ })).not.toBeInTheDocument())
  })

  it('picks the default worker profile, sending only that setting', async () => {
    const bodies = captureBodies('put', '/v1/settings')
    const user = userEvent.setup()
    renderSection()
    const worker = await screen.findByLabelText('Профиль воркера по умолчанию')
    await waitFor(() => expect(worker).toHaveValue('claude-sonnet'))
    expect(screen.getByLabelText('Профиль оркестратора по умолчанию')).toHaveValue('claude-opus')
    expect(within(worker).getByRole('option', { name: 'claude-haiku — выключен' })).toBeInTheDocument()

    await user.selectOptions(worker, 'codex')

    await waitFor(() => expect(bodies).toEqual([{ default_worker_profile: 'codex' }]))
    await waitFor(() => expect(worker).toHaveValue('codex'))
  })

  it('an empty registry invites adding the first profile', async () => {
    server.use(http.get('/v1/model-profiles', () => HttpResponse.json({ profiles: [] })))
    renderSection()
    expect(await screen.findByText('Профилей нет — добавьте первый.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Добавить профиль' })).toBeInTheDocument()
  })
})
