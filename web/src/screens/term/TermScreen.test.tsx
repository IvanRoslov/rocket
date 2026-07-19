// Tests for the dedicated full-window terminal page (/term/:sessionId):
// route rendering with the session name resolved from GET /v1/sessions/{id},
// the header controls (back, A−/A+, attach-copy), the fit container that
// clips the terminal, and the termPagePath helper the «▣ term» links use.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { setupServer } from 'msw/node'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { handlers } from '../../mocks/handlers'
import { TermScreen, termPagePath } from './TermScreen'

vi.mock('../../components/TermPanel', () => ({
  DEFAULT_TERM_FONT_SIZE: 14,
  TermPanel: ({ sessionId, fontSize }: { sessionId: string; fontSize?: number }) => (
    <div className="term-panel" data-testid="term-panel-stub" data-font-size={fontSize}>
      <div className="term-panel__xterm" data-testid="term-fit-container" />
      term panel for {sessionId}
    </div>
  ),
}))

const server = setupServer(...handlers)

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => {
  server.resetHandlers()
  window.localStorage.clear()
})
afterAll(() => server.close())

function renderPage(sessionId = 's-billing-v2-orch') {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[termPagePath(sessionId)]}>
        <Routes>
          <Route path="/term/:sessionId" element={<TermScreen />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('termPagePath', () => {
  it('builds the /term/:sessionId path', () => {
    expect(termPagePath('s-1')).toBe('/term/s-1')
  })

  it('escapes ids that need it', () => {
    expect(termPagePath('a b')).toBe('/term/a%20b')
  })
})

describe('TermScreen', () => {
  it('renders the terminal for the session id from the route', async () => {
    renderPage()
    expect(screen.getByTestId('term-panel-stub')).toHaveTextContent(
      'term panel for s-billing-v2-orch',
    )
    // Session name resolves asynchronously from GET /v1/sessions/{id}.
    expect(await screen.findByText('billing-v2-orch')).toBeInTheDocument()
    expect(screen.getByText('tmux · live attach')).toBeInTheDocument()
  })

  it('shows a back control instead of Close, and no Close button', async () => {
    renderPage()
    expect(screen.getByRole('button', { name: /back/i })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /close/i })).not.toBeInTheDocument()
  })

  it('has A−/A+ font controls that feed TermPanel the stored size', async () => {
    window.localStorage.setItem('rocket.term.fontSize', '16')
    renderPage()
    expect(screen.getByRole('button', { name: /decrease terminal font size/i })).toBeEnabled()
    expect(screen.getByRole('button', { name: /increase terminal font size/i })).toBeEnabled()
    expect(screen.getByTestId('term-panel-stub')).toHaveAttribute('data-font-size', '16')
  })

  it('renders the fit container that clips the terminal', () => {
    renderPage()
    expect(screen.getByTestId('term-fit-container')).toHaveClass('term-panel__xterm')
  })

  it('sets the document title to the session name', async () => {
    renderPage()
    await screen.findByText('billing-v2-orch')
    expect(document.title).toBe('billing-v2-orch — rocket term')
  })
})
