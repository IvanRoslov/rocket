import type { StageDefinition, Status } from './types'
export function moveStage(stages: StageDefinition[], index: number, direction: number): StageDefinition[] {
 const next = [...stages], target = index + direction
 if (target < 0 || target >= next.length) return next
 ;[next[index],next[target]] = [next[target],next[index]]
 return next
}
export function actionsFor(status: Status, canProgress: boolean): string[] {
 if (status === 'completed') return ['reopen']
 if (!canProgress) return []
 if (status === 'awaiting_approval') return ['approve','reject']
 return status === 'pending' ? ['start'] : ['submit']
}
export const statusLabel: Record<Status,string> = {pending:'Not started',in_progress:'In progress',awaiting_approval:'Ready for your review',completed:'Completed'}
