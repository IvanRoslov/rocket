// Smoke test for the «Терминал | Чат» segmented switch shared by /term and
// /chat headers: both links exist with correct hrefs, the active one is
// marked, and the dark tone modifier is applied on /term's dark chrome.

import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import { TermChatSwitch } from './TermChatSwitch'

describe('TermChatSwitch', () => {
  it('renders both links with correct hrefs and marks the active one', () => {
    render(
      <MemoryRouter>
        <TermChatSwitch sessionId="s-1" active="term" />
      </MemoryRouter>,
    )
    const term = screen.getByRole('link', { name: 'Терминал' })
    const chat = screen.getByRole('link', { name: 'Чат' })
    expect(term).toHaveAttribute('href', '/term/s-1')
    expect(chat).toHaveAttribute('href', '/chat/s-1')
    expect(term).toHaveClass('segmented__option--active')
    expect(chat).not.toHaveClass('segmented__option--active')
  })

  it('marks chat active when active="chat"', () => {
    render(
      <MemoryRouter>
        <TermChatSwitch sessionId="s-1" active="chat" />
      </MemoryRouter>,
    )
    expect(screen.getByRole('link', { name: 'Чат' })).toHaveClass('segmented__option--active')
    expect(screen.getByRole('link', { name: 'Терминал' })).not.toHaveClass('segmented__option--active')
  })

  it('applies the dark tone modifier class when tone="dark"', () => {
    render(
      <MemoryRouter>
        <TermChatSwitch sessionId="s-1" active="term" tone="dark" />
      </MemoryRouter>,
    )
    expect(screen.getByRole('group', { name: 'Терминал или чат' })).toHaveClass('segmented--dark')
  })

  it('does not apply the dark tone modifier by default (light tone)', () => {
    render(
      <MemoryRouter>
        <TermChatSwitch sessionId="s-1" active="term" />
      </MemoryRouter>,
    )
    expect(screen.getByRole('group', { name: 'Терминал или чат' })).not.toHaveClass('segmented--dark')
  })
})
