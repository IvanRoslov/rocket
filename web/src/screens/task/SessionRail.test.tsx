// SessionRail «▣ term» actions: since round 6 they are plain anchors that
// open the dedicated /term/:sessionId page in a NEW TAB (target="_blank"),
// not the in-page TermOverlay.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { sessions } from '../../mocks/fixtures'
import { SessionRail } from './SessionRail'

function renderRail() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const orchestrator = sessions.find((s) => s.kind === 'orchestrator')!
  const workers = sessions.filter((s) => s.kind === 'worker')
  render(
    <QueryClientProvider client={queryClient}>
      <SessionRail orchestrator={orchestrator} workers={workers} />
    </QueryClientProvider>,
  )
  return { orchestrator, workers }
}

describe('SessionRail term links', () => {
  it('opens the orchestrator terminal page in a new tab', () => {
    const { orchestrator } = renderRail()
    const links = screen.getAllByRole('link', { name: /term/i })
    const orchLink = links[0]
    expect(orchLink).toHaveAttribute('href', `/term/${orchestrator.id}`)
    expect(orchLink).toHaveAttribute('target', '_blank')
    expect(orchLink).toHaveAttribute('rel', expect.stringContaining('noopener'))
  })

  it('links every worker to its own terminal page', () => {
    const { workers } = renderRail()
    const links = screen.getAllByRole('link', { name: /term/i })
    expect(links).toHaveLength(1 + workers.length)
    for (const w of workers) {
      expect(
        links.some((l) => l.getAttribute('href') === `/term/${w.id}`),
      ).toBe(true)
    }
  })
})

describe('SessionRail chat links', () => {
  it('opens the orchestrator chat page in a new tab', () => {
    const { orchestrator } = renderRail()
    const links = screen.getAllByRole('link', { name: /chat/i })
    const orchLink = links[0]
    expect(orchLink).toHaveAttribute('href', `/chat/${orchestrator.id}`)
    expect(orchLink).toHaveAttribute('target', '_blank')
    expect(orchLink).toHaveAttribute('rel', expect.stringContaining('noopener'))
  })

  it('links every worker to its own chat page', () => {
    const { workers } = renderRail()
    const links = screen.getAllByRole('link', { name: /chat/i })
    expect(links).toHaveLength(1 + workers.length)
    for (const w of workers) {
      expect(links.some((l) => l.getAttribute('href') === `/chat/${w.id}`)).toBe(true)
    }
  })
})

describe('SessionRail worker card layout', () => {
  it('never collapses a long hyphenated worker name to sub-word width, and keeps the repo on its own line', () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const orchestrator = sessions.find((s) => s.kind === 'orchestrator')!
    const longName = 'feature-authoring-service-gateway-dashboard-refactor-w7'
    const longRepo = 'lessly-platform-gateway-frontend'
    const worker: (typeof sessions)[number] = {
      ...sessions.find((s) => s.kind === 'worker')!,
      id: 's-long-name-worker',
      tmux_name: longName,
      repo_id: longRepo,
    }
    render(
      <QueryClientProvider client={queryClient}>
        <SessionRail orchestrator={orchestrator} workers={[worker]} />
      </QueryClientProvider>,
    )

    const nameEl = screen.getByTitle(longName)
    expect(nameEl).toHaveClass('session-rail__name')
    expect(nameEl.textContent).toBe(longName)
    // The name element must not be squeezed by a sibling flex item taking
    // the rest of the row's width (the original bug): it lives alone on
    // the head row, not next to the repo name.
    expect(nameEl.parentElement).toHaveClass('session-rail__worker-head')
    expect(within(nameEl.parentElement as HTMLElement).queryByText(longRepo)).not.toBeInTheDocument()

    const repoEl = screen.getByTitle(longRepo)
    expect(repoEl).toHaveClass('session-rail__worker-repo')
    expect(repoEl.textContent).toBe(longRepo)
  })
})

describe('SessionRail quiz badge', () => {
  it('shows a "quiz" badge for an orchestrator with a pending_quiz', () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const orchestrator = sessions.find((s) => s.id === 's-quiz-demo-orch')!
    render(
      <QueryClientProvider client={queryClient}>
        <SessionRail orchestrator={orchestrator} workers={[]} />
      </QueryClientProvider>,
    )
    const meta = screen.getByText('Orchestrator').closest('.session-rail__orch-meta')
    expect(within(meta as HTMLElement).getByText('quiz')).toBeInTheDocument()
  })

  it('labels a worker\'s pending permission prompt «Разрешение» instead of "quiz"', () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const orchestrator = sessions.find((s) => s.id === 's-quiz-demo-orch')!
    const worker = sessions.find((s) => s.id === 's-perm-demo-worker')!
    render(
      <QueryClientProvider client={queryClient}>
        <SessionRail orchestrator={{ ...orchestrator, pending_quiz: undefined }} workers={[worker]} />
      </QueryClientProvider>,
    )
    expect(screen.getByText('Разрешение')).toBeInTheDocument()
    expect(screen.queryByText('quiz')).not.toBeInTheDocument()
  })

  it('shows no "quiz" badge for an orchestrator with no pending_quiz', () => {
    const { orchestrator } = renderRail()
    expect(orchestrator.pending_quiz).toBeUndefined()
    const meta = screen.getByText('Orchestrator').closest('.session-rail__orch-meta')
    expect(within(meta as HTMLElement).queryByText('quiz')).not.toBeInTheDocument()
  })
})

// Which agent runs which session: workers can be spawned on a different
// agent than their orchestrator (`rocket spawn --agent codex`), so the rail
// names the agent on every card.
describe('SessionRail agent badges', () => {
  it('names the agent of the orchestrator and of every worker', () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const orchestrator = { ...sessions.find((s) => s.kind === 'orchestrator')!, agent: 'claude-code' }
    const workers = sessions
      .filter((s) => s.kind === 'worker')
      .map((w, i) => ({ ...w, agent: i === 0 ? 'codex' : 'claude-code' }))
    render(
      <QueryClientProvider client={queryClient}>
        <SessionRail orchestrator={orchestrator} workers={workers} />
      </QueryClientProvider>,
    )

    const orchCard = screen.getByText(orchestrator.tmux_name).closest<HTMLElement>('.session-rail__orch')!
    expect(within(orchCard).getByText('claude-code')).toBeInTheDocument()

    const codexCard = screen.getByText(workers[0].tmux_name).closest<HTMLElement>('.session-rail__worker')!
    expect(within(codexCard).getByText('codex')).toBeInTheDocument()

    const claudeCard = screen.getByText(workers[1].tmux_name).closest<HTMLElement>('.session-rail__worker')!
    expect(within(claudeCard).getByText('claude-code')).toBeInTheDocument()
  })
})
