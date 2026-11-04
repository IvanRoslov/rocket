// Duplicated from docs/brand/imagery.md. That page is the canon; when a frame
// changes there, change it here in the same commit.

export type StyleName = 'cover' | 'spot' | 'house-spot' | 'portrait'
export type SkyMood = 'twilight' | 'midday' | 'storm' | 'teal' | 'golden'

export const STYLE_NAMES: StyleName[] = ['cover', 'spot', 'house-spot', 'portrait']
export const SKY_MOODS: SkyMood[] = ['twilight', 'midday', 'storm', 'teal', 'golden']

const BANS = 'NO text, NO letters, NO numbers, NO signage, NO watermark.'

export const SKIES: Record<SkyMood, string> = {
  twilight: 'a deep twilight sky in indigo and violet, first faint stars, thin crescent moon',
  midday: 'a clear cerulean-blue midday sky, crisp bright white cumulus clouds',
  storm: 'dramatic moody slate blue-grey storm clouds, a single break of warm light',
  teal: 'a cool teal-and-mint dusk sky, soft green-blue atmospheric glow',
  golden: 'a warm golden-hour sky, amber and teal cumulus clouds',
}

const COVER_HEAD =
  'Bold flat screenprint silhouette in deep ink navy with cobalt-blue and a single small coral accent, set against a soft PAINTERLY anime sky in the style of Studio Ghibli scenery:'

const COVER_TAIL =
  ', gentle painterly brushwork. The foreground is a crisp flat graphic subject reading as ONE clear concept; the sky behind is painterly and atmospheric. Generous negative space.'

const SPOT =
  'Flat editorial illustration with a touch of halftone grain, in mid-tone SATURATED colours — cobalt blue, teal and one muted coral accent — that read clearly on BOTH a light and a dark background. No white, no pale fills, no dark navy. Isolated subject on a fully transparent background — no background box, no scene, no ground. NO text, NO letters, NO watermark.'

const HOUSE_SPOT =
  "Create a HOUSE SPOT marketing illustration derived from the Lepsto blog foreground: a bold, clearly readable silhouette in deep ink navy and cobalt blue with ONE restrained coral accent, and gentle painterly brushwork contained within the subject. Simple modeled forms and printed texture; no photorealism, glossy 3D, clay or studio rendering. Square 1:1 composition. One compact subject cluster, fully visible, with generous transparent margins; aim for about 80% of the canvas's longest dimension. Keep the silhouette and key details readable at 64–128 px on the intended surface. Genuine transparent alpha background, including outer corners and gaps. A small attached support or a contained view inside the object is allowed only when needed for the metaphor. No background tile, rectangular sky, full scene, broad ground plane or detached decorations. Navy defines depth, cobalt defines the main forms, coral marks one accent. Tonal variations are allowed within these roles. Small light details may clarify the subject, without forming a pale background tile. Preserve a visible blue edge where navy would disappear against the target surface. No extra accent families. NO UI, NO logos, NO charts, NO product screenshots. NO text, NO letters, NO numbers, NO signage, NO watermark."

const PORTRAIT =
  "Transform this photograph into a bold editorial screenprint portrait while keeping the person's exact likeness — facial features, hairstyle, expression, and glasses if present. Style: flat screenprint / risograph illustration in deep ink navy on a warm bone-paper background, halftone shading in the shadows, crisp confident linework, a subtle cobalt-blue mid-tone, and ONE small coral accent detail (a collar edge or similar). Head-and-shoulders crop with generous negative space. NO text, NO letters, NO watermark. Square composition."

export function buildPrompt(style: StyleName, subject: string, sky: SkyMood = 'twilight'): string {
  if (style === 'portrait') return PORTRAIT
  if (style === 'house-spot') return `Subject: ${subject}. ${HOUSE_SPOT}`
  if (style === 'spot') return `Subject: ${subject}. ${SPOT}`
  return `Subject: ${subject}. ${COVER_HEAD} ${SKIES[sky]}${COVER_TAIL} ${BANS} 16:9.`
}
