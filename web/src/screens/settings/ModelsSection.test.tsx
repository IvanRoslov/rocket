// Settings › Models (task #5026): the human's model-profile registry —
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
    expect(opus.getByText(/Hard tasks/)).toBeInTheDocument()

    // codex runs the agent's default model and effort
    expect(within(row('codex')).getAllByText('default')).toHaveLength(2)
    expect(within(row('claude-haiku')).getByRole('checkbox', { name: 'Enabled' })).not.toBeChecked()
    expect(within(row('claude-opus')).getByRole('checkbox', { name: 'Enabled' })).toBeChecked()
  })

  it('switching a profile off sends only {enabled:false}', async () => {
    const bodies = captureBodies('patch', '/v1/model-profiles/')
    const user = userEvent.setup()
    renderSection()
    await screen.findByRole('row', { name: /^codex\b/ })

    await user.click(within(row('codex')).getByRole('checkbox', { name: 'Enabled' }))

    await waitFor(() => expect(bodies).toEqual([{ enabled: false }]))
    await waitFor(() => expect(within(row('codex')).getByRole('checkbox', { name: 'Enabled' })).not.toBeChecked())
  })

  it('adds a profile, offering only the chosen agent’s effort levels', async () => {
    const bodies = captureBodies('post', '/v1/model-profiles')
    const user = userEvent.setup()
    renderSection()
    await screen.findByRole('row', { name: /^codex\b/ })

    await user.click(screen.getByRole('button', { name: 'Add profile' }))
    const dialog = screen.getByRole('dialog')
    await user.type(within(dialog).getByLabelText('Name'), 'codex-high')
    await user.selectOptions(within(dialog).getByLabelText('Agent'), 'codex')

    const effort = within(dialog).getByLabelText('Effort')
    const levels = within(effort)
      .getAllByRole('option')
      .map((o) => o.getAttribute('value'))
    expect(levels).toEqual(['', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max', 'ultra'])

    await user.selectOptions(within(dialog).getByLabelText('Model'), 'Other…')
    await user.type(within(dialog).getByLabelText('Custom model'), 'gpt-5')
    await user.selectOptions(effort, 'high')
    await user.type(within(dialog).getByLabelText('Good for'), 'Трудные тексты')
    await user.click(within(dialog).getByRole('button', { name: 'Save' }))

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

    await user.click(within(row('codex')).getByRole('button', { name: 'Edit' }))
    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByLabelText('Name')).toHaveAttribute('readonly')

    await user.selectOptions(within(dialog).getByLabelText('Effort'), 'ultra')
    await user.selectOptions(within(dialog).getByLabelText('Agent'), 'claude-code')
    expect(within(dialog).getByLabelText('Effort')).toHaveValue('')

    await user.click(within(dialog).getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(bodies).toEqual([
        {
          agent: 'claude-code',
          model: '',
          effort: '',
          description: 'Codex with its default model: texts, docs, mechanical edits',
        },
      ]),
    )
  })

  it('offers the agent’s catalog models in groups, then «Other…»', async () => {
    const user = userEvent.setup()
    renderSection()
    await screen.findByRole('row', { name: /^codex\b/ })

    await user.click(screen.getByRole('button', { name: 'Add profile' }))
    const dialog = screen.getByRole('dialog')
    const model = within(dialog).getByLabelText('Model')
    await waitFor(() => expect(within(model).getByRole('group', { name: 'Main' })).toBeInTheDocument())

    const main = within(within(model).getByRole('group', { name: 'Main' })).getAllByRole('option')
    expect(main.map((o) => o.getAttribute('value'))).toEqual([
      'claude-opus-5-5',
      'claude-sonnet-5-5',
      'claude-haiku-4-5-20251001',
    ])
    expect(main[0]).toHaveTextContent('Opus 5.5 — Most capable for complex work')
    const previous = within(within(model).getByRole('group', { name: 'Previous' })).getAllByRole('option')
    expect(previous.map((o) => o.getAttribute('value'))).toEqual(['claude-opus-4-6'])
    const all = within(model).getAllByRole('option')
    expect(all[0]).toHaveTextContent('default')
    expect(all[all.length - 1]).toHaveTextContent('Other…')
    expect(within(dialog).queryByLabelText('Custom model')).not.toBeInTheDocument()

    await user.selectOptions(within(dialog).getByLabelText('Agent'), 'codex')
    expect(within(model).getByRole('option', { name: 'GPT-6 Sol — Frontier agentic coding' })).toBeInTheDocument()
    expect(within(model).queryByRole('group', { name: 'Previous' })).not.toBeInTheDocument()
  })

  it('picking a catalog model offers its own efforts and fills an empty description', async () => {
    const bodies = captureBodies('post', '/v1/model-profiles')
    const user = userEvent.setup()
    renderSection()
    await screen.findByRole('row', { name: /^codex\b/ })

    await user.click(screen.getByRole('button', { name: 'Add profile' }))
    const dialog = screen.getByRole('dialog')
    await user.type(within(dialog).getByLabelText('Name'), 'opus-46')
    const model = within(dialog).getByLabelText('Model')
    await waitFor(() => expect(within(model).getByRole('group', { name: 'Previous' })).toBeInTheDocument())
    await user.selectOptions(model, 'claude-opus-4-6')

    const effort = within(dialog).getByLabelText('Effort')
    expect(within(effort).getAllByRole('option').map((o) => o.getAttribute('value'))).toEqual([
      '',
      'low',
      'medium',
      'high',
      'max',
    ])
    expect(within(effort).getByRole('option', { name: 'high — model default' })).toBeInTheDocument()
    // Opus 4.6 has no description in the catalog: its name stands in.
    expect(within(dialog).getByLabelText('Good for')).toHaveValue('Opus 4.6')

    // A description the human wrote is never overwritten.
    await user.clear(within(dialog).getByLabelText('Good for'))
    await user.type(within(dialog).getByLabelText('Good for'), 'Мой текст')
    await user.selectOptions(model, 'claude-opus-5-5')
    expect(within(dialog).getByLabelText('Good for')).toHaveValue('Мой текст')

    await user.selectOptions(effort, 'xhigh')
    await user.click(within(dialog).getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(bodies).toEqual([
        { name: 'opus-46', agent: 'claude-code', model: 'claude-opus-5-5', effort: 'xhigh', description: 'Мой текст' },
      ]),
    )
  })

  it('switching to a model without the effort clears it; Haiku disables the effort', async () => {
    const user = userEvent.setup()
    renderSection()
    await screen.findByRole('row', { name: /^codex\b/ })

    await user.click(screen.getByRole('button', { name: 'Add profile' }))
    const dialog = screen.getByRole('dialog')
    const model = within(dialog).getByLabelText('Model')
    const effort = within(dialog).getByLabelText('Effort')
    await waitFor(() => expect(within(model).getByRole('group', { name: 'Main' })).toBeInTheDocument())

    await user.selectOptions(model, 'claude-opus-5-5')
    await user.selectOptions(effort, 'xhigh')
    await user.selectOptions(model, 'claude-opus-4-6')
    expect(effort).toHaveValue('')

    await user.selectOptions(effort, 'high')
    await user.selectOptions(model, 'claude-haiku-4-5-20251001')
    expect(effort).toHaveValue('')
    expect(effort).toBeDisabled()

    await user.selectOptions(model, 'claude-sonnet-5-5')
    expect(effort).toBeEnabled()
  })

  it('switching the agent turns a model the new agent lacks into «Other…» and drops its effort', async () => {
    const user = userEvent.setup()
    renderSection()
    await screen.findByRole('row', { name: /^codex\b/ })

    await user.click(screen.getByRole('button', { name: 'Add profile' }))
    const dialog = screen.getByRole('dialog')
    const model = within(dialog).getByLabelText('Model')
    await waitFor(() => expect(within(model).getByRole('group', { name: 'Main' })).toBeInTheDocument())
    await user.selectOptions(within(dialog).getByLabelText('Agent'), 'codex')
    await user.selectOptions(model, 'gpt-6-sol')
    await user.selectOptions(within(dialog).getByLabelText('Effort'), 'ultra')

    await user.selectOptions(within(dialog).getByLabelText('Agent'), 'claude-code')
    expect(model).toHaveValue('__custom__')
    expect(within(dialog).getByLabelText('Custom model')).toHaveValue('gpt-6-sol')
    expect(within(dialog).getByLabelText('Effort')).toHaveValue('')
  })

  it('a v1 profile with an alias model opens as «Other…» with the agent’s efforts and saves unchanged', async () => {
    const bodies = captureBodies('patch', '/v1/model-profiles/')
    const user = userEvent.setup()
    renderSection()
    await screen.findByRole('row', { name: /^claude-haiku\b/ })

    await user.click(within(row('claude-haiku')).getByRole('button', { name: 'Edit' }))
    const dialog = screen.getByRole('dialog')
    await waitFor(() => expect(within(dialog).getByLabelText('Model')).toHaveValue('__custom__'))
    expect(within(dialog).getByLabelText('Custom model')).toHaveValue('haiku')
    const effort = within(dialog).getByLabelText('Effort')
    expect(effort).toHaveValue('low')
    expect(effort).toBeEnabled()
    expect(within(effort).getByRole('option', { name: 'xhigh' })).toBeInTheDocument()

    await user.click(within(dialog).getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(bodies).toEqual([
        { agent: 'claude-code', model: 'haiku', effort: 'low', description: 'Быстрые мелкие правки' },
      ]),
    )
  })

  it('without a catalog the form still takes a custom model with the agent’s efforts', async () => {
    server.use(
      http.get('/v1/model-catalog', () =>
        HttpResponse.json({ error: { code: 'internal_error', message: 'boom' } }, { status: 500 }),
      ),
    )
    const bodies = captureBodies('post', '/v1/model-profiles')
    const user = userEvent.setup()
    renderSection()
    await screen.findByRole('row', { name: /^codex\b/ })

    await user.click(screen.getByRole('button', { name: 'Add profile' }))
    const dialog = screen.getByRole('dialog')
    await user.type(within(dialog).getByLabelText('Name'), 'mine')
    await user.selectOptions(within(dialog).getByLabelText('Model'), 'Other…')
    await user.type(within(dialog).getByLabelText('Custom model'), 'claude-opus-5-5')
    await user.selectOptions(within(dialog).getByLabelText('Effort'), 'max')
    await user.click(within(dialog).getByRole('button', { name: 'Save' }))

    await waitFor(() =>
      expect(bodies).toEqual([
        { name: 'mine', agent: 'claude-code', model: 'claude-opus-5-5', effort: 'max', description: '' },
      ]),
    )
  })

  it('saving a profile whose stored effort the model no longer takes sends an empty effort', async () => {
    server.use(
      http.get('/v1/model-profiles', () =>
        HttpResponse.json({
          profiles: [
            {
              name: 'old-haiku',
              agent: 'claude-code',
              model: 'claude-haiku-4-5-20251001',
              effort: 'low',
              description: 'Old',
              enabled: true,
              position: 0,
            },
          ],
        }),
      ),
      http.patch('/v1/model-profiles/:name', async ({ request }) => HttpResponse.json(await request.json())),
    )
    const bodies = captureBodies('patch', '/v1/model-profiles/')
    const user = userEvent.setup()
    renderSection()
    await screen.findByRole('row', { name: /^old-haiku\b/ })

    await user.click(within(row('old-haiku')).getByRole('button', { name: 'Edit' }))
    const dialog = screen.getByRole('dialog')
    await waitFor(() => expect(within(dialog).getByLabelText('Effort')).toBeDisabled())
    await user.type(within(dialog).getByLabelText('Good for'), ' and fast')
    await user.click(within(dialog).getByRole('button', { name: 'Save' }))

    await waitFor(() =>
      expect(bodies).toEqual([
        { agent: 'claude-code', model: 'claude-haiku-4-5-20251001', effort: '', description: 'Old and fast' },
      ]),
    )
  })

  it('shows the server error inside the editor (profile_exists)', async () => {
    const user = userEvent.setup()
    renderSection()
    await screen.findByRole('row', { name: /^codex\b/ })

    await user.click(screen.getByRole('button', { name: 'Add profile' }))
    const dialog = screen.getByRole('dialog')
    await user.type(within(dialog).getByLabelText('Name'), 'codex')
    await user.selectOptions(within(dialog).getByLabelText('Agent'), 'codex')
    await user.click(within(dialog).getByRole('button', { name: 'Save' }))

    expect(await within(dialog).findByRole('alert')).toHaveTextContent('A profile with this name already exists')
  })

  it('deleting a default profile shows profile_in_use and keeps the row', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const user = userEvent.setup()
    renderSection()
    await screen.findByRole('row', { name: /^claude-opus\b/ })

    await user.click(within(row('claude-opus')).getByRole('button', { name: 'Delete' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('This profile is a default — pick another default first')
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

    await user.click(within(row('codex')).getByRole('checkbox', { name: 'Enabled' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('disk full')

    await user.click(within(row('claude-opus')).getByRole('button', { name: 'Delete' }))
    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent('This profile is a default — pick another default first'),
    )
    expect(screen.getByRole('alert')).not.toHaveTextContent('disk full')
  })

  it('deletes a profile that is not a default', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const user = userEvent.setup()
    renderSection()
    await screen.findByRole('row', { name: /^codex\b/ })

    await user.click(within(row('codex')).getByRole('button', { name: 'Delete' }))

    await waitFor(() => expect(screen.queryByRole('row', { name: /^codex\b/ })).not.toBeInTheDocument())
  })

  it('picks the default worker profile, sending only that setting', async () => {
    const bodies = captureBodies('put', '/v1/settings')
    const user = userEvent.setup()
    renderSection()
    const worker = await screen.findByLabelText('Default worker profile')
    await waitFor(() => expect(worker).toHaveValue('claude-sonnet'))
    expect(screen.getByLabelText('Default orchestrator profile')).toHaveValue('claude-opus')
    expect(within(worker).getByRole('option', { name: 'claude-haiku — disabled' })).toBeInTheDocument()

    await user.selectOptions(worker, 'codex')

    await waitFor(() => expect(bodies).toEqual([{ default_worker_profile: 'codex' }]))
    await waitFor(() => expect(worker).toHaveValue('codex'))
  })

  it('says where each agent’s model list came from, with the fallback warning', async () => {
    renderSection()
    expect(await screen.findByText('Model list for claude-code: from Claude Code cache')).toBeInTheDocument()
    expect(screen.getByText('Model list for codex: built-in')).toBeInTheDocument()
    expect(screen.getByText(/executable file not found/)).toBeInTheDocument()
  })

  it('«Refresh» asks the daemon to refetch the catalog', async () => {
    const urls: string[] = []
    server.events.on('request:start', ({ request }) => {
      if (new URL(request.url).pathname === '/v1/model-catalog') urls.push(new URL(request.url).search)
    })
    const user = userEvent.setup()
    renderSection()
    await screen.findByText('Model list for claude-code: from Claude Code cache')

    await user.click(screen.getByRole('button', { name: 'Refresh' }))

    await waitFor(() => expect(urls).toEqual(['', '?refresh=1']))
  })

  it('a failed catalog load says so and offers Refresh', async () => {
    let fail = true
    server.use(
      http.get('/v1/model-catalog', () =>
        fail
          ? HttpResponse.json({ error: { code: 'internal_error', message: 'boom' } }, { status: 500 })
          : HttpResponse.json({
              agents: [{ agent: 'codex', source: 'cli', fetched_at: null, warning: '', models: [] }],
            }),
      ),
    )
    const user = userEvent.setup()
    renderSection()
    expect(await screen.findByText(/Couldn’t load the model list/)).toBeInTheDocument()

    fail = false
    await user.click(screen.getByRole('button', { name: 'Refresh' }))
    expect(await screen.findByText('Model list for codex: from Codex CLI')).toBeInTheDocument()
    expect(screen.queryByText(/Couldn’t load the model list/)).not.toBeInTheDocument()
  })

  it('an import that skips models for another reason lists them', async () => {
    server.use(
      http.post('/v1/model-profiles/import-catalog', () =>
        HttpResponse.json({
          created: [],
          skipped: [
            { model: 'claude-opus-5-5', reason: 'profile claude-opus-5-5 already uses it' },
            { model: 'weird/id', reason: 'cannot derive a valid profile name' },
          ],
        }),
      ),
    )
    const user = userEvent.setup()
    renderSection()
    await screen.findByRole('row', { name: /^codex\b/ })

    await user.click(screen.getByRole('button', { name: 'Add profiles for all models' }))
    const status = await screen.findByRole('status')
    expect(status).toHaveTextContent(/^No new models\. Skipped/)
    expect(status).toHaveTextContent('Skipped: weird/id (cannot derive a valid profile name)')
    expect(status).not.toHaveTextContent('claude-opus-5-5')
  })

  it('imports the main catalog models as disabled profiles and lists them', async () => {
    const bodies = captureBodies('post', '/v1/model-profiles/import-catalog')
    const user = userEvent.setup()
    renderSection()
    await screen.findByRole('row', { name: /^codex\b/ })

    await user.click(screen.getByRole('button', { name: 'Add profiles for all models' }))

    await waitFor(() => expect(bodies).toEqual([{ include_legacy: false }]))
    expect(await screen.findByRole('status')).toHaveTextContent(
      'Created: 5 (claude-opus-5-5, claude-sonnet-5-5, claude-haiku-4-5-20251001, codex-gpt-6-sol, codex-gpt-5-6-luna)',
    )
    const added = await screen.findByRole('row', { name: /^claude-opus-5-5\b/ })
    expect(within(added).getByRole('checkbox', { name: 'Enabled' })).not.toBeChecked()
    expect(screen.queryByRole('row', { name: /^claude-opus-4-6\b/ })).not.toBeInTheDocument()
  })

  it('«include previous models» imports previous models too; a second run reports nothing new', async () => {
    const bodies = captureBodies('post', '/v1/model-profiles/import-catalog')
    const user = userEvent.setup()
    renderSection()
    await screen.findByRole('row', { name: /^codex\b/ })

    await user.click(screen.getByRole('checkbox', { name: 'include previous models' }))
    await user.click(screen.getByRole('button', { name: 'Add profiles for all models' }))
    expect(await screen.findByRole('row', { name: /^claude-opus-4-6\b/ })).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Add profiles for all models' }))
    await waitFor(() =>
      expect(screen.getByRole('status')).toHaveTextContent('No new models — every model already has a profile.'),
    )
    expect(bodies).toEqual([{ include_legacy: true }, { include_legacy: true }])
  })

  it('shows an import failure in place', async () => {
    server.use(
      http.post('/v1/model-profiles/import-catalog', () =>
        HttpResponse.json({ error: { code: 'human_only', message: 'only a human can do this' } }, { status: 403 }),
      ),
    )
    const user = userEvent.setup()
    renderSection()
    await screen.findByRole('row', { name: /^codex\b/ })

    await user.click(screen.getByRole('button', { name: 'Add profiles for all models' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('only a human can do this')
  })

  it('an empty registry invites adding the first profile', async () => {
    server.use(http.get('/v1/model-profiles', () => HttpResponse.json({ profiles: [] })))
    renderSection()
    expect(await screen.findByText('No profiles yet — add the first one.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Add profile' })).toBeInTheDocument()
  })
})
