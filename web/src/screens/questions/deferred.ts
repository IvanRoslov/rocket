// The deferred-action queue behind the Questions toast and its Undo.
//
// The daemon has no undo: `POST /v1/questions/{id}/answer` closes a thread for
// good, and a human may not reopen one (only the asking agent can, and only if
// the answer turned out wrong — internal/store/questions_reopen.go). So the
// dashboard buys the undo window on the client: closing, dismissing or
// replying schedules the API call instead of making it, and the toast's Undo
// cancels the scheduled call before it is ever sent.
//
// Undo — the explicit take-back — is the ONLY thing that drops a scheduled
// call. Everything else commits it:
//   - it fires when the toast expires (the human let it stand),
//   - it fires as soon as the human acts again, because moving to the next
//     thread is an implicit commit of the previous one, and
//   - it fires when the page goes away — unmount, pagehide, a hidden tab.
//     Leaving is not an Undo: the human decided, and dropping the call there
//     lost the decision silently. They came back to the thread they had just
//     closed, still open and still yellow, with nothing to say why.
// Losing an answer to anything but Undo is silent data loss.

export interface DeferredQueue {
  /** Arms `run` for later, flushing (not dropping) any action already pending. */
  schedule(run: () => void): void
  /** Drops the pending action without running it. True when there was one. */
  cancel(): boolean
  /** Runs the pending action now. A no-op when nothing is pending. */
  flush(): void
  isPending(): boolean
  /** Commits the pending action and releases the timer — for unmount. */
  dispose(): void
}

export function createDeferredQueue(delayMs: number): DeferredQueue {
  let timer: ReturnType<typeof setTimeout> | null = null
  let pending: (() => void) | null = null

  function clear(): (() => void) | null {
    if (timer !== null) {
      clearTimeout(timer)
      timer = null
    }
    const run = pending
    pending = null
    return run
  }

  const queue: DeferredQueue = {
    schedule(run) {
      queue.flush()
      pending = run
      timer = setTimeout(() => queue.flush(), delayMs)
    },
    cancel() {
      return clear() !== null
    },
    flush() {
      // Clear BEFORE running: the action itself may schedule the next one, and
      // it must not find its own slot still occupied.
      const run = clear()
      if (run) run()
    },
    isPending() {
      return pending !== null
    },
    dispose() {
      // flush(), not cancel(): the human's last decision must still go out.
      queue.flush()
    },
  }
  return queue
}
