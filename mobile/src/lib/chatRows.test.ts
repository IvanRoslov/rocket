import type { ChatEntry } from '../api/types'
import { buildChatRows, isNoise, type OutgoingMsg } from './chatRows'

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

  it('thread frames are not noise, rocket injects are', () => {
    expect(isNoise({ role: 'user', text: '[#1/Q1 reply from cto] ok', ts: 1 })).toBe(false)
    expect(isNoise({ role: 'user', text: '[rocket heartbeat] idle', ts: 1 })).toBe(true)
  })
})

describe('buildChatRows v2', () => {
  const u = (text: string, ts: number): ChatEntry => ({ role: 'user', text, ts })
  const toolAt = (name: string, ts: number): ChatEntry => ({
    role: 'tool',
    tool_name: name,
    text: '{"command":"ls"}',
    ts,
  })

  it('confirms two identical sends against two transcript entries', () => {
    const rows = buildChatRows({
      entries: [u('ok', 100), u('ok', 101)],
      outgoing: [
        { msgId: 1, body: 'ok', sentAt: 100 },
        { msgId: 2, body: 'ok', sentAt: 101 },
      ],
      showNoise: false,
    })
    expect(rows.filter((r) => r.kind === 'outgoing')).toHaveLength(0)
    expect(rows).toHaveLength(2)
  })

  it('a [large message] pointer after the send claims it and renders the sent text in place', () => {
    const rows = buildChatRows({
      entries: [
        u('before', 90),
        u('[large message] Full text written to /x.md', 130),
        { role: 'assistant', text: 'got it', ts: 140 },
      ],
      outgoing: [{ msgId: 7, body: 'very long text', sentAt: 120 }],
      showNoise: false,
    })
    expect(rows.map((r) => (r.kind === 'entry' ? r.entry.text : r.kind))).toEqual([
      'before',
      'very long text',
      'got it',
    ])
  })

  it('a pending short send does not steal the pointer of a later large send', () => {
    const big = 'x'.repeat(3000)
    const rows = buildChatRows({
      entries: [u('[large message] Full text written to /x.md', 130)],
      outgoing: [
        { msgId: 1, body: 'hi', sentAt: 120 },
        { msgId: 2, body: big, sentAt: 125 },
      ],
      showNoise: false,
    })
    expect(rows.map((r) => (r.kind === 'entry' ? r.entry.text : r.kind === 'outgoing' ? `outgoing:${r.body}` : r.kind))).toEqual([big, 'outgoing:hi'])
  })

  it('a stale [large message] pointer is not claimed', () => {
    const big = 'x'.repeat(3000)
    const rows = buildChatRows({
      entries: [u('[large message] Full text written to /old.md', 100)],
      outgoing: [{ msgId: 1, body: big, sentAt: 200 }],
      showNoise: false,
    })
    expect(rows.map((r) => r.kind)).toEqual(['outgoing'])
  })

  it('two large sends and two pointers each show their own text, in order', () => {
    const a = 'a'.repeat(3000)
    const b = 'b'.repeat(3000)
    const rows = buildChatRows({
      entries: [u('[large message] one', 130), u('[large message] two', 140)],
      outgoing: [
        { msgId: 1, body: a, sentAt: 120 },
        { msgId: 2, body: b, sentAt: 125 },
      ],
      showNoise: false,
    })
    expect(rows.map((r) => (r.kind === 'entry' ? r.entry.text : r.kind))).toEqual([a, b])
  })

  it('drops the AskUserQuestion ask half when its answer follows', () => {
    const rows = buildChatRows({
      entries: [
        { role: 'tool', tool_name: 'AskUserQuestion', text: '{}', ts: 1, quiz: { questions: [] } },
        { role: 'quiz_answer', text: 'A', ts: 2 },
      ],
      outgoing: [],
      showNoise: true,
    })
    expect(rows.map((r) => r.kind === 'entry' && r.entry.role)).toEqual(['quiz_answer'])
  })

  it('groups 2+ adjacent tool entries when noise is shown, keeps a single one inline', () => {
    const rows = buildChatRows({
      entries: [toolAt('Bash', 1), toolAt('Read', 2), { role: 'assistant', text: 'x', ts: 3 }, toolAt('Bash', 4)],
      outgoing: [],
      showNoise: true,
    })
    expect(rows.map((r) => r.kind)).toEqual(['tools', 'entry', 'entry'])
    expect(rows[0].kind === 'tools' && rows[0].entries).toHaveLength(2)
  })
})
