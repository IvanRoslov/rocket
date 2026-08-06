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

// Leaving the page is not an Undo. The human decided; only the toast's Undo
// may take that back. Dropping the pending call on unmount lost the decision
// silently — the thread stayed open and yellow.
test('dispose() commits the pending action and releases the timer', () => {
  const q = createDeferredQueue(5000)
  const run = vi.fn()

  q.schedule(run)
  q.dispose()

  expect(run).toHaveBeenCalledTimes(1)
  expect(q.isPending()).toBe(false)

  // Released, not merely fired early: the timer must not run it a second time.
  vi.advanceTimersByTime(60_000)
  expect(run).toHaveBeenCalledTimes(1)
})

test('dispose() is a no-op when nothing is pending', () => {
  const q = createDeferredQueue(5000)
  const run = vi.fn()

  q.schedule(run)
  q.cancel()
  q.dispose()

  vi.advanceTimersByTime(60_000)
  expect(run).not.toHaveBeenCalled()
})
