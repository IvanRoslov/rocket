import { resolveToken } from './auth.ts'
import { run } from './replicate.ts'
import { findModel, buildInput, defaultTimeoutMs, checkInput } from './registry.ts'
import { buildPrompt } from './styles.ts'
import type { ModelKind } from './registry.ts'
import type { StyleName, SkyMood } from './styles.ts'
import { toReference, collectOutputs, saveOutputs } from './io.ts'
import { STYLE_NAMES, SKY_MOODS } from './styles.ts'

export type Options = {
  kind: ModelKind
  model: string
  style?: StyleName
  subject?: string
  sky?: SkyMood
  prompt?: string
  image?: string
  out: string
  input: Record<string, unknown>
  timeoutMs?: number
  help: boolean
}

const USAGE = `Usage:
  node src/cli.ts image --model <owner/name> (--style ${STYLE_NAMES.join('|')} --subject <text> [--sky twilight|midday|storm|teal|golden] | --prompt <text>) [--image <path|url>] [--input '<json>'] [--out <dir>] [--timeout <seconds>]
  node src/cli.ts video --model <owner/name> (--prompt <text> | --style ... --subject ...) [--image <path|url>] [--input '<json>'] [--out <dir>] [--timeout <seconds>]

Known models: bytedance/seedance-2.5, bytedance/seedance-2.0, alibaba/wan-3 (video).
Their input keys are verified, so a misspelled --input key fails before the call.
Any other Replicate model works through --model with --input, unchecked.
Token: 1Password if the reference in src/auth.ts is filled in, otherwise REPLICATE_API_TOKEN.`

export function parseArgs(argv: string[]): Options {
  if (argv.includes('--help') || argv.length === 0) {
    return { kind: 'image', model: '', out: 'out', input: {}, help: true }
  }

  const [command, ...rest] = argv
  if (command !== 'image' && command !== 'video') {
    throw new Error(`Unknown command "${command}". Use image or video.`)
  }

  const flags = new Map<string, string>()
  for (let i = 0; i < rest.length; i += 2) {
    const key = rest[i]
    const value = rest[i + 1]
    if (!key?.startsWith('--') || value === undefined) {
      throw new Error(`Malformed argument near "${key ?? ''}".`)
    }
    flags.set(key.slice(2), value)
  }

  const model = flags.get('model')
  if (!model) throw new Error('Missing --model <owner/name>.')

  const styleRaw = flags.get('style')
  if (styleRaw !== undefined && !STYLE_NAMES.includes(styleRaw as StyleName)) {
    throw new Error(`Unknown --style "${styleRaw}". Accepted values: ${STYLE_NAMES.join(', ')}.`)
  }
  const style = styleRaw as StyleName | undefined
  const prompt = flags.get('prompt')
  if (!style && !prompt) throw new Error('Give either --style or --prompt.')
  if (style && style !== 'portrait' && !flags.get('subject')) {
    throw new Error('--style needs --subject.')
  }

  const skyRaw = flags.get('sky')
  if (skyRaw !== undefined && !SKY_MOODS.includes(skyRaw as SkyMood)) {
    throw new Error(`Unknown --sky "${skyRaw}". Accepted values: ${SKY_MOODS.join(', ')}.`)
  }
  const sky = skyRaw as SkyMood | undefined

  let input: Record<string, unknown> = {}
  const raw = flags.get('input')
  if (raw !== undefined) {
    try {
      input = JSON.parse(raw) as Record<string, unknown>
    } catch {
      throw new Error(`--input is not valid JSON: ${raw}`)
    }
  }

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
    kind: command,
    model,
    style,
    subject: flags.get('subject'),
    sky,
    prompt,
    image: flags.get('image'),
    out: flags.get('out') ?? 'out',
    input,
    timeoutMs,
    help: false,
  }
}

export async function main(argv: string[]): Promise<number> {
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
    const { token, from } = resolveToken(process.env)
    console.error(`token: ${from}`)

    const entry = findModel(opts.model)
    const prompt = opts.style
      ? buildPrompt(opts.style, opts.subject ?? '', opts.sky)
      : opts.prompt

    const overrides = { ...opts.input }
    if (opts.image) overrides.image = await toReference(opts.image)

    const input = buildInput(entry, prompt, overrides)
    checkInput(entry, input)
    const timeoutMs = opts.timeoutMs ?? defaultTimeoutMs(entry?.kind ?? opts.kind)

    const controller = new AbortController()
    process.on('SIGINT', () => controller.abort())

    const prediction = await run(opts.model, input, {
      token,
      timeoutMs,
      signal: controller.signal,
      log: (line) => console.error(line),
    })

    const outputs = collectOutputs(prediction.output)
    if (outputs.length === 0) {
      throw new Error(
        `Prediction ${prediction.id} succeeded but carried no downloadable output.`,
      )
    }

    const label = opts.style ?? opts.model.split('/')[1] ?? 'output'
    const files = await saveOutputs(outputs, {
      dir: opts.out,
      label,
      id: prediction.id,
      model: opts.model,
      input,
    })

    for (const file of files) console.log(file)
    return 0
  } catch (err) {
    console.error((err as Error).message)
    return 1
  }
}

if (import.meta.main) process.exit(await main(process.argv.slice(2)))
