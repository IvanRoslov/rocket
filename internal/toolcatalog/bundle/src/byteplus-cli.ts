import { loadEnvFile } from 'node:process'
import { createTask, waitForTask, withFlags, buildContent, baseFor, HOST_NAMES } from './byteplus.ts'
import type { Host, Settings } from './byteplus.ts'
import { uploadImage } from './kie.ts'
import { saveOutputs } from './io.ts'

export type Options = {
  model?: string
  prompt?: string
  images: string[]
  lastFrame?: string
  settings: Settings
  host: Host | string
  out: string
  label: string
  status?: string
  host_upload: boolean
  pollMs: number
  timeoutMs: number
  help: boolean
}

const USAGE = `Usage:
  node src/byteplus-cli.ts --model <model-id> --prompt <text>
    [--image <url|path,...>] [--last-frame <url|path>] [--host-upload true]
    [--ratio 16:9] [--resolution 480p|720p|1080p] [--duration <seconds>]
    [--watermark true|false] [--seed <n>] [--camerafixed true|false]
    [--host ${HOST_NAMES.join('|')}|<base-url>]
    [--out <dir>] [--label <name>] [--poll <seconds>] [--timeout <seconds>]

  node src/byteplus-cli.ts --status <taskId>

Settings are NOT JSON fields on this platform: they are appended to the prompt text as
--flags and the model parses them out. This CLI does that for you, so pass them as flags
here and leave the prompt free of them.

--model takes the platform's own model id, which is dated and changes with each release:
Seedance 2.5 is dreamina-seedance-2-5-260628. Read the current one off the model page in
the ModelArk console rather than carrying that one forward.

BytePlus takes public URLs for images. --host-upload true pushes a local file through the
kie.ai file host first and passes the returned link, which needs KIE_API_KEY as well.

Key: BYTEPLUS_API_KEY, read from tools/.env.`

const millis = (value: string | undefined, flag: string, fallbackMs: number): number => {
  if (value === undefined) return fallbackMs
  const n = Number(value)
  if (!Number.isFinite(n) || n <= 0) throw new Error(`${flag} must be a positive number of seconds, got "${value}".`)
  return n * 1000
}

const bool = (value: string | undefined, flag: string): boolean | undefined => {
  if (value === undefined) return undefined
  if (value !== 'true' && value !== 'false') throw new Error(`${flag} must be true or false, got "${value}".`)
  return value === 'true'
}

const int = (value: string | undefined, flag: string): number | undefined => {
  if (value === undefined) return undefined
  const n = Number(value)
  if (!Number.isInteger(n)) throw new Error(`${flag} must be a whole number, got "${value}".`)
  return n
}

export function parseArgs(argv: string[], env: NodeJS.ProcessEnv = {}): Options {
  const base: Options = {
    images: [],
    settings: {},
    host: (env.BYTEPLUS_HOST?.trim() as Host) || 'byteplus',
    out: 'out',
    label: 'byteplus',
    host_upload: false,
    pollMs: 5_000,
    timeoutMs: 1_800_000,
    help: false,
  }
  if (argv.includes('--help') || argv.length === 0) return { ...base, help: true }

  const flags = new Map<string, string>()
  for (let i = 0; i < argv.length; i += 2) {
    const key = argv[i]
    const value = argv[i + 1]
    if (!key?.startsWith('--') || value === undefined) throw new Error(`Malformed argument near "${key ?? ''}".`)
    flags.set(key.slice(2), value)
  }

  const status = flags.get('status')
  const model = flags.get('model')
  const prompt = flags.get('prompt')
  if (!status) {
    if (!model) throw new Error('Missing --model <model-id>, e.g. dreamina-seedance-2-5-260628. Read the current id off the model page in the ModelArk console.')
    if (!prompt) throw new Error('Missing --prompt <text>.')
  }

  const host = flags.get('host') ?? base.host
  baseFor(host)

  const duration = int(flags.get('duration'), '--duration')
  if (duration !== undefined && duration <= 0) throw new Error(`--duration must be positive, got "${duration}".`)

  return {
    ...base,
    model,
    prompt,
    images: (flags.get('image') ?? '').split(',').map((s) => s.trim()).filter(Boolean),
    lastFrame: flags.get('last-frame'),
    settings: {
      ratio: flags.get('ratio'),
      resolution: flags.get('resolution'),
      duration,
      watermark: bool(flags.get('watermark'), '--watermark'),
      seed: int(flags.get('seed'), '--seed'),
      camerafixed: bool(flags.get('camerafixed'), '--camerafixed'),
    },
    host,
    out: flags.get('out') ?? base.out,
    label: flags.get('label') ?? base.label,
    status,
    host_upload: flags.get('host-upload') === 'true',
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

export function resolveByteplusToken(env: NodeJS.ProcessEnv): string {
  const token = env.BYTEPLUS_API_KEY?.trim()
  if (!token) throw new Error('No BytePlus key. Set BYTEPLUS_API_KEY in tools/.env.')
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
    const token = resolveByteplusToken(process.env)

    let taskId = opts.status
    if (!taskId) {
      const resolve = async (ref: string): Promise<string> => {
        if (/^https?:\/\//.test(ref)) return ref
        if (!opts.host_upload) {
          throw new Error(
            `BytePlus takes public URLs for images, and "${ref}" is a local path. Pass --host-upload true to push it through the kie.ai file host first.`,
          )
        }
        const kieToken = process.env.KIE_API_KEY?.trim()
        if (!kieToken) throw new Error('--host-upload true needs KIE_API_KEY as well; that is whose file host does the hosting.')
        return uploadImage(ref, { token: kieToken })
      }

      const images: { url: string; role?: string }[] = []
      for (const ref of opts.images) images.push({ url: await resolve(ref) })
      if (opts.lastFrame) images.push({ url: await resolve(opts.lastFrame), role: 'last_frame' })
      if (images.length > 0) console.error(`${images.length} image(s) referenced`)

      const prompt = withFlags(opts.prompt!, opts.settings)
      const content = buildContent(prompt, images)

      taskId = await createTask(opts.model!, content, {
        token,
        host: opts.host,
        log: (line) => console.error(line),
      })
      console.error(`task: ${taskId}`)
    }

    const done = await waitForTask(taskId, {
      token,
      host: opts.host,
      pollMs: opts.pollMs,
      timeoutMs: opts.timeoutMs,
      log: (line) => console.error(line),
    })

    if (done.urls.length === 0) {
      console.error(`Task ${taskId} succeeded but carried no output URL.`)
      console.error(JSON.stringify(done.raw).slice(0, 400))
      return 1
    }

    const files = await saveOutputs(done.urls, {
      dir: opts.out,
      label: opts.label,
      id: taskId,
      model: opts.model ?? 'unknown',
      input: { prompt: opts.prompt, ...opts.settings },
    })
    for (const file of files) console.log(file)
    return 0
  } catch (err) {
    console.error((err as Error).message)
    return 1
  }
}

if (import.meta.main) process.exit(await main(process.argv.slice(2)))
