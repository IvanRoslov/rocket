// Covers docs/13-chat.md's client contract for the /chat/:sessionId page:
// all three transcript roles render distinguishably, the composer is
// orchestrator-only and gated on state, sending posts {to,body} (no `from`)
// with an optimistic bubble, and cursor-based increments append to the feed.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  appendChatEntry,
  handlers,
  lastQuizAnswerBody,
  resetChatEntries,
  resetQuizAnswerSpy,
  resetSessions,
} from '../../mocks/handlers'
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
  resetSessions()
  resetQuizAnswerSpy()
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
    expect(screen.getByText('go test ./internal/billing/...')).toBeInTheDocument()
    expect(screen.getByText('go test ./internal/billing/...').closest('.chat-screen__tool-row')).toBeInTheDocument()
  })
})

describe('ChatScreen tool grouping', () => {
  it('collapses 3+ consecutive tool entries into one expandable summary row, single tool entries stay inline', async () => {
    appendChatEntry('s-billing-v2-orch', {
      role: 'tool',
      tool_name: 'Bash',
      text: '{"command":"ls"}',
      ts: 1_800_000_800,
    })
    appendChatEntry('s-billing-v2-orch', {
      role: 'tool',
      tool_name: 'Edit',
      text: '{"file_path":"/repo/foo.go"}',
      ts: 1_800_000_801,
    })
    appendChatEntry('s-billing-v2-orch', {
      role: 'tool',
      tool_name: 'Edit',
      text: '{"file_path":"/repo/bar.go"}',
      ts: 1_800_000_802,
    })
    const user = userEvent.setup()
    renderPage()
    await screen.findByText('смотрю на трейс')

    // The original fixture's lone Bash tool entry stays inline (no wrapper).
    expect(screen.getByText('go test ./internal/billing/...')).toBeInTheDocument()

    // The 3 newly appended tool entries collapse into one summary row.
    const summary = await screen.findByRole('button', { name: /⚒ 3 действий · Bash ×1, Edit ×2/ })
    expect(summary).toBeInTheDocument()
    expect(screen.queryByText('ls')).not.toBeInTheDocument()
    expect(screen.queryByText('foo.go')).not.toBeInTheDocument()

    await user.click(summary)
    expect(screen.getByText('ls')).toBeInTheDocument()
    expect(screen.getByText('foo.go')).toBeInTheDocument()
    expect(screen.getByText('bar.go')).toBeInTheDocument()

    const collapseBtn = screen.getByRole('button', { name: /свернуть/ })
    await user.click(collapseBtn)
    expect(screen.queryByText('ls')).not.toBeInTheDocument()
    expect(await screen.findByRole('button', { name: /⚒ 3 действий/ })).toBeInTheDocument()
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

describe('ChatScreen service inject classification', () => {
  it('renders a task-notification user entry as a collapsed pale-yellow left bubble, expanding on click', async () => {
    appendChatEntry('s-billing-v2-orch', {
      role: 'user',
      text: '<task-notification><summary>Задача #7 закрыта</summary>полный текст уведомления</task-notification>',
      ts: 1_800_000_700,
    })
    const user = userEvent.setup()
    renderPage()
    await screen.findByText('смотрю на трейс')

    const toggle = await screen.findByRole('button', { name: /Задача #7 закрыта/ })
    expect(toggle).toBeInTheDocument()
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText(/полный текст уведомления/)).not.toBeInTheDocument()

    const row = toggle.closest('.chat-screen__row')
    expect(row).not.toHaveClass('chat-screen__row--own')
    expect(within(row as HTMLElement).getByText('system')).toBeInTheDocument()
    expect(toggle.closest('.chat-screen__bubble')).toHaveClass('chat-screen__bubble--system')

    await user.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByText(/полный текст уведомления/)).toBeInTheDocument()
  })

  it('renders a short [large message] pointer entry fully, not collapsed', async () => {
    appendChatEntry('s-billing-v2-orch', {
      role: 'user',
      text: '[large message] stored as file, 4.2kb',
      ts: 1_800_000_701,
    })
    renderPage()
    await screen.findByText('смотрю на трейс')

    const body = await screen.findByText('[large message] stored as file, 4.2kb')
    expect(screen.queryByRole('button', { name: /large message/i })).not.toBeInTheDocument()
    const row = body.closest('.chat-screen__row')
    expect(row).not.toHaveClass('chat-screen__row--own')
    expect(body.closest('.chat-screen__bubble')).toHaveClass('chat-screen__bubble--system')
  })

  it('renders a short [rocket heartbeat] entry fully, in a pale-yellow left bubble with a "rocket" badge', async () => {
    appendChatEntry('s-billing-v2-orch', {
      role: 'user',
      text: '[rocket heartbeat] 3 sessions running, 1 idle, 0 errored',
      ts: 1_800_000_703,
    })
    renderPage()
    await screen.findByText('смотрю на трейс')

    const body = await screen.findByText('[rocket heartbeat] 3 sessions running, 1 idle, 0 errored')
    const bubble = body.closest('.chat-screen__bubble')
    expect(bubble).toHaveClass('chat-screen__bubble--system')
    const row = body.closest('.chat-screen__row')
    expect(row).not.toHaveClass('chat-screen__row--own')
    expect(within(bubble as HTMLElement).getByText('rocket')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /heartbeat/i })).not.toBeInTheDocument()
  })

  it('renders a long <task-notification> dump collapsed with the system badge, expanding to raw text', async () => {
    const longBody = 'x'.repeat(500)
    appendChatEntry('s-billing-v2-orch', {
      role: 'user',
      text: `<task-notification><summary>Длинное уведомление</summary>${longBody}</task-notification>`,
      ts: 1_800_000_704,
    })
    const user = userEvent.setup()
    renderPage()
    await screen.findByText('смотрю на трейс')

    const toggle = await screen.findByRole('button', { name: /Длинное уведомление/ })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    await user.click(toggle)
    expect(screen.getByText(new RegExp(longBody))).toBeInTheDocument()
  })

  it('renders a [from <session>] queue inject as a left-aligned bubble with a from-badge, not the own bubble', async () => {
    appendChatEntry('s-billing-v2-orch', {
      role: 'user',
      text: '[from billing-v2-w1] схема готова к ревью',
      ts: 1_800_000_702,
    })
    renderPage()
    await screen.findByText('смотрю на трейс')

    const body = await screen.findByText('схема готова к ревью')
    const row = body.closest('.chat-screen__row')
    expect(row).not.toHaveClass('chat-screen__row--own')
    expect(within(row as HTMLElement).getByText('from billing-v2-w1')).toBeInTheDocument()
  })

  it('still renders a plain human user entry as the own right-aligned bubble', async () => {
    renderPage()
    const bubble = await screen.findByText('почему упал тест biling_test.go?')
    expect(bubble.closest('.chat-screen__row')).toHaveClass('chat-screen__row--own')
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

// ---------------------------------------------------------------------------
// Quizzes (AskUserQuestion) — docs/13-chat.md «Квизы».
// s-quiz-demo-orch fixture: a live `pending_quiz` (single-select "Мерж" +
// multi-select "Релиз") plus a closed round in its chat history ("Линтер",
// tool+quiz_answer pair, single-select answered "ESLint").
// ---------------------------------------------------------------------------

describe('ChatScreen live quiz', () => {
  it('renders the pending quiz questions and options', async () => {
    renderPage('s-quiz-demo-orch')
    expect(await screen.findByText('Какую стратегию мержа выбрать?')).toBeInTheDocument()
    expect(screen.getByText('Merge commit')).toBeInTheDocument()
    expect(screen.getByText('Squash')).toBeInTheDocument()
    expect(screen.getByText('Что включить в релиз? (можно несколько)')).toBeInTheDocument()
    expect(screen.getByText('Доки')).toBeInTheDocument()
  })

  it('single-select lets exactly one option be picked at a time', async () => {
    const user = userEvent.setup()
    renderPage('s-quiz-demo-orch')
    await screen.findByText('Какую стратегию мержа выбрать?')

    const mergeCommit = screen.getByRole('radio', { name: /Merge commit/ })
    const squash = screen.getByRole('radio', { name: /Squash/ })
    await user.click(mergeCommit)
    expect(mergeCommit).toBeChecked()
    await user.click(squash)
    expect(squash).toBeChecked()
    expect(mergeCommit).not.toBeChecked()
  })

  it('multi-select allows several checkboxes checked at once', async () => {
    const user = userEvent.setup()
    renderPage('s-quiz-demo-orch')
    await screen.findByText('Что включить в релиз? (можно несколько)')

    const docs = screen.getByRole('checkbox', { name: /Доки/ })
    const migrations = screen.getByRole('checkbox', { name: /Миграции/ })
    await user.click(docs)
    await user.click(migrations)
    expect(docs).toBeChecked()
    expect(migrations).toBeChecked()
  })

  it('disables "Ответить" until every question is answered, then enables it', async () => {
    const user = userEvent.setup()
    renderPage('s-quiz-demo-orch')
    await screen.findByText('Какую стратегию мержа выбрать?')

    const submit = screen.getByRole('button', { name: 'Ответить' })
    expect(submit).toBeDisabled()

    await user.click(screen.getByRole('radio', { name: /Squash/ }))
    expect(submit).toBeDisabled() // second question still unanswered

    await user.click(screen.getByRole('checkbox', { name: /Доки/ }))
    expect(submit).toBeEnabled()
  })

  it('POSTs the exact {answers} shape and shows "отправляется…" until resolved', async () => {
    const user = userEvent.setup()
    renderPage('s-quiz-demo-orch')
    await screen.findByText('Какую стратегию мержа выбрать?')

    await user.click(screen.getByRole('radio', { name: /Squash/ }))
    await user.click(screen.getByRole('checkbox', { name: /Доки/ }))
    await user.click(screen.getByRole('checkbox', { name: /CLI/ }))
    await user.click(screen.getByRole('button', { name: 'Ответить' }))

    await waitFor(() =>
      expect(lastQuizAnswerBody).toEqual({
        answers: [
          { question_index: 0, option_indices: [1] },
          { question_index: 1, option_indices: [0, 2] },
        ],
      }),
    )
    expect(await screen.findByText('отправляется…')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Ответить' })).not.toBeInTheDocument()
  })

  it('a free-text "Другое…" answer posts {question_index, text}', async () => {
    const user = userEvent.setup()
    renderPage('s-quiz-demo-orch')
    await screen.findByText('Какую стратегию мержа выбрать?')

    const textareas = screen.getAllByPlaceholderText('свой вариант')
    await user.type(textareas[0], 'rebase, пожалуйста')
    await user.click(screen.getByRole('checkbox', { name: /CLI/ }))
    await user.click(screen.getByRole('button', { name: 'Ответить' }))

    await waitFor(() =>
      expect(lastQuizAnswerBody).toEqual({
        answers: [
          { question_index: 0, text: 'rebase, пожалуйста' },
          { question_index: 1, option_indices: [2] },
        ],
      }),
    )
  })

  it('shows the 400 quiz_answer_invalid message inline and keeps the quiz editable', async () => {
    server.use(
      http.post('/v1/sessions/:id/quiz/answer', () =>
        HttpResponse.json(
          { error: { code: 'quiz_answer_invalid', message: 'опций больше 9' } },
          { status: 400 },
        ),
      ),
    )
    const user = userEvent.setup()
    renderPage('s-quiz-demo-orch')
    await screen.findByText('Какую стратегию мержа выбрать?')

    await user.click(screen.getByRole('radio', { name: /Squash/ }))
    await user.click(screen.getByRole('checkbox', { name: /Доки/ }))
    await user.click(screen.getByRole('button', { name: 'Ответить' }))

    expect(await screen.findByText('опций больше 9')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Ответить' })).toBeEnabled()
  })

  it('blocks the composer with "агент ждёт ответа на квиз" while pending_quiz is set', async () => {
    renderPage('s-quiz-demo-orch')
    await screen.findByText('Какую стратегию мержа выбрать?')
    expect(screen.queryByLabelText('Message the orchestrator')).not.toBeInTheDocument()
    expect(screen.getByText('агент ждёт ответа на квиз')).toBeInTheDocument()
  })
})

describe('ChatScreen closed quiz rounds', () => {
  it('renders a tool+quiz_answer pair as one bubble with the chosen option highlighted', async () => {
    renderPage('s-quiz-demo-orch')
    const question = await screen.findByText('Какой линтер подключить?')
    expect(question).toBeInTheDocument()

    const bubble = question.closest('.chat-screen__bubble--quiz')
    expect(bubble).not.toBeNull()
    const selected = within(bubble as HTMLElement).getByText('ESLint')
    expect(selected).toHaveClass('chat-screen__quiz-option--selected')
    expect(within(bubble as HTMLElement).getByText('Biome')).not.toHaveClass(
      'chat-screen__quiz-option--selected',
    )
    // AskUserQuestion never joins a "⚒" tool-group.
    expect(screen.queryByText(/⚒/)).not.toBeInTheDocument()
  })

  it('renders a cancelled round as a muted "квиз отменён" bubble', async () => {
    appendChatEntry('s-quiz-demo-orch', {
      role: 'tool',
      tool_name: 'AskUserQuestion',
      text: '{"questions":[{"question":"Деплоить сейчас?"...',
      ts: 1_800_010_000,
      quiz: {
        questions: [
          { question: 'Деплоить сейчас?', header: 'Деплой', multiSelect: false, options: [{ label: 'Да' }, { label: 'Нет' }] },
        ],
      },
    })
    appendChatEntry('s-quiz-demo-orch', { role: 'quiz_answer', text: 'квиз отменён', ts: 1_800_010_001 })

    renderPage('s-quiz-demo-orch')
    const cancelled = await screen.findByText('квиз отменён')
    expect(cancelled.closest('.chat-screen__bubble')).toHaveClass('chat-screen__bubble--quiz-cancelled')
  })

  it('degrades a lone unpaired AskUserQuestion tool entry to a plain tool row', async () => {
    appendChatEntry('s-quiz-demo-orch', {
      role: 'tool',
      tool_name: 'AskUserQuestion',
      text: '{"questions":[{"question":"Незакрытый квиз?"...',
      ts: 1_800_020_000,
      quiz: {
        questions: [{ question: 'Незакрытый квиз?', header: 'Открыт', multiSelect: false, options: [{ label: 'Да' }] }],
      },
    })

    renderPage('s-quiz-demo-orch')
    const badge = await screen.findAllByText('AskUserQuestion')
    expect(badge.length).toBeGreaterThan(0)
    expect(badge[badge.length - 1].closest('.chat-screen__tool-row')).toBeInTheDocument()
  })
})
