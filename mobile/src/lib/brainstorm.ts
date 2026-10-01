// Pure rules behind the storm UI (task #4901, spec §3.1): outcome labels,
// the recommendation star, the Problem doc and the exit gate. No React, so
// they are unit-tested without rendering.
import type { BrainstormFields, BrainstormOutcome, GateStatus, TaskDoc, TaskDocKind, TaskGate, TaskStatus } from '../api/types'

export const OUTCOMES: BrainstormOutcome[] = ['accepted', 'corrected', 'wrong_turn']

export const OUTCOME_LABEL: Record<BrainstormOutcome, string> = {
  accepted: 'принято',
  corrected: 'поправлено',
  wrong_turn: 'ушли не туда',
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
  pending: 'ждёт решения',
  go: 'Go',
  superseded: 'снят: спека обновилась',
}

/** One line of the gate history: "v1 — правки «…»", "v2 — Go". */
export function gateHistoryLabel(g: TaskGate): string {
  const what = g.status === 'changes' ? `правки «${g.comment}»` : GATE_STATUS_LABEL[g.status]
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
