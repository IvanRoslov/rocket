/** Encodes a terminal-history scroll request for the term WebSocket. */
export function encodeScroll(lines: number): string {
  return JSON.stringify({ type: 'scroll', lines })
}

/** Converts the three DOM wheel delta units to terminal rows. */
export function wheelDeltaToLines(
  deltaY: number,
  deltaMode: number,
  cellHeightPx: number,
  rows: number
): number {
  if (deltaMode === 1) return deltaY
  if (deltaMode === 2) return deltaY * rows
  return deltaY / cellHeightPx
}

/** Keeps sub-row trackpad movement until it amounts to a whole row. */
export class ScrollAccumulator {
  private remainder = 0
  private readonly maxLines: number

  constructor(maxLines = 1000) {
    this.maxLines = maxLines
  }

  add(deltaLines: number): number {
    const total = this.remainder + deltaLines
    const whole = Math.trunc(total)
    this.remainder = total - whole
    // Math.trunc of a negative fraction is -0; the wire contract uses 0.
    if (whole === 0) return 0
    return Math.max(-this.maxLines, Math.min(this.maxLines, whole))
  }
}
