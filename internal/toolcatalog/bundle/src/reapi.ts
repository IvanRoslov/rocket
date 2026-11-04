// Contract read from https://reapi.ai/llms.txt and each model's own llms.txt on
// 2026-08-31. reAPI publishes an agent-readable schema per model and says outright that
// the catalog moves, so this file holds only what is stable across models — the two
// generation endpoints, the task lifecycle, and the shape of a result.
export const API_BASE = 'https://reapi.ai/api/v1'

export type TaskState = 'processing' | 'completed' | 'failed'

export type TaskStatus = {
  state: TaskState
  urls: string[]
  error?: string
  raw: Record<string, unknown>
}

export type Kind = 'image' | 'video' | 'audio'

const PATHS: Record<Kind, string> = {
  image: '/images/generations',
  video: '/videos/generations',
  audio: '/audio/generations',
}

export function endpointFor(kind: Kind, base = API_BASE): string {
  return `${base.replace(/\/+$/, '')}${PATHS[kind]}`
}

// Every media input is a public HTTP(S) URL; base64 and data: URIs are rejected
// platform-wide, so a local card has to be hosted somewhere before it can be referenced.
export function checkMediaUrls(urls: string[]): void {
  const bad = urls.filter((u) => !/^https?:\/\//.test(u))
  if (bad.length > 0) {
    throw new Error(
      `reAPI takes public URLs for media, not local paths or data URIs: ${bad.join(', ')}. Host the file first — the kie.ai uploader in kie.ts returns a usable link.`,
    )
  }
}

export function readOutputUrls(payload: Record<string, unknown>): string[] {
  const collect = (value: unknown): string[] => {
    if (typeof value === 'string' && /^https?:\/\//.test(value)) return [value]
    if (Array.isArray(value)) return value.flatMap(collect)
    if (value && typeof value === 'object') return Object.values(value).flatMap(collect)
    return []
  }
  return collect(payload.output)
}

async function request(url: string, token: string, fetchImpl: typeof fetch, init?: RequestInit): Promise<Record<string, unknown>> {
  const res = await fetchImpl(url, {
    ...init,
    headers: { authorization: `Bearer ${token}`, 'content-type': 'application/json', ...(init?.headers ?? {}) },
  })
  const text = await res.text()
  let payload: unknown
  try {
    payload = JSON.parse(text)
  } catch {
    throw new Error(`${res.status}: response was not JSON: ${text.slice(0, 200)}`)
  }
  if (!res.ok) {
    const error = (payload as { error?: { message?: string; code?: string } }).error
    throw new Error(`${res.status}: ${error?.message ?? text.slice(0, 300)}`)
  }
  return payload as Record<string, unknown>
}

export async function submit(
  kind: Kind,
  body: Record<string, unknown>,
  opts: { token: string; base?: string; fetchImpl?: typeof fetch; log?: (line: string) => void },
): Promise<string> {
  const url = endpointFor(kind, opts.base)
  opts.log?.(`POST ${url} (${String(body.model ?? 'no model')})`)
  const payload = await request(url, opts.token, opts.fetchImpl ?? fetch, {
    method: 'POST',
    body: JSON.stringify(body),
  })
  const id = payload.id
  if (typeof id !== 'string') throw new Error(`Submission returned no task id: ${JSON.stringify(payload).slice(0, 200)}`)
  return id
}

export async function taskStatus(
  id: string,
  opts: { token: string; base?: string; fetchImpl?: typeof fetch },
): Promise<TaskStatus> {
  const base = (opts.base ?? API_BASE).replace(/\/+$/, '')
  const payload = await request(`${base}/tasks/${encodeURIComponent(id)}`, opts.token, opts.fetchImpl ?? fetch)
  const state = String(payload.status ?? 'processing') as TaskState
  const error = payload.error as { message?: string; code?: string } | null | undefined
  return {
    state,
    urls: readOutputUrls(payload),
    error: error ? `${error.code ?? 'error'}: ${error.message ?? 'no message'}` : undefined,
    raw: payload,
  }
}

export async function waitForTask(
  id: string,
  opts: {
    token: string
    base?: string
    fetchImpl?: typeof fetch
    // The published rate limit is 5 requests a second per user, polling included, and
    // their own guidance is to poll every 1-2 seconds. 2s stays clear of the limit.
    pollMs?: number
    timeoutMs?: number
    sleep?: (ms: number) => Promise<void>
    log?: (line: string) => void
  },
): Promise<TaskStatus> {
  const pollMs = opts.pollMs ?? 2_000
  const timeoutMs = opts.timeoutMs ?? 1_800_000
  const sleep = opts.sleep ?? ((ms: number) => new Promise((r) => setTimeout(r, ms)))
  const started = Date.now()
  let last = ''

  for (;;) {
    const status = await taskStatus(id, opts)
    if (status.state !== last) {
      opts.log?.(`${id}: ${status.state}`)
      last = status.state
    }
    if (status.state === 'completed') return status
    if (status.state === 'failed') {
      throw new Error(`Task ${id} failed — ${status.error ?? 'no reason given'}. Reserved credits are refunded automatically.`)
    }
    if (Date.now() - started > timeoutMs) {
      throw new Error(`Task ${id} still ${status.state} after ${Math.round(timeoutMs / 1000)}s. It may still finish — check with --status ${id}.`)
    }
    await sleep(pollMs)
  }
}
