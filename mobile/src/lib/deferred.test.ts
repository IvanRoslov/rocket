import { createDeferredQueue } from './deferred'

beforeEach(() => jest.useFakeTimers())
afterEach(() => jest.useRealTimers())

it('runs after the delay', () => {
  const run = jest.fn()
  const q = createDeferredQueue(5000)
  q.schedule(run)
  jest.advanceTimersByTime(4999)
  expect(run).not.toHaveBeenCalled()
  jest.advanceTimersByTime(1)
  expect(run).toHaveBeenCalledTimes(1)
})

it('cancel drops the pending action', () => {
  const run = jest.fn()
  const q = createDeferredQueue(5000)
  q.schedule(run)
  expect(q.cancel()).toBe(true)
  jest.advanceTimersByTime(10000)
  expect(run).not.toHaveBeenCalled()
})

it('scheduling a second action commits the first immediately', () => {
  const first = jest.fn()
  const second = jest.fn()
  const q = createDeferredQueue(5000)
  q.schedule(first)
  q.schedule(second)
  expect(first).toHaveBeenCalledTimes(1)
  expect(second).not.toHaveBeenCalled()
  expect(q.isPending()).toBe(true)
})

it('dispose commits rather than drops', () => {
  const run = jest.fn()
  const q = createDeferredQueue(5000)
  q.schedule(run)
  q.dispose()
  expect(run).toHaveBeenCalledTimes(1)
})
