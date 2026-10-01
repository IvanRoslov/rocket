// Pure rules behind the storm UI (task #4901, spec §3.1): outcome labels,
// the recommendation star, the Problem doc and the exit gate. No React, so
// they are unit-tested without rendering.
import type { BrainstormFields, BrainstormOutcome, GateStatus, TaskDoc, TaskDocKind, TaskGate, TaskStatus } from '../api/types'

export const OUTCOMES: BrainstormOutcome[] = ['accepted', 'corrected', 'wrong_turn']

export const OUTCOME_LABEL: Record<BrainstormOutcome, string> = {
  accepted: 'Accepted',
  corrected: 'Corrected',
  wrong_turn: 'Wrong turn',
}

export function isBrainstorm(t: { type?: string }): boolean {
  return t.type === 'brainstorm'
}

/** Whether the 0-based option `index` is the recommended one (wire numbers are 1-based). */
export function isRecommended(t: Pick<BrainstormFields, 'recommended_option'>, index: number): boolean {
  return t.recommended_option != null && t.recommended_option === index + 1
}

/** Text of the option the human picked; `''` for an answer in their own words. */
export function chosenLabel(t: { options?: string[]; chosen_option?: number | null }): string {
  if (t.chosen_option == null) return ''
  return t.options?.[t.chosen_option - 1] ?? ''
}

/** The newest doc of `kind` — the highest id, since versions count per title. */
export function latestDoc(docs: TaskDoc[], kind: TaskDocKind): TaskDoc | undefined {
  return docs.filter((d) => d.kind === kind).reduce<TaskDoc | undefined>((a, d) => (!a || d.id > a.id ? d : a), undefined)
}

/** The gate awaiting the human's decision, if any. */
export function pendingGate(gates: TaskGate[]): TaskGate | undefined {
  return gates.find((g) => g.status === 'pending')
}

const GATE_STATUS_LABEL: Record<Exclude<GateStatus, 'changes'>, string> = {
  pending: 'Awaiting decision',
  go: 'Go',
  superseded: 'Superseded by a newer spec',
}

/** One line of the gate history: "v1 — Needs changes: “…”", "v2 — Go". */
export function gateHistoryLabel(g: TaskGate): string {
  const what = g.status === 'changes' ? `Needs changes: “${g.comment}”` : GATE_STATUS_LABEL[g.status]
  return `v${g.spec_version} — ${what}`
}

/**
 * The storm tab is shown while the task brainstorms and stays afterwards for
 * any task that had a storm; old tasks without one never see it.
 */
export function showBrainstormTab(
  task: { status: TaskStatus; brainstorm_skill?: string },
  questions: { type?: string }[],
  gates: TaskGate[],
): boolean {
  return (
    task.status === 'brainstorm' || !!task.brainstorm_skill || questions.some(isBrainstorm) || gates.length > 0
  )
}

/**
 * The doc a gate pinned. A gate records only kind + version (the newest doc
 * of that kind when it was requested), so among every version in the history
 * it is the latest doc of that kind and version written by request time —
 * the same rule as the web tab.
 */
export function docAt(
  docs: TaskDoc[],
  kind: TaskDocKind,
  version: number | null,
  requestedAt: number,
): TaskDoc | undefined {
  if (version == null) return undefined
  return latestDoc(
    docs.filter((d) => d.version === version && d.created_at <= requestedAt),
    kind,
  )
}

export type ExitState =
  | { kind: 'waiting_spec' }
  | { kind: 'waiting_request' }
  | { kind: 'pending'; gate: TaskGate }
  | { kind: 'go'; gate: TaskGate }

/**
 * What the exit block shows (the web tab's `waitingText` rules). `gates` are
 * newest first; `specVersion` is the latest spec's, undefined without one. A
 * spec newer than the last gate, no gate yet or a superseded one only needs a
 * request; after "changes" on the current spec, a revised spec comes first.
 */
export function exitState(gates: TaskGate[], specVersion: number | undefined): ExitState {
  const pending = pendingGate(gates)
  if (pending) return { kind: 'pending', gate: pending }
  const latest = gates[0]
  if (latest?.status === 'go') return { kind: 'go', gate: latest }
  if (specVersion === undefined) return { kind: 'waiting_spec' }
  if (!latest || specVersion > latest.spec_version || latest.status === 'superseded') {
    return { kind: 'waiting_request' }
  }
  return { kind: 'waiting_spec' }
}
