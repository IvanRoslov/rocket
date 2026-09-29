import { connectSse, parseSseFrames } from './sse'

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

describe('connectSse auth', () => {
  class FakeXhr {
    static last: FakeXhr | null = null
    static count = 0
    headers: Record<string, string> = {}
    readyState = 0
    status = 0
    responseText = ''
    aborted = false
    onreadystatechange: (() => void) | null = null
    onerror: (() => void) | null = null
    ontimeout: (() => void) | null = null
    constructor() {
      FakeXhr.last = this
      FakeXhr.count++
    }
    open() {}
    setRequestHeader(k: string, v: string) {
      this.headers[k] = v
    }
    send() {}
    abort() {
      this.aborted = true
    }
  }
  const realXhr = globalThis.XMLHttpRequest
  beforeEach(() => {
    jest.useFakeTimers()
    FakeXhr.last = null
    FakeXhr.count = 0
    globalThis.XMLHttpRequest = FakeXhr as unknown as typeof XMLHttpRequest
  })
  afterEach(() => {
    jest.useRealTimers()
    globalThis.XMLHttpRequest = realXhr
  })

  it('sets the supplied headers on the request', () => {
    const conn = connectSse('http://x/v1/events/stream', { onOpen() {}, onEvent() {}, onError() {} }, 4000, {
      Authorization: 'Bearer t',
    })
    expect(FakeXhr.last!.headers.Authorization).toBe('Bearer t')
    expect(FakeXhr.last!.headers.Accept).toBe('text/event-stream')
    conn.close()
  })

  it('calls onUnauthorized on 401 and does not reconnect', () => {
    const onUnauthorized = jest.fn()
    const onError = jest.fn()
    connectSse('http://x/v1/events/stream', { onOpen() {}, onEvent() {}, onError, onUnauthorized })
    const x = FakeXhr.last!
    x.readyState = 2
    x.status = 401
    x.onreadystatechange!()
    expect(onUnauthorized).toHaveBeenCalledTimes(1)
    expect(x.aborted).toBe(true)
    jest.advanceTimersByTime(20000)
    expect(FakeXhr.count).toBe(1)
    expect(onError).not.toHaveBeenCalled()
  })
})
