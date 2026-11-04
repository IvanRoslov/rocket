import { loadEnvFile } from 'node:process'
import { credit, uploadImage, createTask, waitForTask, taskStatus } from './kie.ts'
import { saveOutputs } from './io.ts'

const RESOLUTIONS = ['480p', '720p', '1080p'] as const
const ASPECTS = ['1:1', '4:3', '3:4', '16:9', '9:16', '21:9', 'adaptive'] as const
const FORMATS = ['mp4', 'mov'] as const

type Resolution = (typeof RESOLUTIONS)[number]
type Aspect = (typeof ASPECTS)[number]
type Format = (typeof FORMATS)[number]

export type Options = {
  model: string
  prompt?: string
  firstFrame?: string
  lastFrame?: string
  references: string[]
  duration: number
  resolution: Resolution
  aspect: Aspect
  format: Format
  audio: boolean
  out: string
  label: string
  status?: string
  input?: Record<string, unknown>
  credit: boolean
  pollMs: number
  timeoutMs: number
  help: boolean
}

const USAGE = `Usage:
  node src/kie-cli.ts --prompt <text> [--first-frame <path|url>] [--reference <path,path,...>]
    [--model bytedance/seedance-2-5] [--duration 4..30] [--resolution ${RESOLUTIONS.join('|')}]
    [--aspect ${ASPECTS.join('|')}] [--format ${FORMATS.join('|')}] [--audio true|false]
    [--out <dir>] [--label <name>] [--poll <seconds>] [--timeout <seconds>]

  node src/kie-cli.ts --model <name> --input '<json>'   any other model on the market;
                                                        --input replaces the video fields
  node src/kie-cli.ts --status <taskId>     resume watching a task already submitted
  node src/kie-cli.ts --credit true         what the account has left

A first frame and reference images are mutually exclusive — the model takes one scenario
or the other, never both. Local paths are uploaded first; URLs are passed through.
Key: KIE_API_KEY, read from tools/.env.`

const pick = <T extends string>(value: string | undefined, allowed: readonly T[], flag: string, fallback: T): T => {
  if (value === undefined) return fallback
  if (!allowed.includes(value as T)) throw new Error(`Unknown ${flag} "${value}". Accepted values: ${allowed.join(', ')}.`)
  return value as T
}

// Takes seconds from the flag, returns milliseconds. The fallback is already in
// milliseconds — passing it through a seconds-to-ms conversion is what made the first
// run give up after two seconds.
const millis = (value: string | undefined, flag: string, fallbackMs: number): number => {
  if (value === undefined) return fallbackMs
  const n = Number(value)
  if (!Number.isFinite(n) || n <= 0) throw new Error(`${flag} must be a positive number of seconds, got "${value}".`)
  return n * 1000
}

