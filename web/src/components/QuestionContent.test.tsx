// The brief (the plain-language version an agent writes alongside a question)
// leads; the full body folds under "Details". No brief — nothing changes.

import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { QuestionContent } from './QuestionContent'

const body = 'The long **detailed** body with every consideration.'
const brief = '**Проблема:** it is slow.\n\n**Рекомендация:** cache it.'

describe('QuestionContent', () => {
  it('renders the brief as markdown and keeps the body collapsed', () => {
    const { container } = render(
      <QuestionContent brief={brief} body={body} bodyClassName="the-body" />,
    )

    expect(container.querySelector('.question-brief strong')).toHaveTextContent('Проблема:')
    expect(screen.getByText(/cache it/)).toBeInTheDocument()
    expect(screen.queryByText(/every consideration/)).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Details/ })).toHaveAttribute('aria-expanded', 'false')
  })

  it('shows the body once Details is expanded, and hides it again', async () => {
    const { container } = render(
      <QuestionContent brief={brief} body={body} bodyClassName="the-body" />,
    )

    await userEvent.click(screen.getByRole('button', { name: /Details/ }))

    expect(screen.getByRole('button', { name: /Details/ })).toHaveAttribute('aria-expanded', 'true')
    expect(container.querySelector('.the-body strong')).toHaveTextContent('detailed')

    await userEvent.click(screen.getByRole('button', { name: /Details/ }))
    expect(screen.queryByText(/every consideration/)).not.toBeInTheDocument()
  })

  it('puts children between the brief and the Details disclosure', () => {
    const { container } = render(
      <QuestionContent brief={brief} body={body} bodyClassName="the-body">
        <div className="the-options">options</div>
      </QuestionContent>,
    )

    const order = Array.from(container.children).map((el) => el.className)
    expect(order).toEqual(['question-brief', 'the-options', 'question-details'])
  })

  it.each([undefined, '', '   '])('renders the body as before without a brief (%j)', (none) => {
    const { container } = render(
      <QuestionContent brief={none} body={body} bodyClassName="the-body">
        <div className="the-options">options</div>
      </QuestionContent>,
    )

    expect(container.querySelector('.the-body strong')).toHaveTextContent('detailed')
    expect(container.querySelector('.question-brief')).toBeNull()
    expect(screen.queryByRole('button', { name: /Details/ })).not.toBeInTheDocument()
    expect(Array.from(container.children).map((el) => el.className)).toEqual([
      'the-body',
      'the-options',
    ])
  })
})
