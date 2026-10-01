/**
 * A storm card in the inbox takes one answer: while its answer is in flight
 * (`busy` — the Undo window and the POST) the option buttons are disabled.
 */
import { fireEvent, render, screen } from '@testing-library/react-native'
import type { ThreadInboxEntry } from '../../src/api/types'
import { ThreadCard } from '../../src/components/ThreadCard'

jest.mock('expo-router', () => ({ router: { navigate: jest.fn() } }))

const STORM: ThreadInboxEntry = {
  local_ref: '12/Q1', kind: 'task', task_id: 12, subject: 'task #12', id: 7, ordinal: 1, asked_by: 'orch',
  title: 'Which DB?', body: 'b', status: 'open', type: 'brainstorm', options: ['Postgres', 'SQLite'],
  recommended_option: 2, participants: ['human', 'orch'], attention: ['human'], waiting_on: ['human'],
  your_turn: true, asked_at: 1, updated_at: 1,
}

describe('storm ThreadCard', () => {
  it('disables the options while busy', async () => {
    const onAnswer = jest.fn()
    await render(<ThreadCard thread={STORM} onAnswer={onAnswer} busy />)
    await fireEvent.press(screen.getByTestId('option-2'))
    expect(onAnswer).not.toHaveBeenCalled()
    expect(screen.getByTestId('option-2').props.accessibilityState?.disabled).toBe(true)
  })

  it('answers when not busy', async () => {
    const onAnswer = jest.fn()
    await render(<ThreadCard thread={STORM} onAnswer={onAnswer} />)
    await fireEvent.press(screen.getByTestId('option-2'))
    expect(onAnswer).toHaveBeenCalledWith(STORM, { choose: 2 }, 'SQLite')
  })
})
