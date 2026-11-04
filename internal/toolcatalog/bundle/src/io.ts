import { readFile as readFileFs, writeFile, mkdir } from 'node:fs/promises'
import { basename, extname, join } from 'node:path'

const MIME: Record<string, string> = {
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.webp': 'image/webp',
  '.mp4': 'video/mp4',
}

export async function toReference(
  ref: string,
  readFile: (p: string) => Promise<Buffer> = (p) => readFileFs(p),
): Promise<string> {
  if (/^https?:\/\//.test(ref) || ref.startsWith('data:')) return ref
  const bytes = await readFile(ref)
  const mime = MIME[extname(ref).toLowerCase()] ?? 'application/octet-stream'
  return `data:${mime};base64,${bytes.toString('base64')}`
}

export function collectOutputs(output: unknown): string[] {
  if (typeof output === 'string') return [output]
  if (Array.isArray(output)) return output.filter((o): o is string => typeof o === 'string')
  return []
}

export function outputName(date: Date, label: string, id: string, url: string, index?: number): string {
  const day = date.toISOString().slice(0, 10)
  const ext = extname(basename(new URL(url).pathname)) || '.bin'
  const suffix = index === undefined ? '' : `-${index + 1}`
  return `${day}-${label}-${id.slice(0, 8)}${suffix}${ext}`
}

const MAGIC: [string, number[]][] = [
  ['png', [0x89, 0x50, 0x4e, 0x47]],
  ['jpeg', [0xff, 0xd8, 0xff]],
  ['gif', [0x47, 0x49, 0x46, 0x38]],
  ['riff', [0x52, 0x49, 0x46, 0x46]],
  ['mp4', [0x66, 0x74, 0x79, 0x70]],
]

export function looksLikeMedia(bytes: Buffer): boolean {
  return MAGIC.some(([name, sig]) => {
    const offset = name === 'mp4' ? 4 : 0
    return sig.every((byte, i) => bytes[offset + i] === byte)
  })
}

const DATA_URI = /^data:([^;,]+)(;[^,]*)?,(.*)$/s

function redactDataUris(value: unknown): unknown {
  if (typeof value === 'string') {
    const match = DATA_URI.exec(value)
    if (!match) return value
    const mime = match[1]
    const isBase64 = match[2]?.includes('base64') ?? false
    const bytes = isBase64 ? Math.floor((match[3]!.length * 3) / 4) : match[3]!.length
    return `<data: ${mime}, ${bytes} bytes>`
  }
  if (Array.isArray(value)) return value.map(redactDataUris)
  if (value && typeof value === 'object') {
    return Object.fromEntries(Object.entries(value).map(([k, v]) => [k, redactDataUris(v)]))
  }
  return value
}

export async function saveOutputs(
  urls: string[],
  opts: {
    dir: string
    label: string
    id: string
    date?: Date
    model: string
    input: Record<string, unknown>
    fetchImpl?: typeof fetch
  },
): Promise<string[]> {
  const doFetch = opts.fetchImpl ?? fetch
  const date = opts.date ?? new Date()
  await mkdir(opts.dir, { recursive: true })

  const files: string[] = []
  for (const [i, url] of urls.entries()) {
    const name = outputName(date, opts.label, opts.id, url, urls.length > 1 ? i : undefined)
    const res = await doFetch(url)
    if (!res.ok) throw new Error(`${res.status}: could not download ${url}`)
    const bytes = Buffer.from(await res.arrayBuffer())
    if (!looksLikeMedia(bytes)) {
      throw new Error(
        `${url} did not serve media: ${bytes.subarray(0, 200).toString('utf8')}`,
      )
    }
    await writeFile(join(opts.dir, name), bytes)
    files.push(name)
  }

  const sidecar = `${date.toISOString().slice(0, 10)}-${opts.label}-${opts.id.slice(0, 8)}.json`
  await writeFile(
    join(opts.dir, sidecar),
    `${JSON.stringify(
      { id: opts.id, model: opts.model, input: redactDataUris(opts.input), outputs: urls, files },
      null,
      2,
    )}\n`,
  )

  return files.map((name) => join(opts.dir, name))
}
