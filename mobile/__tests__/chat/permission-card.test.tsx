/**
 * Permission prompts in chat mode (task #4881): the daemon publishes a
 * Claude Code permission dialog as pending_quiz with source="permission".
 * One tap on an option answers it — a single keypress on the daemon side.
 */
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react-native'
import { SafeAreaProvider } from 'react-native-safe-area-context'
import { emitDaemonEvent } from '../../src/api/events'
import type { PendingQuiz } from '../../src/api/types'
import { ResolvedPermissionCard } from '../../src/components/PermissionCard'
import { PendingQuizCard } from '../../src/components/QuizCard'
import { ToastProvider } from '../../src/components/Toast'
import { ServerProvider } from '../../src/servers/ServerContext'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

const permission = (options: string[], raw = ''): PendingQuiz => ({
  source: 'permission',
  asked_at: 1785622879,
  raw,
  questions: [
    {
      header: 'Разрешение',
      question: 'Do you want to make this edit to settings.json?\n\n.claude/settings.json\n+ "allow": ["Bash"]',
      multi_select: false,
      options: options.map((label) => ({ label, description: '' })),
    },
  ],
})

const OPTIONS = ['Yes', "Yes, and don't ask again this session", 'No, and tell Claude what to do differently (esc)']

/** Answers every POST with `answer` and records the bodies. */
function mockAnswer(answer: { status: number; body: unknown } = { status: 202, body: { status: 'accepted' } }) {
  const posted: Array<{ path: string; body: unknown }> = []
  globalThis.fetch = jest.fn(async (url: string, init?: RequestInit) => {
    const path = String(url).replace(/^https?:\/\/[^/]+/, '')
    if (init?.method === 'POST') {
      posted.push({ path, body: JSON.parse(String(init.body)) })
      return { ok: answer.status < 300, status: answer.status, json: async () => answer.body }
    }
    return { ok: false, status: 404, json: async () => ({ error: { code: 'not_found' } }) }
  }) as unknown as typeof fetch
  return posted
}

function renderCard(quiz: PendingQuiz, sessionId = 'w1') {
  return render(
    <SafeAreaProvider
      initialMetrics={{ frame: { x: 0, y: 0, width: 390, height: 844 }, insets: { top: 47, left: 0, right: 0, bottom: 34 } }}
    >
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <ServerProvider>
          <ToastProvider>
            <PendingQuizCard sessionId={sessionId} quiz={quiz} />
          </ToastProvider>
        </ServerProvider>
      </QueryClientProvider>
    </SafeAreaProvider>,
  )
}

const isDisabled = (label: string) => screen.getByRole('button', { name: label }).props.accessibilityState?.disabled

