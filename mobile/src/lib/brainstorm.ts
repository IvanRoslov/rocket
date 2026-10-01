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
 * The doc of `kind` at `version` — from the full history, since a gate pins
 * versions that a newer save has since replaced. Newest row wins when two
 * titles share a version number.
 */
export function docAt(docs: TaskDoc[], kind: TaskDocKind, version: number | null): TaskDoc | undefined {
  if (version == null) return undefined
  return latestDoc(
    docs.filter((d) => d.version === version),
    kind,
  )
}

export type ExitState =
  | { kind: 'waiting_spec' }
  | { kind: 'waiting_request' }
  | { kind: 'pending'; gate: TaskGate }
  | { kind: 'go'; gate: TaskGate }

/**
 * What the exit block shows. `gates` are newest first. A superseded or sent
 * back gate leaves the storm waiting for the next request — not for a spec,
 * which already exists by then.
 */
export function exitState(gates: TaskGate[], docs: TaskDoc[]): ExitState {
  const pending = pendingGate(gates)
  if (pending) return { kind: 'pending', gate: pending }
  if (gates[0]?.status === 'go') return { kind: 'go', gate: gates[0] }
  if (gates.length > 0 || latestDoc(docs, 'spec')) return { kind: 'waiting_request' }
  return { kind: 'waiting_spec' }
}
