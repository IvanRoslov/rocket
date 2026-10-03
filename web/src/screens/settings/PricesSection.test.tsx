// Settings › Prices (task #5138 spec §5): every model seen in usage with its
// four $/1M prices, saved per row; Clear drops a model's prices.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import { MemoryRouter } from 'react-router-dom'
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest'
import { handlers, resetPrices } from '../../mocks/handlers'
import { PricesSection } from './PricesSection'
import { SettingsScreen } from './SettingsScreen'

const server = setupServer(...handlers)
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => {
  server.resetHandlers()
  resetPrices()
})
afterAll(() => server.close())

function renderWith(node: React.ReactNode, path = '/settings') {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[path]}>{node}</MemoryRouter>
    </QueryClientProvider>,
  )
}

async function rowOf(model: string) {
  return (await screen.findByRole('row', { name: new RegExp(model) })) as HTMLElement
}

function spyPuts() {
  const puts: Array<{ model: string; body: unknown }> = []
  server.events.on('request:start', async ({ request }) => {
    const url = new URL(request.url)
    if (request.method === 'PUT' && url.pathname.startsWith('/v1/stats/prices/')) {
      puts.push({ model: decodeURIComponent(url.pathname.split('/').pop()!), body: await request.clone().json() })
    }
  })
  return puts
}

describe('PricesSection', () => {
  afterEach(() => server.events.removeAllListeners())

  it('lists every model with its prices, unpriced ones empty', async () => {
    renderWith(<PricesSection />)
    const opus = await rowOf('claude-opus-5-5')
    expect(within(opus).getByLabelText('claude-opus-5-5 input')).toHaveValue('5')
    expect(within(opus).getByLabelText('claude-opus-5-5 cache read')).toHaveValue('0.5')
    const sol = await rowOf('gpt-6-sol')
    for (const kind of ['input', 'cache write', 'cache read', 'output']) {
      expect(within(sol).getByLabelText(`gpt-6-sol ${kind}`)).toHaveValue('')
    }
  })

  it('saves a row with numbers, an empty field goes as null', async () => {
    const user = userEvent.setup()
    const puts = spyPuts()
    renderWith(<PricesSection />)
    const sol = await rowOf('gpt-6-sol')
    const save = within(sol).getByRole('button', { name: 'Save' })
    expect(save).toBeDisabled()
    await user.type(within(sol).getByLabelText('gpt-6-sol input'), '1.25')
    await user.type(within(sol).getByLabelText('gpt-6-sol cache read'), '0.125')
    await user.type(within(sol).getByLabelText('gpt-6-sol output'), '10')
    await user.click(save)
    await waitFor(() => expect(puts).toHaveLength(1))
    expect(puts[0]).toEqual({
      model: 'gpt-6-sol',
      body: { input: 1.25, cache_write: null, cache_read: 0.125, output: 10 },
    })
    // The saved prices come back from the daemon and the row starts over from them.
    await waitFor(async () => expect(within(await rowOf('gpt-6-sol')).getByLabelText('gpt-6-sol input')).toHaveValue('1.25'))
    expect(within(await rowOf('gpt-6-sol')).getByRole('button', { name: 'Save' })).toBeDisabled()
  })

  it('treats the same number in another spelling as unchanged', async () => {
    const user = userEvent.setup()
    renderWith(<PricesSection />)
    const opus = await rowOf('claude-opus-5-5')
    const input = within(opus).getByLabelText('claude-opus-5-5 input')
    await user.clear(input)
    await user.type(input, '5.00')
    expect(within(opus).getByRole('button', { name: 'Save' })).toBeDisabled()
  })

  it('refuses a negative or non-numeric price without a request', async () => {
    const user = userEvent.setup()
    const puts = spyPuts()
    renderWith(<PricesSection />)
    const sol = await rowOf('gpt-6-sol')
    await user.type(within(sol).getByLabelText('gpt-6-sol input'), '-1')
    await user.click(within(sol).getByRole('button', { name: 'Save' }))
    expect(await within(sol).findByRole('alert')).toHaveTextContent(/number ≥ 0/)
    await user.clear(within(sol).getByLabelText('gpt-6-sol input'))
    await user.type(within(sol).getByLabelText('gpt-6-sol input'), 'abc')
    await user.click(within(sol).getByRole('button', { name: 'Save' }))
    expect(await within(sol).findByRole('alert')).toHaveTextContent(/number ≥ 0/)
    expect(puts).toHaveLength(0)
  })

  it('Clear drops the prices of a model', async () => {
    const user = userEvent.setup()
    const deleted: string[] = []
    server.events.on('request:start', ({ request }) => {
      if (request.method === 'DELETE') deleted.push(new URL(request.url).pathname)
    })
    renderWith(<PricesSection />)
    const opus = await rowOf('claude-opus-5-5')
    expect(within(await rowOf('gpt-6-sol')).getByRole('button', { name: 'Clear' })).toBeDisabled()
    await user.click(within(opus).getByRole('button', { name: 'Clear' }))
    await waitFor(() => expect(deleted).toEqual(['/v1/stats/prices/claude-opus-5-5']))
    await waitFor(async () =>
      expect(within(await rowOf('claude-opus-5-5')).getByLabelText('claude-opus-5-5 input')).toHaveValue(''),
    )
  })

  it('shows the daemon refusal', async () => {
    const user = userEvent.setup()
    server.use(
      http.put('/v1/stats/prices/:model', () =>
        HttpResponse.json({ error: { code: 'human_only', message: 'only a human edits prices' } }, { status: 403 }),
      ),
    )
    renderWith(<PricesSection />)
    const sol = await rowOf('gpt-6-sol')
    await user.type(within(sol).getByLabelText('gpt-6-sol input'), '2')
    await user.click(within(sol).getByRole('button', { name: 'Save' }))
    expect(await within(sol).findByRole('alert')).toHaveTextContent('only a human edits prices')
  })

  it('says there is nothing to price before any usage', async () => {
    server.use(http.get('/v1/stats/prices', () => HttpResponse.json({ prices: [] })))
    renderWith(<PricesSection />)
    expect(await screen.findByText(/No models yet/)).toBeInTheDocument()
  })
})

describe('Settings › Prices', () => {
  it('is in the nav and opens straight from ?section=prices', async () => {
    renderWith(<SettingsScreen />, '/settings?section=prices')
    expect(await screen.findByRole('heading', { name: 'Prices' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Prices' })).toHaveClass('settings-nav__item--active')
  })
})
