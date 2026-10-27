/** Renders the Storms tab against GET /v1/brainstorms: rows, filters, tap target, empty state. */
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native'
import { router } from 'expo-router'
import { SafeAreaProvider } from 'react-native-safe-area-context'
import { ServerProvider } from '../../src/servers/ServerContext'
import StormsScreen from '../../app/(tabs)/storms'

jest.mock('expo-router', () => ({
  router: { navigate: jest.fn(), push: jest.fn(), back: jest.fn() },
}))

const gate = { id: 1, spec_version: 3, requested_at: 1 }
const base = {
  project_id: 'pay', task_status: 'brainstorm', questions_waiting: 0, questions_stale: false,
  pending_gate: null, waiting_since: 1,
}
const ASK = { ...base, task_id: 11, task_title: 'Ask me', questions_waiting: 2, questions_stale: true }
const GO = { ...base, task_id: 12, task_title: 'Go me', pending_gate: gate, task_status: 'in_progress' }

function mockApi(brainstorms: unknown[]) {
  globalThis.fetch = jest.fn(async (url: string) => {
    const path = String(url).replace(/^https?:\/\/[^/]+/, '')
    if (path === '/v1/brainstorms') return { ok: true, status: 200, json: async () => ({ brainstorms }) }
    return { ok: false, status: 404, json: async () => ({ error: { code: 'not_found' } }) }
  }) as unknown as typeof fetch
}

async function renderScreen() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } })
  const metrics = {
    frame: { x: 0, y: 0, width: 390, height: 844 },
    insets: { top: 47, left: 0, right: 0, bottom: 34 },
  }
  return await render(
    <SafeAreaProvider initialMetrics={metrics}>
      <QueryClientProvider client={qc}>
        <ServerProvider>
          <StormsScreen />
        </ServerProvider>
      </QueryClientProvider>
    </SafeAreaProvider>,
  )
}

describe('Storms tab', () => {
  afterEach(() => jest.clearAllMocks())

  it('lists cards with their waits and filters to Needs Go', async () => {
    mockApi([ASK, GO])
    await renderScreen()
    expect(await screen.findByText('#11 Ask me')).toBeTruthy()
    expect(screen.getByText('2 questions')).toBeTruthy()
    expect(screen.getByText('STALE')).toBeTruthy()
    expect(screen.getByText('Go · spec v3')).toBeTruthy()
    expect(screen.getByText('mid-work')).toBeTruthy()

    fireEvent.press(screen.getByText('Needs Go'))
    await waitFor(() => expect(screen.queryByText('#11 Ask me')).toBeNull())
    expect(screen.getByText('#12 Go me')).toBeTruthy()

    fireEvent.press(screen.getByText('Needs answer'))
    await waitFor(() => expect(screen.queryByText('#12 Go me')).toBeNull())
    expect(screen.getByText('#11 Ask me')).toBeTruthy()
  })

  it('a tap opens the task on its Brainstorm chip', async () => {
    mockApi([ASK])
    await renderScreen()
    fireEvent.press(await screen.findByText('#11 Ask me'))
    expect(router.navigate).toHaveBeenCalledWith('/task/11?tab=brainstorm')
  })

  it('says so when the selected filter matches nothing', async () => {
    mockApi([ASK])
    await renderScreen()
    await screen.findByText('#11 Ask me')
    fireEvent.press(screen.getByText('Needs Go'))
    expect(await screen.findByText('Nothing needs a Go')).toBeTruthy()
    expect(screen.queryByText('#11 Ask me')).toBeNull()
  })

  it('says so when nothing waits', async () => {
    mockApi([])
    await renderScreen()
    expect(await screen.findByText('No brainstorms are waiting on you')).toBeTruthy()
  })
})
