import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { TermOverlay } from './TermOverlay'

vi.mock('../../components/TermPanel', () => ({
  DEFAULT_TERM_FONT_SIZE: 14,
  TermPanel: ({ sessionId }: { sessionId: string }) => (
    <div data-testid="term-panel-stub">term panel for {sessionId}</div>
  ),
}))

const session = { id: 'sess-1', tmux_name: 'billing-v2-orch' }

afterEach(() => {
  vi.restoreAllMocks()
  window.localStorage.clear()
})

describe('TermOverlay', () => {
  it('renders the session name, live-attach meta, default geometry, and the term panel', () => {
    render(<TermOverlay session={session} onClose={() => {}} />)

    expect(screen.getByText('billing-v2-orch')).toBeInTheDocument()
    expect(screen.getByText('tmux · live attach')).toBeInTheDocument()
    expect(screen.getByText('80×24')).toBeInTheDocument()
    expect(screen.getByTestId('term-panel-stub')).toHaveTextContent('term panel for sess-1')
  })

  it('calls onClose when the Close button is clicked', () => {
    const onClose = vi.fn()
    render(<TermOverlay session={session} onClose={onClose} />)

    fireEvent.click(screen.getByRole('button', { name: /close/i }))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('calls onClose when the backdrop is clicked, but not when the panel itself is clicked', () => {
    const onClose = vi.fn()
    render(<TermOverlay session={session} onClose={onClose} />)

    fireEvent.click(screen.getByRole('dialog'))
    expect(onClose).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('dialog').parentElement as HTMLElement)
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('calls onClose on Escape', () => {
    const onClose = vi.fn()
    render(<TermOverlay session={session} onClose={onClose} />)

    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('copies the attach command to the clipboard and shows confirmation', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      configurable: true,
    })

    render(<TermOverlay session={session} onClose={() => {}} />)
    fireEvent.click(screen.getByRole('button', { name: /attach/i }))

    expect(writeText).toHaveBeenCalledWith('rocket attach billing-v2-orch')
    expect(await screen.findByText('copied: rocket attach billing-v2-orch')).toBeInTheDocument()
  })

  it('persists the terminal font size to localStorage when A+/A- is clicked', () => {
    render(<TermOverlay session={session} onClose={() => {}} />)

    fireEvent.click(screen.getByRole('button', { name: /increase terminal font size/i }))
    expect(window.localStorage.getItem('rocket.term.fontSize')).toBe('15')

    fireEvent.click(screen.getByRole('button', { name: /decrease terminal font size/i }))
    fireEvent.click(screen.getByRole('button', { name: /decrease terminal font size/i }))
    expect(window.localStorage.getItem('rocket.term.fontSize')).toBe('13')
  })

  it('restores a previously stored font size on mount', () => {
    window.localStorage.setItem('rocket.term.fontSize', '18')
    render(<TermOverlay session={session} onClose={() => {}} />)

    expect(window.localStorage.getItem('rocket.term.fontSize')).toBe('18')
  })
})
