import { writeFile, mkdir } from 'node:fs/promises'
import { join } from 'node:path'
import { loadEnvFile } from 'node:process'
import { resolveLaozhangToken } from './laozhang-auth.ts'
import { generate, DEFAULT_BASE_URL, SIZES, QUALITIES, OUTPUT_FORMATS, BACKGROUNDS, RESPONSE_FORMATS } from './laozhang.ts'
import type { Size, Quality, OutputFormat, Background, ResponseFormat, Generated } from './laozhang.ts'
import { buildPrompt, STYLE_NAMES, SKY_MOODS } from './styles.ts'
import type { StyleName, SkyMood } from './styles.ts'
import { saveOutputs } from './io.ts'

export type Options = {
  model: string
  prompt?: string
  style?: StyleName
  subject?: string
  sky?: SkyMood
  size: Size
  quality: Quality
  outputFormat: OutputFormat
  background: Background
  responseFormat: ResponseFormat
  n: number
  out: string
  label: string
  baseUrl: string
  timeoutMs?: number
  help: boolean
}

const USAGE = `Usage:
  node src/laozhang-cli.ts (--prompt <text> | --style ${STYLE_NAMES.join('|')} --subject <text> [--sky ${SKY_MOODS.join('|')}])
    [--model gpt-image-1] [--size ${SIZES.join('|')}] [--quality ${QUALITIES.join('|')}]
    [--format ${OUTPUT_FORMATS.join('|')}] [--background ${BACKGROUNDS.join('|')}] [--n <1-10>]
    [--response-format ${RESPONSE_FORMATS.join('|')}]
    [--out <dir>] [--label <name>] [--base-url <url>] [--timeout <seconds>]

Some models hand back a link to a file host the proxy has not enabled; --response-format b64_json
takes the bytes inline instead and sidesteps it.
Key: LAOZHANG_API_KEY. Base URL defaults to ${DEFAULT_BASE_URL}, override with --base-url or LAOZHANG_BASE_URL.`

function pick<T extends string>(value: string | undefined, allowed: T[], flag: string, fallback: T): T {
  if (value === undefined) return fallback
  if (!allowed.includes(value as T)) {
    throw new Error(`Unknown ${flag} "${value}". Accepted values: ${allowed.join(', ')}.`)
  }
  return value as T
}

export function parseArgs(argv: string[], env: NodeJS.ProcessEnv = {}): Options {
  const base = {
    model: 'gpt-image-1',
    size: 'auto' as Size,
    quality: 'auto' as Quality,
    outputFormat: 'png' as OutputFormat,
    background: 'auto' as Background,
    responseFormat: 'url' as ResponseFormat,
    n: 1,
    out: 'out',
    label: 'laozhang',
    baseUrl: env.LAOZHANG_BASE_URL?.trim() || DEFAULT_BASE_URL,
    help: false,
  }
  if (argv.includes('--help') || argv.length === 0) return { ...base, help: true }

  const flags = new Map<string, string>()
  for (let i = 0; i < argv.length; i += 2) {
    const key = argv[i]
    const value = argv[i + 1]
    if (!key?.startsWith('--') || value === undefined) {
      throw new Error(`Malformed argument near "${key ?? ''}".`)
    }
    flags.set(key.slice(2), value)
  }

  const styleRaw = flags.get('style')
  const style = styleRaw === undefined ? undefined : pick(styleRaw, [...STYLE_NAMES], '--style', STYLE_NAMES[0]!)
  const prompt = flags.get('prompt')
  if (!style && !prompt) throw new Error('Give either --style or --prompt.')
  if (style && style !== 'portrait' && !flags.get('subject')) throw new Error('--style needs --subject.')

  const skyRaw = flags.get('sky')
  const sky = skyRaw === undefined ? undefined : pick(skyRaw, [...SKY_MOODS], '--sky', SKY_MOODS[0]!)

  const nRaw = flags.get('n')
  const n = nRaw === undefined ? 1 : Number(nRaw)
  if (!Number.isInteger(n) || n < 1 || n > 10) throw new Error(`--n must be a whole number from 1 to 10, got "${nRaw}".`)

  const timeout = flags.get('timeout')
  let timeoutMs: number | undefined
  if (timeout !== undefined) {
    const seconds = Number(timeout)
    if (!Number.isFinite(seconds) || seconds <= 0) {
      throw new Error(`--timeout must be a positive number of seconds, got "${timeout}".`)
    }
    timeoutMs = seconds * 1_000
  }

  return {
    ...base,
    model: flags.get('model') ?? base.model,
    prompt,
    style,
    subject: flags.get('subject'),
    sky,
    size: pick(flags.get('size'), SIZES, '--size', base.size),
    quality: pick(flags.get('quality'), QUALITIES, '--quality', base.quality),
    outputFormat: pick(flags.get('format'), OUTPUT_FORMATS, '--format', base.outputFormat),
    background: pick(flags.get('background'), BACKGROUNDS, '--background', base.background),
    responseFormat: pick(flags.get('response-format'), RESPONSE_FORMATS, '--response-format', base.responseFormat),
    n,
    out: flags.get('out') ?? base.out,
    label: flags.get('label') ?? base.label,
    baseUrl: flags.get('base-url') ?? base.baseUrl,
    timeoutMs,
  }
}

