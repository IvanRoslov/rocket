const BASE = 'https://api.replicate.com'
const TERMINAL = new Set(['succeeded', 'failed', 'canceled'])

export type Prediction = { id: string; status: string; output?: unknown; error?: unknown }

export type RunOptions = {
  token: string
  timeoutMs: number
  fetchImpl?: typeof fetch
  sleep?: (ms: number) => Promise<void>
  now?: () => number
  log?: (line: string) => void
  signal?: AbortSignal
}

async function readError(res: Response): Promise<string> {
  const text = await res.text()
  try {
    const body = JSON.parse(text) as { detail?: string; title?: string }
    return body.detail ?? body.title ?? text
  } catch {
    return text
  }
}

export async function run(
  model: string,
  input: Record<string, unknown>,
  opts: RunOptions,
): Promise<Prediction> {
  const doFetch = opts.fetchImpl ?? fetch
  const sleep = opts.sleep ?? ((ms: number) => new Promise((r) => setTimeout(r, ms)))
  const now = opts.now ?? Date.now
  const log = opts.log ?? (() => {})

  const headers = {
    authorization: `Bearer ${opts.token}`,
    'content-type': 'application/json',
    prefer: 'wait',
  }

  const created = await doFetch(`${BASE}/v1/models/${model}/predictions`, {
    method: 'POST',
    headers,
    body: JSON.stringify({ input }),
    signal: opts.signal,
  })
  if (!created.ok) throw new Error(`${created.status}: ${await readError(created)}`)

  let prediction = (await created.json()) as Prediction
  const deadline = now() + opts.timeoutMs
  let wait = 1_000

  while (!TERMINAL.has(prediction.status)) {
    if (now() > deadline) {
      throw new Error(
        `timeout: prediction ${prediction.id} still ${prediction.status} after ${opts.timeoutMs}ms`,
      )
    }
    log(`${prediction.id}: ${prediction.status}`)
    await sleep(wait)
    wait = Math.min(wait * 2, 10_000)

    const polled = await doFetch(`${BASE}/v1/predictions/${prediction.id}`, {
      headers,
      signal: opts.signal,
    })
    if (!polled.ok) throw new Error(`${polled.status}: ${await readError(polled)}`)
    prediction = (await polled.json()) as Prediction
  }

  if (prediction.status !== 'succeeded') {
    throw new Error(`${prediction.status}: ${String(prediction.error ?? 'no detail given')}`)
  }
  return prediction
}
