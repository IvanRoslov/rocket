// The presentational half of a question thread: task threads and role threads
// render the same markup and differ only in the mutations wired into it.

import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { QuestionThreadView } from './QuestionThreadView'

const base = {
  ordinal: 2,
  body: 'Ship it?',
  messages: [{ id: 1, author: 'sre-run-3', body: 'ready', created_at: 1_800_000_000 }],
  turnLabel: 'awaiting you',
  turnWarn: true,
  askerLabel: 'sre asked',
  agentName: 'sre',
  agentInitial: 'A',
  onClarify: vi.fn(),
  onAnswer: vi.fn(),
  onDismiss: vi.fn(),
}

describe('QuestionThreadView', () => {
  it('renders the question, the turn chip and agent-authored entries', () => {
    render(<QuestionThreadView {...base} />)

    expect(screen.getByText('Q2')).toBeInTheDocument()
    expect(screen.getByText('awaiting you')).toBeInTheDocument()
    expect(screen.getByText('sre asked')).toBeInTheDocument()
    expect(screen.getByText('sre')).toBeInTheDocument()
    expect(screen.getByText('ready')).toBeInTheDocument()
  })

  it('labels human entries as "you"', () => {
    render(
      <QuestionThreadView
        {...base}
        messages={[{ id: 2, author: '', body: 'not yet', created_at: 1_800_000_000 }]}
      />,
    )

    expect(screen.getByText('you')).toBeInTheDocument()
  })

  it('hands the composed body to onAnswer and clears the textarea', async () => {
    const onAnswer = vi.fn()
    render(<QuestionThreadView {...base} onAnswer={onAnswer} />)

    const box = screen.getByLabelText('Reply to Q2')
    await userEvent.type(box, 'yes, ship')
    await userEvent.click(screen.getByRole('button', { name: /Answer & close/ }))

    expect(onAnswer).toHaveBeenCalledWith('yes, ship')
    expect(box).toHaveValue('')
  })

  it('hands the body to onClarify without clearing on a keep-open reply', async () => {
    const onClarify = vi.fn()
    render(<QuestionThreadView {...base} onClarify={onClarify} />)

    await userEvent.type(screen.getByLabelText('Reply to Q2'), 'rephrase please')
    await userEvent.click(screen.getByRole('button', { name: /Clarify/ }))

    expect(onClarify).toHaveBeenCalledWith('rephrase please')
  })

  it('disables both submit actions while the body is empty', () => {
    render(<QuestionThreadView {...base} />)

    expect(screen.getByRole('button', { name: /Clarify/ })).toBeDisabled()
    expect(screen.getByRole('button', { name: /Answer & close/ })).toBeDisabled()
    expect(screen.getByRole('button', { name: /Dismiss/ })).toBeEnabled()
  })

  it('hides the turn chip when nobody is waiting', () => {
    render(<QuestionThreadView {...base} turnLabel="" turnWarn={false} />)

    expect(screen.queryByText('awaiting you')).not.toBeInTheDocument()
  })
})
