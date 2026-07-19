import { parseSseFrames } from './sse'

describe('parseSseFrames', () => {
  it('parses named events in the daemon wire format', () => {
    const buf = 'id: 7\nevent: session.spawned\ndata: {"id":7,"type":"session.spawned"}\n\n'
    const { events, parsed } = parseSseFrames(buf, 0)
    expect(events).toEqual([['session.spawned', '{"id":7,"type":"session.spawned"}']])
    expect(parsed).toBe(buf.length)
  })

  it('keeps the incomplete tail unparsed', () => {
    const complete = 'event: task.question_asked\ndata: {}\n\n'
    const buf = complete + 'event: pr.merged\ndata: {"x"'
    const { events, parsed } = parseSseFrames(buf, 0)
    expect(events).toEqual([['task.question_asked', '{}']])
    expect(parsed).toBe(complete.length)
  })

  it('resumes from a prior offset without re-emitting old events', () => {
    const a = 'event: a.b\ndata: 1\n\n'
    const b = 'event: c.d\ndata: 2\n\n'
    const first = parseSseFrames(a, 0)
    const second = parseSseFrames(a + b, first.parsed)
    expect(second.events).toEqual([['c.d', '2']])
  })

  it('handles multiple frames at once and multi-line data', () => {
    const buf = 'event: x.y\ndata: line1\ndata: line2\n\nevent: z.w\ndata: 3\n\n'
    const { events } = parseSseFrames(buf, 0)
    expect(events).toEqual([
      ['x.y', 'line1\nline2'],
      ['z.w', '3'],
    ])
  })

  it('ignores comment/heartbeat frames without data or type', () => {
    const { events } = parseSseFrames(': ping\n\n', 0)
    expect(events).toEqual([])
  })
})