describe('PendingQuizCard with a permission prompt', () => {
  afterEach(() => jest.restoreAllMocks())

  it('shows the badge, title, context and one button per option — no quiz controls', async () => {
    mockAnswer()
    await renderCard(permission(OPTIONS))

    expect(screen.getByText('Разрешение')).toBeTruthy()
    expect(screen.getByText('Do you want to make this edit to settings.json?')).toBeTruthy()
    expect(screen.getByText(/^\.claude\/settings\.json \+ "allow"/)).toBeTruthy()
    for (const o of OPTIONS) expect(screen.getByRole('button', { name: o })).toBeTruthy()
    expect(screen.queryByText('Answer')).toBeNull()
    expect(screen.queryByText(/Other/)).toBeNull()
  })

  it('always offers Esc after the options, e.g. to reject a plan whose text option was dropped', async () => {
    const posted = mockAnswer()
    await renderCard(permission(['Yes, and auto-accept edits', 'Yes, and manually approve edits']))

    const buttons = screen.getAllByRole('button').map((b) => b.props.accessibilityLabel)
    expect(buttons).toEqual(['Yes, and auto-accept edits', 'Yes, and manually approve edits', 'Esc'])
    fireEvent.press(screen.getByRole('button', { name: 'Esc' }))

    await waitFor(() => expect(posted).toHaveLength(1))
    expect(posted[0].body).toEqual({ answers: [{ question_index: 0, option_indices: [-1] }] })
    await waitFor(() => expect(isDisabled('Esc')).toBe(true))
  })

  it('answers with a single tap on an option and locks the card', async () => {
    const posted = mockAnswer()
    await renderCard(permission(OPTIONS))

    fireEvent.press(screen.getByRole('button', { name: OPTIONS[1] }))

    await waitFor(() => expect(posted).toHaveLength(1))
    expect(posted[0]).toEqual({
      path: '/v1/sessions/w1/quiz/answer',
      body: { answers: [{ question_index: 0, option_indices: [1] }] },
    })
    await waitFor(() => expect(isDisabled(OPTIONS[0])).toBe(true))
    expect(isDisabled(OPTIONS[2])).toBe(true)
  })

  it('falls back to the raw pane and an Esc button when no options were parsed', async () => {
    const posted = mockAnswer()
    await renderCard(permission([], '╭──────╮\n│ Do you want to proceed? │\n│ ❯ 1. Yes │'))

    expect(screen.getByText(/│ ❯ 1\. Yes │/)).toBeTruthy()
    fireEvent.press(screen.getByRole('button', { name: 'Esc' }))

    await waitFor(() => expect(posted).toHaveLength(1))
    expect(posted[0].body).toEqual({ answers: [{ question_index: 0, option_indices: [-1] }] })
  })

  it('explains a changed dialog and lets you tap again', async () => {
    mockAnswer({ status: 409, body: { error: { code: 'prompt_changed', message: 'prompt changed' } } })
    await renderCard(permission(OPTIONS))

    fireEvent.press(screen.getByRole('button', { name: 'Yes' }))

    await waitFor(() => expect(screen.getByText('Диалог изменился, обновляю…')).toBeTruthy())
    expect(isDisabled('Yes')).toBe(false)
  })

  it('reports an unconfirmed answer for this session only and unlocks the card', async () => {
    mockAnswer()
    await renderCard(permission(OPTIONS))
    fireEvent.press(screen.getByRole('button', { name: 'Yes' }))
    await waitFor(() => expect(isDisabled('Yes')).toBe(true))

    const ev = (sid: string) => JSON.stringify({ type: 'session.quiz_answer_unconfirmed', session_id: sid })
    await act(async () => emitDaemonEvent('session.quiz_answer_unconfirmed', ev('someone-else')))
    expect(screen.queryByText('Ответ не подтвердился')).toBeNull()

    await act(async () => emitDaemonEvent('session.quiz_answer_unconfirmed', ev('w1')))
    expect(screen.getByText('Ответ не подтвердился')).toBeTruthy()
    expect(isDisabled('Yes')).toBe(false)
  })

  it('leaves a regular quiz as it was', async () => {
    mockAnswer()
    await renderCard({ ...permission(OPTIONS), source: undefined })

    expect(screen.getByText('Answer')).toBeTruthy()
    expect(screen.getByText('Other — type your own')).toBeTruthy()
    expect(screen.queryByText('Разрешение', { exact: true })).toBeTruthy() // header badge stays
  })
})

describe('ResolvedPermissionCard', () => {
  it('names the option chosen in chat', async () => {
    await render(
      <ResolvedPermissionCard
        permission={{ title: 'Do you want to proceed?', answer_label: 'Yes', answered_via: 'chat' }}
        fallback="Do you want to proceed?"
      />,
    )
    expect(screen.getByText('Разрешение: Do you want to proceed? → Yes')).toBeTruthy()
  })

  it('says the dialog was answered in the terminal', async () => {
    await render(
      <ResolvedPermissionCard
        permission={{ title: 'Do you want to proceed?', answered_via: 'terminal' }}
        fallback="Do you want to proceed?"
      />,
    )
    expect(screen.getByText('Разрешение: Do you want to proceed? → отвечено в терминале')).toBeTruthy()
  })

  it('falls back to the entry text when the permission object is missing', async () => {
    await render(<ResolvedPermissionCard fallback="Do you want to proceed?" />)
    expect(screen.getByText('Разрешение: Do you want to proceed?')).toBeTruthy()
  })
})
