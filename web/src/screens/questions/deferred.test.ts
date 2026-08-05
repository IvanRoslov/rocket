import { createDeferredQueue } from './deferred'

beforeEach(() => vi.useFakeTimers())
afterEach(() => vi.useRealTimers())

test('runs the scheduled action once the delay elapses', () => {
  const q = createDeferredQueue(5000)
  const run = vi.fn()

  q.schedule(run)
  expect(run).not.toHaveBeenCalled()
  expect(q.isPending()).toBe(true)

  vi.advanceTimersByTime(5000)

  expect(run).toHaveBeenCalledTimes(1)
  expect(q.isPending()).toBe(false)
})

// This is the whole point of the queue: Undo must stop the API call, and the
// server has no undo of its own — once the call goes out it is final.
test('cancel() drops the pending action and reports that there was one', () => {
  const q = createDeferredQueue(5000)
  const run = vi.fn()

  q.schedule(run)

  expect(q.cancel()).toBe(true)
  vi.advanceTimersByTime(60_000)
  expect(run).not.toHaveBeenCalled()
  expect(q.cancel()).toBe(false)
})

test('flush() runs the pending action immediately and only once', () => {
  const q = createDeferredQueue(5000)
  const run = vi.fn()

  q.schedule(run)
  q.flush()
  expect(run).toHaveBeenCalledTimes(1)

  q.flush()
  vi.advanceTimersByTime(60_000)
  expect(run).toHaveBeenCalledTimes(1)
})

// Acting again is an implicit commit of the previous action: you answered one
// thread and moved to the next, so the first answer must go out — not be lost
// when the second one takes the slot.
test('scheduling a second action flushes the first', () => {
  const q = createDeferredQueue(5000)
  const first = vi.fn()
  const second = vi.fn()

  q.schedule(first)
  q.schedule(second)

  expect(first).toHaveBeenCalledTimes(1)
  expect(second).not.toHaveBeenCalled()

  vi.advanceTimersByTime(5000)
  expect(second).toHaveBeenCalledTimes(1)
})

// Unmount is not a decision: leaving the page must not silently fire an answer
// the human never saw land.
test('dispose() cancels without running', () => {
  const q = createDeferredQueue(5000)
  const run = vi.fn()

  q.schedule(run)
  q.dispose()

  vi.advanceTimersByTime(60_000)
  expect(run).not.toHaveBeenCalled()
  expect(q.isPending()).toBe(false)
})
