// SessionRail «▣ term» actions: since round 6 they are plain anchors that
// open the dedicated /term/:sessionId page in a NEW TAB (target="_blank"),
// not the in-page TermOverlay.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
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
