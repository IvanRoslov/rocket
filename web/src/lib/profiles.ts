import { ApiError } from './api'

// Model profiles (task #5026): one-line labels shared by the screens that
// show which profile a session or a registry entry runs. Empty model/effort
// mean "the agent's default" and are never invented.

/** `claude-opus (opus, high)` for a session's launch snapshot; '' for a
 * session started without a profile (legacy launch, pre-feature). */
export function sessionModelLabel(s: { profile?: string; model?: string; effort?: string }): string {
  const detail = [s.model, s.effort].filter(Boolean).join(', ')
  if (!s.profile) return detail
  return detail ? `${s.profile} (${detail})` : s.profile
}

/** `claude-code · opus · high` — what a profile launches. */
export function profileSummary(p: { agent: string; model: string; effort: string }): string {
  return [p.agent, p.model || 'default model', p.effort].filter(Boolean).join(' · ')
}

const PROFILE_ERROR_TEXT: Record<string, string> = {
  profile_exists: 'A profile with this name already exists',
  profile_in_use: 'This profile is a default — pick another default first',
  bad_effort: 'This effort level is not supported for this model',
}

/** Human copy for a registry/allowlist error: the known daemon codes in
 * Russian, anything else as the daemon's own message. */
export function profileErrorText(err: Error): string {
  const code = err instanceof ApiError ? err.code : undefined
  return (code && PROFILE_ERROR_TEXT[code]) || err.message
}
