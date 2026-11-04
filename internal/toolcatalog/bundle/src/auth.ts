import { execFileSync } from 'node:child_process'

export type TokenSource = { token: string; from: 'op' | 'env' }

// Verified 2026-08-30. readOpToken still refuses to shell out on a `<TODO:` value,
// so an unfilled reference degrades to the environment variable rather than
// running `op read` on a placeholder.
export const OP_REFERENCE = 'op://Lessly Platform/Replicate/password'

export function readOpToken(ref: string): string | null {
  if (ref.startsWith('<TODO:')) return null
  try {
    const out = execFileSync('op', ['read', ref], { encoding: 'utf8' })
    return out.trim() || null
  } catch {
    return null
  }
}

export function resolveToken(
  env: NodeJS.ProcessEnv,
  readOp: (ref: string) => string | null = readOpToken,
): TokenSource {
  const fromOp = readOp(OP_REFERENCE)
  if (fromOp) return { token: fromOp, from: 'op' }

  const fromEnv = env.REPLICATE_API_TOKEN?.trim()
  if (fromEnv) return { token: fromEnv, from: 'env' }

  throw new Error(
    `No Replicate token. Set REPLICATE_API_TOKEN, or sign in to 1Password so "op read ${OP_REFERENCE}" works.`,
  )
}
