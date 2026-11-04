export type ModelKind = 'image' | 'video'

export type RegistryEntry = {
  slug: string
  kind: ModelKind
  preset: Record<string, unknown>
  fields: string[]
}

// Presets stay empty: a default belongs to the model, and a copy of it here
// would start lying the moment Replicate changes one. `fields` is the verified
// list of input keys, read from GET /v1/models/{owner}/{name} on 2026-08-30 —
// it exists to catch a misspelled --input key before a paid call, not to
// restate the model's defaults.
export const REGISTRY: RegistryEntry[] = [
  {
    slug: 'bytedance/seedance-2.5',
    kind: 'video',
    preset: {},
    fields: [
      'seed', 'image', 'prompt', 'duration', 'watermark', 'resolution',
      'aspect_ratio', 'output_format', 'generate_audio', 'last_frame_image',
      'reference_audios', 'reference_images', 'reference_videos',
    ],
  },
  {
    slug: 'bytedance/seedance-2.0',
    kind: 'video',
    preset: {},
    fields: [
      'seed', 'image', 'prompt', 'duration', 'resolution', 'aspect_ratio',
      'generate_audio', 'last_frame_image', 'reference_audios',
      'reference_images', 'reference_videos',
    ],
  },
  {
    slug: 'alibaba/wan-3',
    kind: 'video',
    preset: {},
    fields: [
      'seed', 'image', 'prompt', 'duration', 'resolution', 'aspect_ratio',
      'negative_prompt', 'enable_prompt_expansion',
    ],
  },
]

// Only for a model the registry knows. An unknown model has no verified field
// list, so nothing is checked rather than something being guessed.
export function checkInput(
  entry: RegistryEntry | undefined,
  input: Record<string, unknown>,
): void {
  if (!entry) return
  const unknown = Object.keys(input).filter((key) => !entry.fields.includes(key))
  if (unknown.length === 0) return
  throw new Error(
    `${entry.slug} does not accept ${unknown.join(', ')}. It accepts: ${entry.fields.join(', ')}.`,
  )
}

export function findModel(slug: string): RegistryEntry | undefined {
  return REGISTRY.find((entry) => entry.slug === slug)
}

export function buildInput(
  entry: RegistryEntry | undefined,
  prompt: string | undefined,
  overrides: Record<string, unknown>,
): Record<string, unknown> {
  return {
    ...(entry?.preset ?? {}),
    ...(prompt === undefined ? {} : { prompt }),
    ...overrides,
  }
}

export function defaultTimeoutMs(kind: ModelKind): number {
  return kind === 'video' ? 600_000 : 120_000
}
