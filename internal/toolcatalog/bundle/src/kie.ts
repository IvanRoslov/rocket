import { readFile } from 'node:fs/promises'
import { basename, extname } from 'node:path'

// Verified live on 2026-08-31: the credit, upload, createTask and recordInfo calls below
// were each exercised against the running service, not read off a docs page.
export const API_BASE = 'https://api.kie.ai'
export const UPLOAD_URL = 'https://kieai.redpandaai.co/api/file-base64-upload'
export const UPLOAD_PATH = 'images/user-uploads'

const MIME: Record<string, string> = {
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.webp': 'image/webp',
}

export type TaskState = 'waiting' | 'queuing' | 'generating' | 'success' | 'fail'

export type TaskStatus = {
  state: TaskState
  urls: string[]
  failMsg?: string
  raw: unknown
}

function unwrap(payload: unknown, what: string): Record<string, unknown> {
  const body = payload as { code?: number; msg?: string; data?: unknown }
  if (body?.code !== 200) throw new Error(`${what} failed (${body?.code}): ${body?.msg ?? 'no message'}`)
  return (body.data ?? {}) as Record<string, unknown>
}

async function post(url: string, token: string, body: unknown, fetchImpl: typeof fetch): Promise<unknown> {
  const res = await fetchImpl(url, {
    method: 'POST',
    headers: { authorization: `Bearer ${token}`, 'content-type': 'application/json' },
    body: JSON.stringify(body),
  })
  const text = await res.text()
  if (!res.ok) throw new Error(`${res.status}: ${text.slice(0, 400)}`)
  try {
    return JSON.parse(text)
  } catch {
    throw new Error(`Response was not JSON: ${text.slice(0, 200)}`)
  }
}

export async function credit(token: string, fetchImpl: typeof fetch = fetch): Promise<number> {
  const res = await fetchImpl(`${API_BASE}/api/v1/chat/credit`, {
    headers: { authorization: `Bearer ${token}` },
  })
  const payload = JSON.parse(await res.text())
  const value = (payload as { data?: unknown }).data
  if (typeof value !== 'number') throw new Error(`Credit call returned no number: ${JSON.stringify(payload).slice(0, 200)}`)
  return value
}

// The video endpoint takes URLs, never bytes, so a local card is uploaded first and the
// returned link is what the task actually references.
export async function uploadImage(
  path: string,
  opts: { token: string; fetchImpl?: typeof fetch; read?: (p: string) => Promise<Buffer> },
): Promise<string> {
  const read = opts.read ?? ((p: string) => readFile(p))
  const bytes = await read(path)
  const mime = MIME[extname(path).toLowerCase()]
  if (!mime) throw new Error(`Unsupported image type for ${path}. Use png, jpg or webp.`)

  const payload = await post(
    UPLOAD_URL,
    opts.token,
    {
      base64Data: `data:${mime};base64,${bytes.toString('base64')}`,
      uploadPath: UPLOAD_PATH,
      fileName: basename(path),
    },
    opts.fetchImpl ?? fetch,
  )

  const body = payload as { success?: boolean; msg?: string; data?: { downloadUrl?: string } }
  if (!body?.success) throw new Error(`Upload failed: ${body?.msg ?? JSON.stringify(payload).slice(0, 200)}`)
  const url = body.data?.downloadUrl
  if (!url) throw new Error(`Upload returned no downloadUrl: ${JSON.stringify(payload).slice(0, 200)}`)
  return url
}

export async function createTask(
  model: string,
  input: Record<string, unknown>,
  opts: { token: string; fetchImpl?: typeof fetch },
): Promise<string> {
  const data = unwrap(await post(`${API_BASE}/api/v1/jobs/createTask`, opts.token, { model, input }, opts.fetchImpl ?? fetch), 'createTask')
  const id = data.taskId ?? data.task_id
  if (typeof id !== 'string') throw new Error(`createTask returned no taskId: ${JSON.stringify(data).slice(0, 200)}`)
  return id
}

export function readResultUrls(data: Record<string, unknown>): string[] {
  const collect = (value: unknown): string[] => {
    if (typeof value === 'string' && /^https?:\/\//.test(value)) return [value]
    if (Array.isArray(value)) return value.flatMap(collect)
    if (value && typeof value === 'object') return Object.values(value).flatMap(collect)
    return []
  }
  let result: unknown = data.resultJson ?? data.result ?? data.response
  if (typeof result === 'string') {
    try {
      result = JSON.parse(result)
    } catch {
      return collect(result)
    }
  }
  return collect(result)
}

export async function taskStatus(
  taskId: string,
  opts: { token: string; fetchImpl?: typeof fetch },
): Promise<TaskStatus> {
  const doFetch = opts.fetchImpl ?? fetch
  const res = await doFetch(`${API_BASE}/api/v1/jobs/recordInfo?taskId=${encodeURIComponent(taskId)}`, {
    headers: { authorization: `Bearer ${opts.token}` },
  })
  const text = await res.text()
  if (!res.ok) throw new Error(`${res.status}: ${text.slice(0, 400)}`)
  const data = unwrap(JSON.parse(text), 'recordInfo')
  const state = String(data.state ?? data.status ?? 'waiting').toLowerCase() as TaskState
  return {
    state,
    urls: readResultUrls(data),
    failMsg: typeof data.failMsg === 'string' && data.failMsg ? data.failMsg : undefined,
    raw: data,
  }
}

export async function waitForTask(
  taskId: string,
  opts: {
    token: string
    fetchImpl?: typeof fetch
    pollMs?: number
    timeoutMs?: number
    sleep?: (ms: number) => Promise<void>
    log?: (line: string) => void
  },
): Promise<TaskStatus> {
  const pollMs = opts.pollMs ?? 10_000
  const timeoutMs = opts.timeoutMs ?? 1_800_000
  const sleep = opts.sleep ?? ((ms: number) => new Promise((r) => setTimeout(r, ms)))
  const started = Date.now()
  let last = ''

  for (;;) {
    const status = await taskStatus(taskId, opts)
    if (status.state !== last) {
      opts.log?.(`${taskId}: ${status.state}`)
      last = status.state
    }
    if (status.state === 'success') return status
    if (status.state === 'fail') throw new Error(`Task ${taskId} failed: ${status.failMsg ?? 'no reason given'}`)
    if (Date.now() - started > timeoutMs) {
      throw new Error(`Task ${taskId} still ${status.state} after ${Math.round(timeoutMs / 1000)}s. It may still finish — check with --status ${taskId}.`)
    }
    await sleep(pollMs)
  }
}
