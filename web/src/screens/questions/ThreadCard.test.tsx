// The rebuilt question card (task #1264): a big standalone title, a markdown
// body, and a thread that is always open but shows only its last replies.

import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import type { ThreadInboxEntry } from '../../lib/types'
import { ThreadCard, type ThreadCardProps } from './ThreadCard'
import type { ThreadDetail } from './useThreadDetail'

const entry: ThreadInboxEntry = {
  local_ref: '12/Q3',
  kind: 'task',
  task_id: 12,
  subject: 'task #12 "billing v2"',
  id: 3,
  ordinal: 3,
  asked_by: 'billing-v2-orch',
  title: 'Prorate refunds on downgrades?',
  body: 'Finance wants **prorated** refunds.\n\n- option A\n- option B',
  status: 'open',
  type: 'decision',
  participants: ['human', 'billing-v2-orch'],
  attention: ['human'],
  waiting_on: ['human'],
  your_turn: true,
  asked_at: 1_800_000_000,
  updated_at: 1_800_000_000,
  project_id: 'acme',
  task_title: 'billing v2',
}

function reply(n: number) {
  return {
    id: n,
    author: n % 2 === 0 ? 'billing-v2-orch' : 'human',
    kind: 'reply' as const,
    body: `**reply ${n}**`,
    created_at: 1_800_000_000 + n,
  }
}

function renderCard(over: Partial<ThreadCardProps> = {}) {
  const detail: ThreadDetail = { messages: [], isLoading: false }
  const props: ThreadCardProps = {
    entry,
    detail,
    draft: '',
    onDraft: vi.fn(),
    picks: [],
    onTogglePick: vi.fn(),
    onChoose: vi.fn(),
    onAnswerClose: vi.fn(),
    onReply: vi.fn(),
    onSkip: vi.fn(),
    onDismiss: vi.fn(),
    onBackToQueue: vi.fn(),
    onBrowse: vi.fn(),
    ...over,
  }
  return render(
    <MemoryRouter>
      <ThreadCard {...props} />
    </MemoryRouter>,
  )
}

describe('ThreadCard', () => {
  it('renders the title as the heading, separately from the body', () => {
    renderCard()

    const heading = screen.getByRole('heading', { level: 2 })
    expect(heading).toHaveTextContent('Prorate refunds on downgrades?')
    expect(heading.textContent).not.toContain('Finance wants')
    expect(screen.getByText(/Finance wants/)).toBeInTheDocument()
  })

  it('falls back to the body when the daemon sent no title', () => {
    renderCard({ entry: { ...entry, title: '' } })

    expect(screen.getByRole('heading', { level: 2 })).toHaveTextContent('Finance wants')
  })

  it('renders the body as markdown rather than as raw text', () => {
    const { container } = renderCard()

    expect(container.querySelector('.q__body strong')).toHaveTextContent('prorated')
    expect(container.querySelectorAll('.q__body li')).toHaveLength(2)
    expect(screen.queryByText(/\*\*prorated\*\*/)).not.toBeInTheDocument()
  })

  it('renders thread replies as markdown', () => {
    const { container } = renderCard({
      detail: { messages: [reply(1)], isLoading: false },
    })

    expect(container.querySelector('.q__msg-body strong')).toHaveTextContent('reply 1')
  })

  it('keeps the thread open without a conversation toggle', () => {
    renderCard({ detail: { messages: [reply(1)], isLoading: false } })

    expect(screen.getByText('reply 1')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /conversation/i })).not.toBeInTheDocument()
  })

  it('shows the last three replies and reveals the rest on demand', async () => {
    const user = userEvent.setup()
    const messages = [1, 2, 3, 4, 5, 6].map(reply)
    renderCard({ detail: { messages, isLoading: false } })

    expect(screen.queryByText('reply 3')).not.toBeInTheDocument()
    expect(screen.getByText('reply 4')).toBeInTheDocument()
    expect(screen.getByText('reply 6')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Show 3 earlier replies' }))

    expect(screen.getByText('reply 1')).toBeInTheDocument()
    expect(screen.getByText('reply 6')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /earlier replies/ })).not.toBeInTheDocument()
  })

  it('has no context control at all', () => {
    renderCard({ detail: { messages: [], isLoading: false } })

    expect(screen.queryByText(/context/i)).not.toBeInTheDocument()
  })
})
