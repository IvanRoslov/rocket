import { describe, expect, it } from 'vitest'
import { ScrollAccumulator, encodeScroll, wheelDeltaToLines } from './termScroll'

describe('wheelDeltaToLines', () => {
  it('converts pixel deltas using the terminal cell height', () => {
    expect(wheelDeltaToLines(-36, 0, 18, 30)).toBe(-2)
  })

  it('keeps line deltas as lines', () => {
    expect(wheelDeltaToLines(3, 1, 18, 30)).toBe(3)
  })

  it('converts page deltas using the current row count', () => {
    expect(wheelDeltaToLines(-1, 2, 18, 30)).toBe(-30)
  })
})

describe('ScrollAccumulator', () => {
  it('accumulates small trackpad deltas without dropping a line', () => {
    const acc = new ScrollAccumulator()
    expect([0.25, 0.25, 0.25, 0.25].map((delta) => acc.add(-delta))).toEqual([0, 0, 0, -1])
  })

  it('keeps the fractional remainder after a whole line', () => {
    const acc = new ScrollAccumulator()
    expect(acc.add(-1.75)).toBe(-1)
    expect(acc.add(-0.25)).toBe(-1)
  })

  it('nets opposite-direction fractions before sending a line', () => {
    const acc = new ScrollAccumulator()
    expect(acc.add(-0.75)).toBe(0)
    expect(acc.add(0.25)).toBe(0)
    expect(acc.add(-0.5)).toBe(-1)
  })

  it('clamps a large gesture to the default maximum', () => {
    const acc = new ScrollAccumulator()
    expect(acc.add(-5000)).toBe(-1000)
    expect(acc.add(5000)).toBe(1000)
  })

  it('honors a custom maximum', () => {
    expect(new ScrollAccumulator(8).add(12)).toBe(8)
  })
})

it('encodes a scroll control frame', () => {
  expect(JSON.parse(encodeScroll(-3))).toEqual({ type: 'scroll', lines: -3 })
})
