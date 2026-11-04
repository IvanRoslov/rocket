import { loadEnvFile } from 'node:process'
import { execFileSync } from 'node:child_process'
import { randomBytes } from 'node:crypto'
import { extname } from 'node:path'
import { submit, waitForTask, checkMediaUrls, API_BASE } from './reapi.ts'
import type { Kind } from './reapi.ts'
import { uploadImage } from './kie.ts'
import { saveOutputs } from './io.ts'

const KINDS: Kind[] = ['image', 'video', 'audio']

export type Options = {
  kind: Kind
  model?: string
  prompt?: string
  images: string[]
  input: Record<string, unknown>
  out: string
  label: string
  base: string
  status?: string
  host: false | 'kie' | 'gcs'
  pollMs: number
  timeoutMs: number
  help: boolean
}

const USAGE = `Usage:
  node src/reapi-cli.ts <${KINDS.join('|')}> --model <wire-id> --prompt <text>
    [--image <url|path,...>] [--input '<json>'] [--host gcs|true]
    [--out <dir>] [--label <name>] [--poll <seconds>] [--timeout <seconds>]

  node src/reapi-cli.ts --status <taskId>

Model ids are the wire ids, not the slugs: seedance-2-5 is "doubao-seedance-2.5-face".
Fetch https://reapi.ai/models/<slug>/llms.txt for a model's current schema and price —
the catalog moves, so nothing about a model is hardcoded here.

reAPI takes public URLs for media; base64 and data: URIs are rejected. --host gcs
uploads each local path to gs://lepsto-marketing-gen-refs under a random name (public read,
deleted after 1 day; needs gcloud logged in). --host true uses the kie.ai file host
instead, which needs KIE_API_KEY as well.

Key: REAPI_API_KEY (an rk_live_... token), read from tools/.env.`

export const GCS_BUCKET = 'lepsto-marketing-gen-refs'

const hostFlag = (value: string | undefined): Options['host'] => {
  if (value === undefined || value === 'false') return false
  if (value === 'true' || value === 'kie') return 'kie'
  if (value === 'gcs') return 'gcs'
  throw new Error(`--host must be gcs, true or false, got "${value}".`)
}

// Copies a local file to the refs bucket under an unguessable name and returns its public URL.
export function uploadToGcs(path: string, run: typeof execFileSync = execFileSync): string {
  const name = randomBytes(12).toString('hex') + extname(path).toLowerCase()
  run('gcloud', ['storage', 'cp', '--quiet', path, `gs://${GCS_BUCKET}/${name}`], { stdio: ['ignore', 'ignore', 'pipe'] })
  return `https://storage.googleapis.com/${GCS_BUCKET}/${name}`
}

const millis = (value: string | undefined, flag: string, fallbackMs: number): number => {
  if (value === undefined) return fallbackMs
  const n = Number(value)
  if (!Number.isFinite(n) || n <= 0) throw new Error(`${flag} must be a positive number of seconds, got "${value}".`)
  return n * 1000
}

