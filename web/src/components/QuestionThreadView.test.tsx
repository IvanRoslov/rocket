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

  // The local ref ("1023/Q2", "cto/Q1") is the one thread id a human sees —
  // the bare ordinal only survives as the fallback for a pre-#1023 daemon.
  it('shows the local ref in place of the bare ordinal', () => {
    render(<QuestionThreadView {...base} localRef="1023/Q2" />)

    expect(screen.getByText('1023/Q2')).toBeInTheDocument()
    expect(screen.queryByText('Q2')).not.toBeInTheDocument()
    expect(screen.getByLabelText('Reply to 1023/Q2')).toBeInTheDocument()
  })

  it('falls back to Q<ordinal> when the daemon sends no local ref', () => {
    render(<QuestionThreadView {...base} />)

    expect(screen.getByText('Q2')).toBeInTheDocument()
  })

  it('badges a stale thread', () => {
    render(<QuestionThreadView {...base} stale />)

    expect(screen.getByText('stale')).toBeInTheDocument()
  })

  it('leaves a moving thread unbadged', () => {
    render(<QuestionThreadView {...base} />)

    expect(screen.queryByText('stale')).not.toBeInTheDocument()
  })

  // Picking an option closes the thread with that answer; `choose` is 1-based
  // (internal/api/threads.go chooseOptionBody), which is the whole point of
  // this test — an off-by-one here answers the wrong option.
  it('renders options as buttons that close the thread with a 1-based index', async () => {
    const onChoose = vi.fn()
    render(<QuestionThreadView {...base} options={['Ship it', 'Wait']} onChoose={onChoose} />)

    await userEvent.click(screen.getByRole('button', { name: 'Wait' }))

    expect(onChoose).toHaveBeenCalledWith(2)
  })

  it('renders no option row when the thread has no options', () => {
    render(<QuestionThreadView {...base} onChoose={vi.fn()} />)

    expect(screen.queryByLabelText('Answer options')).not.toBeInTheDocument()
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
