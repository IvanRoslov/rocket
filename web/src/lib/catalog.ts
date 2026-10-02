import type { AgentCatalog, CatalogModel } from './types'

// Model catalog (choose-model v2): which models an agent offers and which
// effort levels each takes. The daemon validates efforts the same way —
// a catalog model by its own levels, anything else by the agent's.

/** The catalog entry whose id is exactly `model`. */
export function findCatalogModel(cat: AgentCatalog | undefined, model: string): CatalogModel | undefined {
  if (!model) return undefined
  return cat?.models.find((m) => m.id === model)
}

/** Effort levels a profile of this agent+model may pick. A catalog model
 * without levels (Haiku) disables the choice. */
export function effortsFor(
  agentEfforts: string[],
  cat: AgentCatalog | undefined,
  model: string,
): { efforts: string[]; disabled: boolean } {
  const m = findCatalogModel(cat, model)
  if (!m) return { efforts: agentEfforts, disabled: false }
  return { efforts: m.efforts, disabled: m.efforts.length === 0 }
}

const AGENT_TITLE: Record<string, string> = { 'claude-code': 'Claude Code', codex: 'Codex' }

/** «из Codex CLI» / «из кэша Claude Code» / «встроенный». */
export function catalogSourceText(c: AgentCatalog): string {
  const title = AGENT_TITLE[c.agent] ?? c.agent
  if (c.source === 'cli') return `из ${title} CLI`
  if (c.source === 'cache') return `из кэша ${title}`
  return 'встроенный'
}
