// Contract confirmed by probing the live service on 2026-08-31: both the create and the
// retrieve paths answered AuthenticationError rather than 404, on both hosts below.
// BytePlus is ByteDance's own platform, so Seedance here is first-party rather than
// resold — which is also why its request shape looks nothing like the proxies'.
export const HOSTS = {
  byteplus: 'https://ark.ap-southeast.bytepluses.com/api/v3',
  volcengine: 'https://ark.cn-beijing.volces.com/api/v3',
} as const

export type Host = keyof typeof HOSTS
export const HOST_NAMES = Object.keys(HOSTS) as Host[]

export type TaskState = 'queued' | 'running' | 'succeeded' | 'failed' | 'cancelled'

export type TaskStatus = {
  state: TaskState
  urls: string[]
  error?: string
  raw: Record<string, unknown>
}

// The distinctive thing about this API: generation settings do not travel as JSON fields.
// They are appended to the prompt text as command-line style flags, and the model parses
// them out of the sentence. Everything else on this platform is ordinary JSON, so the
// serialisation lives here rather than being spelled by hand at each call site.
export type Settings = {
  ratio?: string
  resolution?: string
  duration?: number
  watermark?: boolean
  seed?: number
  camerafixed?: boolean
}

const FLAG_ORDER: (keyof Settings)[] = ['ratio', 'resolution', 'duration', 'camerafixed', 'watermark', 'seed']

export function withFlags(prompt: string, settings: Settings): string {
  const trimmed = prompt.trim()
  if (/(^|\s)--(ratio|resolution|duration|watermark|seed|camerafixed)\b/.test(trimmed)) {
    throw new Error(
      'The prompt already carries --flags. Pass settings through the options instead, or drop them from the prompt — sending both leaves the model to pick a winner.',
    )
  }
  const flags = FLAG_ORDER.filter((key) => settings[key] !== undefined).map((key) => `--${key} ${settings[key]}`)
  return flags.length > 0 ? `${trimmed} ${flags.join(' ')}` : trimmed
}

export type ContentItem =
  | { type: 'text'; text: string }
  | { type: 'image_url'; image_url: { url: string }; role?: string }

export function buildContent(prompt: string, images: { url: string; role?: string }[] = []): ContentItem[] {
  const content: ContentItem[] = [{ type: 'text', text: prompt }]
  for (const image of images) {
    content.push({
      type: 'image_url',
      image_url: { url: image.url },
      ...(image.role ? { role: image.role } : {}),
    })
  }
  return content
}

export function readOutputUrls(payload: Record<string, unknown>): string[] {
  const collect = (value: unknown): string[] => {
    if (typeof value === 'string' && /^https?:\/\//.test(value)) return [value]
    if (Array.isArray(value)) return value.flatMap(collect)
    if (value && typeof value === 'object') return Object.values(value).flatMap(collect)
    return []
  }
  return collect(payload.content ?? payload.output ?? payload.result)
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
  const error = (payload as { error?: { message?: string; code?: string } }).error
  if (!res.ok || error) {
    throw new Error(`${res.status}: ${error?.code ? `${error.code}: ` : ''}${error?.message ?? text.slice(0, 300)}`)
  }
  return payload as Record<string, unknown>
}

export function baseFor(host: Host | string): string {
  if (host in HOSTS) return HOSTS[host as Host]
  if (/^https?:\/\//.test(host)) return host.replace(/\/+$/, '')
  throw new Error(`Unknown host "${host}". Use one of ${HOST_NAMES.join(', ')}, or a full base URL.`)
}

export async function createTask(
  model: string,
  content: ContentItem[],
  opts: { token: string; host?: Host | string; fetchImpl?: typeof fetch; log?: (line: string) => void },
): Promise<string> {
  const base = baseFor(opts.host ?? 'byteplus')
  opts.log?.(`POST ${base}/contents/generations/tasks (${model})`)
  const payload = await request(`${base}/contents/generations/tasks`, opts.token, opts.fetchImpl ?? fetch, {
    method: 'POST',
    body: JSON.stringify({ model, content }),
  })
  const id = payload.id
  if (typeof id !== 'string') throw new Error(`createTask returned no id: ${JSON.stringify(payload).slice(0, 200)}`)
  return id
}

export async function taskStatus(
  id: string,
  opts: { token: string; host?: Host | string; fetchImpl?: typeof fetch },
): Promise<TaskStatus> {
  const base = baseFor(opts.host ?? 'byteplus')
  const payload = await request(`${base}/contents/generations/tasks/${encodeURIComponent(id)}`, opts.token, opts.fetchImpl ?? fetch)
  const state = String(payload.status ?? 'queued') as TaskState
  const failure = payload.error as { message?: string; code?: string } | null | undefined
  return {
    state,
    urls: readOutputUrls(payload),
    error: failure ? `${failure.code ?? 'error'}: ${failure.message ?? 'no message'}` : undefined,
    raw: payload,
  }
}

export async function waitForTask(
  id: string,
  opts: {
    token: string
    host?: Host | string
    fetchImpl?: typeof fetch
    pollMs?: number
    timeoutMs?: number
    sleep?: (ms: number) => Promise<void>
    log?: (line: string) => void
  },
): Promise<TaskStatus> {
  const pollMs = opts.pollMs ?? 5_000
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
    if (status.state === 'succeeded') return status
    if (status.state === 'failed' || status.state === 'cancelled') {
      throw new Error(`Task ${id} ${status.state} — ${status.error ?? 'no reason given'}`)
    }
    if (Date.now() - started > timeoutMs) {
      throw new Error(`Task ${id} still ${status.state} after ${Math.round(timeoutMs / 1000)}s. It may still finish — check with --status ${id}.`)
    }
    await sleep(pollMs)
  }
}