export function parseArgs(argv: string[]): Options {
  const base: Options = {
    model: 'bytedance/seedance-2-5',
    references: [],
    duration: 5,
    resolution: '720p',
    aspect: 'adaptive',
    format: 'mp4',
    audio: false,
    out: 'out',
    label: 'kie',
    credit: false,
    pollMs: 10_000,
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

  let input: Record<string, unknown> | undefined
  const inputRaw = flags.get('input')
  if (inputRaw !== undefined) {
    try {
      input = JSON.parse(inputRaw) as Record<string, unknown>
    } catch {
      throw new Error(`--input is not valid JSON: ${inputRaw}`)
    }
  }

  const status = flags.get('status')
  const wantsCredit = flags.get('credit') === 'true'
  const prompt = flags.get('prompt')
  if (!status && !wantsCredit && !prompt && !input) {
    throw new Error('Give --prompt, or --input <json>, or --status <taskId>, or --credit true.')
  }

  const firstFrame = flags.get('first-frame')
  const references = (flags.get('reference') ?? '').split(',').map((s) => s.trim()).filter(Boolean)
  if (firstFrame && references.length > 0) {
    throw new Error('--first-frame and --reference are mutually exclusive; the model accepts one scenario or the other.')
  }
  if (flags.get('last-frame') && !firstFrame) {
    throw new Error('--last-frame cannot travel alone; give --first-frame with it.')
  }

  const durationRaw = flags.get('duration')
  const duration = durationRaw === undefined ? base.duration : Number(durationRaw)
  if (!Number.isInteger(duration) || duration < 4 || duration > 30) {
    throw new Error(`--duration must be a whole number of seconds from 4 to 30, got "${durationRaw}".`)
  }

  const audioRaw = flags.get('audio')
  if (audioRaw !== undefined && audioRaw !== 'true' && audioRaw !== 'false') {
    throw new Error(`--audio must be true or false, got "${audioRaw}".`)
  }

  return {
    ...base,
    model: flags.get('model') ?? base.model,
    prompt,
    firstFrame,
    lastFrame: flags.get('last-frame'),
    references,
    duration,
    resolution: pick(flags.get('resolution'), RESOLUTIONS, '--resolution', base.resolution),
    aspect: pick(flags.get('aspect'), ASPECTS, '--aspect', base.aspect),
    format: pick(flags.get('format'), FORMATS, '--format', base.format),
    audio: audioRaw === 'true',
    out: flags.get('out') ?? base.out,
    label: flags.get('label') ?? base.label,
    status,
    input,
    credit: wantsCredit,
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

export function resolveKieToken(env: NodeJS.ProcessEnv): string {
  const token = env.KIE_API_KEY?.trim()
  if (!token) throw new Error('No kie.ai key. Set KIE_API_KEY in tools/.env.')
  return token
}

const isUrl = (s: string) => /^https?:\/\//.test(s) || s.startsWith('asset://')

export async function main(argv: string[]): Promise<number> {
  loadDotEnv()
  let opts: Options
  try {
    opts = parseArgs(argv)
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
    const token = resolveKieToken(process.env)

    if (opts.credit) {
      console.log(await credit(token))
      return 0
    }

    let taskId = opts.status
    if (!taskId) {
      const resolve = async (ref: string) => (isUrl(ref) ? ref : uploadImage(ref, { token }))

      // A raw --input replaces the video fields wholesale: an image model rejects
      // duration and generate_audio outright, and silently merging them would turn a
      // clear refusal into a confusing one.
      const input: Record<string, unknown> = opts.input
        ? { ...(opts.prompt ? { prompt: opts.prompt } : {}), ...opts.input }
        : {
            prompt: opts.prompt,
            duration: opts.duration,
            resolution: opts.resolution,
            aspect_ratio: opts.aspect,
            output_format: opts.format,
            generate_audio: opts.audio,
          }
      if (opts.firstFrame) {
        input.first_frame_url = await resolve(opts.firstFrame)
        console.error(`first frame: ${input.first_frame_url}`)
        if (opts.lastFrame) input.last_frame_url = await resolve(opts.lastFrame)
      }
      if (opts.references.length > 0) {
        input.reference_image_urls = await Promise.all(opts.references.map(resolve))
        console.error(`${opts.references.length} reference image(s) uploaded`)
      }

      taskId = await createTask(opts.model, input, { token })
      console.error(`task: ${taskId}`)
    }

    const done = await waitForTask(taskId, {
      token,
      pollMs: opts.pollMs,
      timeoutMs: opts.timeoutMs,
      log: (line) => console.error(line),
    })

    if (done.urls.length === 0) {
      console.error(`Task ${taskId} succeeded but carried no downloadable output.`)
      console.error(JSON.stringify(done.raw).slice(0, 400))
      return 1
    }

    const files = await saveOutputs(done.urls, {
      dir: opts.out,
      label: opts.label,
      id: taskId,
      model: opts.model,
      input: { prompt: opts.prompt, duration: opts.duration, resolution: opts.resolution },
    })
    for (const file of files) console.log(file)
    return 0
  } catch (err) {
    console.error((err as Error).message)
    return 1
  }
}

if (import.meta.main) process.exit(await main(process.argv.slice(2)))
