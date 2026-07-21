import type { ChatEntry } from '../api/types'
import { buildChatRows, type OutgoingMsg } from './chatRows'

const T0 = 1_784_640_000
const user = (text: string, ts = T0): ChatEntry => ({ role: 'user', text, ts })
const bot = (text: string, ts = T0): ChatEntry => ({ role: 'assistant', text, ts })
const tool = (text: string, ts = T0): ChatEntry => ({ role: 'tool', tool_name: 'Bash', text, ts })
const out = (msgId: number, body: string, sentAt = T0): OutgoingMsg => ({ msgId, body, sentAt })

const build = (entries: ChatEntry[], outgoing: OutgoingMsg[] = [], showNoise = false) =>
  buildChatRows({ entries, outgoing, showNoise })

describe('buildChatRows', () => {
  it('keeps both optimistic bubbles when the same text is sent twice', () => {
    const rows = build([], [out(1, 'ок'), out(2, 'ок')])
    expect(rows.filter((r) => r.kind === 'outgoing')).toHaveLength(2)
  })

  it('each transcript entry retires exactly one optimistic bubble', () => {
    const rows = build([user('ок')], [out(1, 'ок'), out(2, 'ок')])
    const outgoing = rows.filter((r) => r.kind === 'outgoing')
    expect(outgoing).toHaveLength(1)
    expect(rows.filter((r) => r.kind === 'entry')).toHaveLength(1)
  })

  it('two consecutive different messages both survive the round trip', () => {
    const entries = [user('по токену создам в гитхабе'), user('или где у нас там сборка')]
    const rows = build(entries, [out(1, 'по токену создам в гитхабе'), out(2, 'или где у нас там сборка')])
    expect(rows).toHaveLength(2)
    expect(rows.every((r) => r.kind === 'entry')).toBe(true)
  })

  it('an identical message from earlier does not swallow a fresh one', () => {
    const rows = build([user('ок', T0 - 3600)], [out(9, 'ок', T0)])
    expect(rows.filter((r) => r.kind === 'outgoing')).toHaveLength(1)
  })

  it('row keys are unique', () => {
    const rows = build([user('a'), bot('b'), tool('{}'), user('c')], [out(1, 'x'), out(2, 'y')], true)
    expect(new Set(rows.map((r) => r.key)).size).toBe(rows.length)
  })

  it('hides tool calls and system injections unless asked', () => {
    const entries = [user('hi'), tool('{}'), user('<task-notification>done</task-notification>'), bot('yo')]
    expect(build(entries)).toHaveLength(2)
    expect(build(entries, [], true)).toHaveLength(4)
  })

  it('carries queue status onto the optimistic bubble', () => {
    const rows = buildChatRows({
      entries: [],
      outgoing: [out(7, 'hi')],
      queueMessages: [
        { id: 7, to: 'orch', body: 'hi', status: 'failed', attempts: 3, created_at: T0, reason: 'recipient busy' },
      ],
      showNoise: false,
    })
    expect(rows[0]).toMatchObject({ kind: 'outgoing', status: 'failed', reason: 'recipient busy' })
  })
})
