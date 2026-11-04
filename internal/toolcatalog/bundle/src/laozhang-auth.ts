import { readOpToken } from './auth.ts'

export type TokenSource = { token: string; from: 'op' | 'env' }

// Fill this in once the key has a home in the vault; readOpToken refuses to shell out
// on a `<TODO:` value, so until then the environment variable carries it.
export const OP_REFERENCE = '<TODO: op:// reference for the laozhang.ai API key>'

export function resolveLaozhangToken(
  env: NodeJS.ProcessEnv,
  readOp: (ref: string) => string | null = readOpToken,
): TokenSource {
  const fromOp = readOp(OP_REFERENCE)
  if (fromOp) return { token: fromOp, from: 'op' }

  const fromEnv = env.LAOZHANG_API_KEY?.trim()
  if (fromEnv) return { token: fromEnv, from: 'env' }

  throw new Error('No laozhang.ai key. Set LAOZHANG_API_KEY.')
}