export async function saveGenerated(
  images: Generated[],
  opts: { dir: string; label: string; id: string; model: string; input: Record<string, unknown>; ext: string; date?: Date },
): Promise<string[]> {
  const urls = images.filter((i): i is { url: string } => 'url' in i).map((i) => i.url)
  if (urls.length === images.length) {
    return saveOutputs(urls, { dir: opts.dir, label: opts.label, id: opts.id, model: opts.model, input: opts.input, date: opts.date })
  }

  const date = opts.date ?? new Date()
  const day = date.toISOString().slice(0, 10)
  await mkdir(opts.dir, { recursive: true })
  const files: string[] = []
  for (const [i, image] of images.entries()) {
    if (!('b64' in image)) continue
    const suffix = images.length > 1 ? `-${i + 1}` : ''
    const name = `${day}-${opts.label}-${opts.id.slice(0, 8)}${suffix}.${opts.ext}`
    await writeFile(join(opts.dir, name), Buffer.from(image.b64, 'base64'))
    files.push(name)
  }
  await writeFile(
    join(opts.dir, `${day}-${opts.label}-${opts.id.slice(0, 8)}.json`),
    `${JSON.stringify({ id: opts.id, model: opts.model, input: opts.input, files }, null, 2)}\n`,
  )
  return files.map((name) => join(opts.dir, name))
}

export function loadDotEnv(load: (path: string) => void = loadEnvFile, path = new URL('../.env', import.meta.url).pathname): boolean {
  try {
    load(path)
    return true
  } catch {
    return false
  }
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
    const { token, from } = resolveLaozhangToken(process.env)
    console.error(`token: ${from}`)

    const prompt = opts.style ? buildPrompt(opts.style, opts.subject ?? '', opts.sky) : opts.prompt!

    const controller = new AbortController()
    process.on('SIGINT', () => controller.abort())

    const request = {
      model: opts.model,
      prompt,
      n: opts.n,
      size: opts.size,
      quality: opts.quality,
      output_format: opts.outputFormat,
      background: opts.background,
      response_format: opts.responseFormat,
    }

    const images = await generate(request, {
      token,
      baseUrl: opts.baseUrl,
      timeoutMs: opts.timeoutMs,
      signal: controller.signal,
      log: (line) => console.error(line),
    })

    const id = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 8)}`
    const files = await saveGenerated(images, {
      dir: opts.out,
      label: opts.style ?? opts.label,
      id,
      model: opts.model,
      input: request,
      ext: opts.outputFormat,
    })

    for (const file of files) console.log(file)
    return 0
  } catch (err) {
    console.error((err as Error).message)
    return 1
  }
}

if (import.meta.main) process.exit(await main(process.argv.slice(2)))
