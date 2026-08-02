// The presentational half of a question thread: task threads and role threads
// render the same markup and differ only in the mutations wired into it.

import { render, screen, within } from '@testing-library/react'
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

    expect(onAnswer).toHaveBeenCalledWith('yes, ship', [])
    expect(box).toHaveValue('')
  })

  it('hands the body to onClarify without clearing on a keep-open reply', async () => {
    const onClarify = vi.fn()
    render(<QuestionThreadView {...base} onClarify={onClarify} />)

    await userEvent.type(screen.getByLabelText('Reply to Q2'), 'rephrase please')
    await userEvent.click(screen.getByRole('button', { name: /Clarify/ }))

    expect(onClarify).toHaveBeenCalledWith('rephrase please', [])
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

  it('lists the thread participants, the human as "you"', () => {
    render(<QuestionThreadView {...base} participants={['human', 'sre-run-3', 'cto']} />)

    const row = screen.getByLabelText('Participants')
    expect(within(row).getByText('you')).toBeInTheDocument()
    expect(within(row).getByText('sre-run-3')).toBeInTheDocument()
    expect(within(row).getByText('cto')).toBeInTheDocument()
  })

  it('shows a message\u2019s addressees when it has them', () => {
    render(
      <QuestionThreadView
        {...base}
        participants={['human', 'sre-run-3', 'cto']}
        messages={[
          {
            id: 1,
            author: 'cto',
            body: 'over to you',
            created_at: 1_800_000_000,
            addressed_to: ['sre-run-3', 'human'],
          },
        ]}
      />,
    )

    expect(screen.getByText('\u2192 sre-run-3, you')).toBeInTheDocument()
  })

  it('renders a human-authored entry as "you" for both "" and "human"', () => {
    render(
      <QuestionThreadView
        {...base}
        messages={[
          { id: 1, author: '', body: 'legacy wire', created_at: 1_800_000_000 },
          { id: 2, author: 'human', body: 'post-#736 wire', created_at: 1_800_000_000 },
        ]}
      />,
    )

    expect(screen.getAllByText('you')).toHaveLength(2)
  })

  it('keeps every agent under its own id once the thread has several of them', () => {
    render(
      <QuestionThreadView
        {...base}
        participants={['human', 'sre-run-3', 'cto']}
        messages={[
          { id: 1, author: 'sre-run-3', body: 'a', created_at: 1_800_000_000 },
          { id: 2, author: 'cto', body: 'b', created_at: 1_800_000_000 },
        ]}
      />,
    )

    const messages = screen.getByLabelText('Discussion')
    expect(within(messages).getByText('sre-run-3')).toBeInTheDocument()
    expect(within(messages).getByText('cto')).toBeInTheDocument()
    expect(within(messages).queryByText('sre')).not.toBeInTheDocument()
  })

  it('passes the picked addressees out, and an empty list when none are picked', async () => {
    const onAnswer = vi.fn()
    render(
      <QuestionThreadView
        {...base}
        participants={['human', 'sre-run-3', 'cto']}
        onAnswer={onAnswer}
      />,
    )

    await userEvent.type(screen.getByLabelText('Reply to Q2'), 'ok')
    await userEvent.click(screen.getByRole('button', { name: /Answer & close/ }))
    expect(onAnswer).toHaveBeenLastCalledWith('ok', [])

    await userEvent.type(screen.getByLabelText('Reply to Q2'), 'again')
    await userEvent.click(screen.getByRole('checkbox', { name: 'cto' }))
    await userEvent.click(screen.getByRole('button', { name: /Answer & close/ }))
    expect(onAnswer).toHaveBeenLastCalledWith('again', ['cto'])
  })

  it('never offers the human as an addressee, and offers nothing without participants', () => {
    const { unmount } = render(
      <QuestionThreadView {...base} participants={['human', 'sre-run-3']} />,
    )
    expect(screen.queryByRole('checkbox', { name: 'you' })).not.toBeInTheDocument()
    expect(screen.getByRole('checkbox', { name: 'sre' })).toBeInTheDocument()
    unmount()

    render(<QuestionThreadView {...base} />)
    expect(screen.queryByRole('checkbox')).not.toBeInTheDocument()
  })
})
