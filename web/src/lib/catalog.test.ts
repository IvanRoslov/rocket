import { describe, expect, it } from 'vitest'
import { catalogSourceText, effortsFor, findCatalogModel } from './catalog'
import type { AgentCatalog } from './types'

const claude: AgentCatalog = {
  agent: 'claude-code',
  source: 'cache',
  fetched_at: '2026-10-02T08:00:00Z',
  warning: '',
  models: [
    { id: 'claude-opus-5-5', name: 'Opus 5.5', description: 'Hard', main: true, efforts: ['low', 'high'], default_effort: 'high' },
    { id: 'claude-haiku-4-5-20251001', name: 'Haiku 4.5', description: '', main: true, efforts: [], default_effort: '' },
  ],
}
const agentLevels = ['low', 'medium', 'high', 'xhigh', 'max']

describe('effortsFor', () => {
  it('uses the catalog model’s own levels', () => {
    expect(effortsFor(agentLevels, claude, 'claude-opus-5-5')).toEqual({ efforts: ['low', 'high'], disabled: false })
  })
  it('disables the effort for a model without levels (Haiku)', () => {
    expect(effortsFor(agentLevels, claude, 'claude-haiku-4-5-20251001')).toEqual({ efforts: [], disabled: true })
  })
  it('falls back to the agent’s levels for an alias, a custom or an empty model', () => {
    for (const model of ['opus', 'my-model', '']) {
      expect(effortsFor(agentLevels, claude, model)).toEqual({ efforts: agentLevels, disabled: false })
    }
  })
  it('falls back to the agent’s levels without a catalog', () => {
    expect(effortsFor(agentLevels, undefined, 'claude-opus-5-5')).toEqual({ efforts: agentLevels, disabled: false })
  })
})

describe('findCatalogModel', () => {
  it('matches the id exactly', () => {
    expect(findCatalogModel(claude, 'claude-opus-5-5')?.name).toBe('Opus 5.5')
    expect(findCatalogModel(claude, 'opus')).toBeUndefined()
    expect(findCatalogModel(undefined, 'claude-opus-5-5')).toBeUndefined()
  })
})

describe('catalogSourceText', () => {
  it('names where the list came from', () => {
    expect(catalogSourceText({ ...claude, source: 'cache' })).toBe('из кэша Claude Code')
    expect(catalogSourceText({ ...claude, agent: 'codex', source: 'cli' })).toBe('из Codex CLI')
    expect(catalogSourceText({ ...claude, agent: 'codex', source: 'cache' })).toBe('из кэша Codex')
    expect(catalogSourceText({ ...claude, source: 'builtin' })).toBe('встроенный')
  })
})
