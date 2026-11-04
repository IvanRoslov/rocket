export type Size = '1024x1024' | '1536x1024' | '1024x1536' | 'auto'
export type Quality = 'low' | 'medium' | 'high' | 'auto'
export type OutputFormat = 'png' | 'jpeg' | 'webp'
export type Background = 'transparent' | 'opaque' | 'auto'
export type ResponseFormat = 'url' | 'b64_json'

export const SIZES: Size[] = ['1024x1024', '1536x1024', '1024x1536', 'auto']
export const QUALITIES: Quality[] = ['low', 'medium', 'high', 'auto']
export const OUTPUT_FORMATS: OutputFormat[] = ['png', 'jpeg', 'webp']
export const BACKGROUNDS: Background[] = ['transparent', 'opaque', 'auto']
export const RESPONSE_FORMATS: ResponseFormat[] = ['url', 'b64_json']

// Read from docs.laozhang.ai/en/api-capabilities/gpt-image-1 on 2026-08-31. That page
// gives api2 as the host while the marketing pages give api; LAOZHANG_BASE_URL settles
// it per environment rather than this file guessing.
export const DEFAULT_BASE_URL = 'https://api2.laozhang.ai/v1'

// The docs page above states a 1000-character prompt cap. It is wrong: a 2950-character
// prompt was accepted verbatim by gpt-image-2 on 2026-08-31 (usage reported 900 text
// tokens, so nothing was trimmed). The guard stays, set to OpenAI's own documented
// ceiling, because a silently truncated prompt loses its trailing negative list first
// and the loss is invisible in the output.
export const PROMPT_LIMIT = 32_000

export type ImageRequest = {
  model: string
  prompt: string
  n?: number
  size?: Size
  quality?: Quality
  output_format?: OutputFormat
  output_compression?: number
  background?: Background
  response_format?: ResponseFormat
}

export type Generated = { url: string } | { b64: string }

export function checkPrompt(prompt: string, limit = PROMPT_LIMIT): void {
  const length = [...prompt].length
  if (length === 0) throw new Error('Prompt is empty.')
  if (length > limit) {
    throw new Error(
      `Prompt is ${length} characters; this endpoint accepts ${limit}. Shorten it — truncating a prompt silently drops whichever constraint happens to sit last.`,
    )
  }
}

export function collectImages(payload: unknown): Generated[] {
  const data = (payload as { data?: unknown })?.data
  if (!Array.isArray(data)) return []
  const out: Generated[] = []
  for (const item of data) {
    const url = (item as { url?: unknown })?.url
    if (typeof url === 'string' && url) {
      out.push({ url })
      continue
    }
    const b64 = (item as { b64_json?: unknown })?.b64_json
    if (typeof b64 === 'string' && b64) out.push({ b64 })
  }
  return out
}

export async function generate(
  request: ImageRequest,
  opts: {
    token: string
    baseUrl?: string
    timeoutMs?: number
    signal?: AbortSignal
    fetchImpl?: typeof fetch
    log?: (line: string) => void
  },
): Promise<Generated[]> {
  checkPrompt(request.prompt)

  const doFetch = opts.fetchImpl ?? fetch
  const baseUrl = (opts.baseUrl ?? DEFAULT_BASE_URL).replace(/\/+$/, '')
  const url = `${baseUrl}/images/generations`
  const body: ImageRequest = { response_format: 'url', ...request }

  opts.log?.(`POST ${url} (${request.model}, ${body.quality ?? 'auto'})`)

  const timeout = AbortSignal.timeout(opts.timeoutMs ?? 300_000)
  const signal = opts.signal ? AbortSignal.any([opts.signal, timeout]) : timeout

  const res = await doFetch(url, {
    method: 'POST',
    headers: {
      authorization: `Bearer ${opts.token}`,
      'content-type': 'application/json',
    },
    body: JSON.stringify(body),
    signal,
  })

  const text = await res.text()
  if (!res.ok) throw new Error(`${res.status}: ${text.slice(0, 400)}`)

  let payload: unknown
  try {
    payload = JSON.parse(text)
  } catch {
    throw new Error(`Response was not JSON: ${text.slice(0, 200)}`)
  }

  const error = (payload as { error?: { message?: string } })?.error
  if (error) throw new Error(error.message ?? JSON.stringify(error))

  const images = collectImages(payload)
  if (images.length === 0) throw new Error(`Response carried no image: ${text.slice(0, 300)}`)
  return images
}
