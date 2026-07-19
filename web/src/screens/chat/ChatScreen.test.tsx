// Covers docs/13-chat.md's client contract for the /chat/:sessionId page:
// all three transcript roles render distinguishably, the composer is
// orchestrator-only and gated on state, sending posts {to,body} (no `from`)
// with an optimistic bubble, and cursor-based increments append to the feed.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { appendChatEntry, handlers, resetChatEntries } from '../../mocks/handlers'
import { ChatScreen, chatPagePath } from './ChatScreen'

class MockEventSource {
  static instances: MockEventSource[] = []
  url: string
  listeners: Record<string, ((ev: MessageEvent) => void)[]> = {}
  onerror: ((ev: unknown) => void) | null = null

  constructor(url: string) {
    this.url = url
    MockEventSource.instances.push(this)
  }
  addEventListener(type: string, cb: (ev: MessageEvent) => void) {
    this.listeners[type] ??= []
    this.listeners[type].push(cb)
  }
  close() {}
  emit(type: string, data: unknown) {
    for (const cb of this.listeners[type] ?? []) cb({ data: JSON.stringify(data) } as MessageEvent)
  }
}

const server = setupServer(...handlers)

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
beforeEach(() => {
  MockEventSource.instances = []
  vi.stubGlobal('EventSource', MockEventSource)
})
afterEach(() => {
  server.resetHandlers()
  resetChatEntries()
  vi.unstubAllGlobals()
})
afterAll(() => server.close())

function renderPage(sessionId = 's-billing-v2-orch') {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[chatPagePath(sessionId)]}>
        <Routes>
          <Route path="/chat/:sessionId" element={<ChatScreen />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('chatPagePath', () => {
  it('builds the /chat/:sessionId path', () => {
    expect(chatPagePath('s-1')).toBe('/chat/s-1')
  })
})

describe('ChatScreen feed rendering', () => {
  it('renders user, assistant and tool entries distinguishably', async () => {
    renderPage()

    expect(await screen.findByText('почему упал тест biling_test.go?')).toBeInTheDocument()
    const userBubble = screen.getByText('почему упал тест biling_test.go?').closest('.chat-screen__row')
    expect(userBubble).toHaveClass('chat-screen__row--own')

    const assistantBubble = screen.getByText('смотрю на трейс').closest('.chat-screen__row')
    expect(assistantBubble).not.toHaveClass('chat-screen__row--own')

    expect(screen.getByText('Bash')).toBeInTheDocument()
    expect(screen.getByText(/"command":"go test/)).toBeInTheDocument()
    expect(screen.getByText(/"command":"go test/).closest('.chat-screen__tool-row')).toBeInTheDocument()
  })
})

describe('ChatScreen composer visibility', () => {
  it('shows the composer for a live orchestrator', async () => {
    renderPage('s-billing-v2-orch')
    await screen.findByText('смотрю на трейс')
    expect(screen.getByLabelText('Message the orchestrator')).toBeInTheDocument()
  })

  it('hides the composer for a worker with a read-only note', async () => {
    renderPage('s-billing-v2-w1')
    await screen.findByText('начни со схемы billing_accounts')
    expect(screen.queryByLabelText('Message the orchestrator')).not.toBeInTheDocument()
    expect(screen.getByText('канал воркера — оркестратор')).toBeInTheDocument()
  })

  it('hides the composer for a dead session with a "session ended" note', async () => {
    server.use(
      http.get('/v1/sessions/:id/chat', ({ params }) =>
        HttpResponse.json({
          entries: [],
          next_cursor: '',
          session: { id: params.id as string, kind: 'orchestrator', state: 'killed' },
        }),
      ),
    )
    renderPage('s-billing-v2-orch')
    await screen.findByText('сессия завершена')
    expect(screen.queryByLabelText('Message the orchestrator')).not.toBeInTheDocument()
  })
})

describe('ChatScreen sending', () => {
  it('posts {to, body} without from and renders an optimistic bubble', async () => {
    let posted: unknown
    server.use(
      http.post('/v1/messages', async ({ request }) => {
        posted = await request.json()
        return HttpResponse.json({ id: 999, status: 'queued' }, { status: 201 })
      }),
    )

    const user = userEvent.setup()
    renderPage('s-billing-v2-orch')
    await screen.findByText('смотрю на трейс')

    const box = screen.getByLabelText('Message the orchestrator')
    await user.type(box, 'привет из чата e2e')
    await user.click(screen.getByRole('button', { name: /send/i }))

    await waitFor(() => expect(posted).toEqual({ to: 's-billing-v2-orch', body: 'привет из чата e2e' }))
    expect(await screen.findByText('привет из чата e2e')).toBeInTheDocument()
    expect(screen.getByText('queued')).toBeInTheDocument()
  })
})

describe('ChatScreen GFM rendering', () => {
  it('renders a markdown table in an assistant bubble', async () => {
    appendChatEntry('s-billing-v2-orch', {
      role: 'assistant',
      text: '| Option | Cost |\n| --- | --- |\n| A | $1 |\n| B | $2 |\n',
      ts: 1_800_000_600,
    })
    renderPage('s-billing-v2-orch')

    const table = await screen.findByRole('table')
    expect(table).toBeInTheDocument()
    expect(screen.getByText('Option')).toBeInTheDocument()
    expect(screen.getByText('$2')).toBeInTheDocument()
  })
})

describe('ChatScreen cursor increment', () => {
  it('appends new entries on a session.chat_updated ping', async () => {
    renderPage('s-billing-v2-orch')
    await screen.findByText('смотрю на трейс')

    appendChatEntry('s-billing-v2-orch', { role: 'assistant', text: 'фикс закоммичен', ts: 1_800_000_500 })
    const es = MockEventSource.instances[0]
    es.emit('session.chat_updated', {
      id: 1,
      ts: 1,
      type: 'session.chat_updated',
      session_id: 's-billing-v2-orch',
    })

    expect(await screen.findByText('фикс закоммичен')).toBeInTheDocument()
  })
})
