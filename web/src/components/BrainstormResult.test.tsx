// The answered storm question (task #4901 spec §3.1): what was chosen, the
// human's words, the outcome — switchable by the human — and where it came from.

import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { BrainstormResult, outcomeLabel } from './BrainstormResult'

const answered = {
  options: ['Per seat', 'Per event', 'Hybrid'],
  recommended_option: 2,
  chosen_option: 2,
  answer_comment: '',
  answer_source: 'ui' as const,
  outcome: 'accepted' as const,
  outcome_overridden: false,
}

describe('outcomeLabel', () => {
  it('names every outcome, and an accepted one with a comment', () => {
    expect(outcomeLabel('accepted', '')).toBe('Accepted')
    expect(outcomeLabel('accepted', 'да, но')).toBe('Accepted with comment')
    expect(outcomeLabel('corrected', '')).toBe('Corrected')
    expect(outcomeLabel('wrong_turn', 'своё')).toBe('Wrong turn')
  })
})

describe('BrainstormResult', () => {
  it('shows the chosen option, the comment and the outcome', () => {
    render(<BrainstormResult question={{ ...answered, answer_comment: 'seats as a floor' }} />)

    expect(screen.getByText(/Option 2: Per event/)).toBeInTheDocument()
    expect(screen.getByText('seats as a floor')).toBeInTheDocument()
    expect(screen.getByText('Accepted with comment')).toBeInTheDocument()
    expect(screen.queryByText('From terminal')).not.toBeInTheDocument()
  })

  it('says when the answer was given in own words', () => {
    render(
      <BrainstormResult
        question={{ ...answered, chosen_option: null, answer_comment: 'neither', outcome: 'wrong_turn' }}
      />,
    )
    expect(screen.getByText('Own answer')).toBeInTheDocument()
    expect(screen.getByText('Wrong turn')).toBeInTheDocument()
  })

  it('marks an answer recorded from the terminal', () => {
    render(<BrainstormResult question={{ ...answered, answer_source: 'terminal' }} />)
    expect(screen.getByText('From terminal')).toBeInTheDocument()
  })

  it('lets the human override the outcome', async () => {
    const onOverride = vi.fn()
    render(<BrainstormResult question={answered} onOverride={onOverride} />)

    await userEvent.selectOptions(screen.getByLabelText('Outcome'), 'corrected')
    expect(onOverride).toHaveBeenCalledWith('corrected')
  })

  it('marks an overridden outcome', () => {
    render(<BrainstormResult question={{ ...answered, outcome: 'corrected', outcome_overridden: true }} />)
    expect(screen.getByText('changed by hand')).toBeInTheDocument()
  })

  it('renders nothing for a thread without an outcome', () => {
    const { container } = render(<BrainstormResult question={{ ...answered, outcome: '' }} />)
    expect(container).toBeEmptyDOMElement()
  })
})
