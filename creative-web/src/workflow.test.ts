import { describe, expect, it } from 'vitest'
import { actionsFor, moveStage } from './workflow'
import type { StageDefinition } from './types'
const stages: StageDefinition[] = [
 { name:'Brief',description:'Understand',expected_artifacts:'Brief',repeated:false,approval:'stage' },
 { name:'Scenes',description:'Produce',expected_artifacts:'Video',repeated:true,approval:'item' },
]
describe('saved creative process', () => {
 it('reorders template definitions without changing existing snapshots', () => {
  const reordered = moveStage(stages, 1, -1)
  expect(reordered.map(s => s.name)).toEqual(['Scenes','Brief'])
  expect(stages.map(s => s.name)).toEqual(['Brief','Scenes'])
 })
 it('offers human approval only for a submitted review and explicit reopening of completed work', () => {
  expect(actionsFor('awaiting_approval', true)).toEqual(['approve','reject'])
  expect(actionsFor('completed', false)).toEqual(['reopen'])
  expect(actionsFor('pending', false)).toEqual([])
  expect(actionsFor('pending', true)).toEqual(['start'])
  expect(actionsFor('in_progress', true)).toEqual(['submit'])
 })
})