export function parseArgs(argv: string[], env: NodeJS.ProcessEnv = {}): Options {
  const base: Options = {
    kind: 'video',
    images: [],
    input: {},
    out: 'out',
    label: 'reapi',
    base: env.REAPI_BASE_URL?.trim() || API_BASE,
    host: false,
    pollMs: 2_000,
    timeoutMs: 1_800_000,
    help: false,
  }
  if (argv.includes('--help') || argv.length === 0) return { ...base, help: true }

  let rest = argv
  let kind = base.kind
  if (!argv[0]!.startsWith('--')) {
    const first = argv[0]!
    if (!KINDS.includes(first as Kind)) {
      throw new Error(`Unknown command "${first}". Use one of: ${KINDS.join(', ')}.`)
    }
    kind = first as Kind
    rest = argv.slice(1)
  }

  const flags = new Map<string, string>()
  for (let i = 0; i < rest.length; i += 2) {
    const key = rest[i]
    const value = rest[i + 1]
    if (!key?.startsWith('--') || value === undefined) throw new Error(`Malformed argument near "${key ?? ''}".`)
    flags.set(key.slice(2), value)
  }

  let input: Record<string, unknown> = {}
  const raw = flags.get('input')
  if (raw !== undefined) {
    try {
      input = JSON.parse(raw) as Record<string, unknown>
    } catch {
      throw new Error(`--input is not valid JSON: ${raw}`)
    }
  }

  const status = flags.get('status')
  const model = flags.get('model')
  const prompt = flags.get('prompt')
  if (!status) {
    if (!model) throw new Error('Missing --model <wire-id>. Fetch https://reapi.ai/models/<slug>/llms.txt for it.')
    if (!prompt && input.prompt === undefined) throw new Error('Give --prompt, or a prompt inside --input.')
  }

  return {
    ...base,
    kind,
    model,
    prompt,
    images: (flags.get('image') ?? '').split(',').map((s) => s.trim()).filter(Boolean),
    input,
    out: flags.get('out') ?? base.out,
    label: flags.get('label') ?? base.label,
    base: flags.get('base-url') ?? base.base,
    status,
    host: hostFlag(flags.get('host')),
    pollMs: millis(flags.get('poll'), '--poll', base.pollMs),
    timeoutMs: millis(flags.get('timeout'), '--timeout', base.timeoutMs),
  }
}

export function loadDotEnv(load: (path: string) => void = loadEnvFile, path = new URL('../.env', import.meta.url).pathname): boolean {
  try {
    load(path)
    return true
  } catch {
    return false
  }
}

export function resolveReapiToken(env: NodeJS.ProcessEnv): string {
  const token = env.REAPI_API_KEY?.trim()
  if (!token) throw new Error('No reAPI key. Set REAPI_API_KEY in tools/.env — create one at https://reapi.ai/settings/apikeys.')
  return token
}

export async function main(argv: string[]): Promise<number> {
  loadDotEnv()
  let opts: Options
  try {
    opts = parseArgs(argv, process.env)
  } catch (err) {
    console.error((err as Error).message)
    console.error(USAGE)
    return 1
  }
  if (opts.help) {
    console.log(USAGE)
    return 0
  }

  try {
    const token = resolveReapiToken(process.env)

    let taskId = opts.status
    if (!taskId) {
      let images = opts.images
      if (opts.host === 'gcs') {
        images = images.map((ref) => (/^https?:\/\//.test(ref) ? ref : uploadToGcs(ref)))
        console.error(`${images.length} image(s) hosted on gs://${GCS_BUCKET}`)
      } else if (opts.host) {
        const kieToken = process.env.KIE_API_KEY?.trim()
        if (!kieToken) throw new Error('--host true needs KIE_API_KEY as well; that is whose file host does the hosting.')
        images = await Promise.all(
          images.map(async (ref) => (/^https?:\/\//.test(ref) ? ref : uploadImage(ref, { token: kieToken }))),
        )
        console.error(`${images.length} image(s) hosted`)
      }
      checkMediaUrls(images)

      const body: Record<string, unknown> = {
        model: opts.model,
        ...(opts.prompt ? { prompt: opts.prompt } : {}),
        ...(images.length > 0 ? { image_urls: images } : {}),
        ...opts.input,
      }

      taskId = await submit(opts.kind, body, { token, base: opts.base, log: (line) => console.error(line) })
      console.error(`task: ${taskId}`)
    }

    const done = await waitForTask(taskId, {
      token,
      base: opts.base,
      pollMs: opts.pollMs,
      timeoutMs: opts.timeoutMs,
      log: (line) => console.error(line),
    })

    if (done.urls.length === 0) {
      console.error(`Task ${taskId} completed but carried no output URL.`)
      console.error(JSON.stringify(done.raw).slice(0, 400))
      return 1
    }

    const files = await saveOutputs(done.urls, {
      dir: opts.out,
      label: opts.label,
      id: taskId,
      model: opts.model ?? 'unknown',
      input: { prompt: opts.prompt, ...opts.input },
    })
    for (const file of files) console.log(file)
    return 0
  } catch (err) {
    console.error((err as Error).message)
    return 1
  }
}

if (import.meta.main) process.exit(await main(process.argv.slice(2)))
