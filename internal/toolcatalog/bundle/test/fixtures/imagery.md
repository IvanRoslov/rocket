---
canonical: true
---
> Migrated from `lepsto/lepsto-landing` (`publish/` skill and its `lib/cover-art-direction.ts`,
> `generate-spot.ts`, `generate-portrait.ts`) 2026-08-26. This page is the **canon for the styles**.
> The frames below are duplicated in `tools/src/styles.ts` — change both in the same commit.
> To generate from these frames, use `tools/` (see [[docs/toolbox]]); the older OpenAI
> `gpt-image-1` path and its 1Password lookup still live in lepsto-landing.

# Imagery

AI-generated image styles for Lepsto content. Cover, House spot and portrait each
have a fixed prompt frame you drop a subject into. The older universal spot frame
is retained below as historical reference; use House spot for new spot illustrations. Part of [[INDEX]]; siblings — [[docs/brand/tokens]],
[[docs/brand/logo]].

## Universal bans

Every prompt ends with:

> NO text, NO letters, NO numbers, NO signage, NO watermark.

The subject reads as **one clear concept** with generous negative space. No user
interface, no logos, no charts, no product screenshots inside generated art.

## 1. Cover — house screenprint (blog covers, hero art)

The house style for every blog cover, short or long-form. Flat deep-navy
silhouette on a bright painterly sky with one small coral accent (existing
covers: a coral doorway, a coral key, a coral lantern).

Prompt frame (`Subject: {subject}.` + head + sky + tail + bans + `16:9.`):

> **Head:** Bold flat screenprint silhouette in deep ink navy with cobalt-blue and a single small coral accent, set against a soft PAINTERLY anime sky in the style of Studio Ghibli scenery: `{sky}`
>
> **Tail:** , gentle painterly brushwork. The foreground is a crisp flat graphic subject reading as ONE clear concept; the sky behind is painterly and atmospheric. Generous negative space.

Sky moods (pick to match the post's tone):

| Mood       | Sky |
|------------|-----|
| `twilight` | deep twilight sky in indigo and violet, first faint stars, thin crescent moon |
| `midday`   | clear cerulean-blue midday sky, crisp bright white cumulus clouds |
| `storm`    | dramatic moody slate blue-grey storm clouds, a single break of warm light |
| `teal`     | cool teal-and-mint dusk sky, soft green-blue atmospheric glow |
| `golden`   | warm golden-hour sky, amber and teal cumulus clouds |

Worked example (subject: coral key, sky: `twilight`) — swap only the `Subject:`
phrase and the sky line, keep the rest of the frame verbatim:

> Subject: a hand holding up a keyring with one key picked out in coral. Bold flat screenprint silhouette in deep ink navy with cobalt-blue and a single small coral accent, set against a soft PAINTERLY anime sky in the style of Studio Ghibli scenery: a deep twilight sky in indigo and violet with the first faint stars and a thin crescent moon, gentle painterly brushwork. The foreground is a crisp flat graphic subject reading as ONE clear concept; the sky behind is painterly and atmospheric. Generous negative space. NO text, NO letters, NO numbers, NO signage, NO watermark. 16:9.

**Do not use the `rendered` style for blog covers.** It hardcodes a dark studio
backdrop, ignores the sky, and reads as off-brand and "too dark" next to the flat
bright house look. It exists for other art, not covers:

> A cinematic 3D render, editorial key art: a single subject on a dark studio backdrop in deep ink navy and cobalt, lit with soft directional studio light and one warm coral rim-light accent, shallow depth of field with gentle bokeh, subtle film grain, premium and understated.

## 2. Spot illustration (transparent, both themes)

**Retired from the current catalog.** The keyring example has been replaced by
House spot. Do not use this frame for new spot illustrations; follow section 2b
and [[storybook/src/stories/images/spot-illustrations/README]]. The unchanged frame below
records the older style for historical assets and existing tooling.

Original use: empty states and inline art on both light and dark backgrounds:

> Subject: `{subject}`. Flat editorial illustration with a touch of halftone grain, in mid-tone SATURATED colours — cobalt blue, teal and one muted coral accent — that read clearly on BOTH a light and a dark background. No white, no pale fills, no dark navy. Isolated subject on a fully transparent background — no background box, no scene, no ground. NO text, NO letters, NO watermark.

## 2b. House spot — transparent marketing illustration

The current spot illustration style, replacing the older keyring example. Use it for feature cards and other small marketing illustrations that share the blog's visual language. When an illustration must work on both light and dark surfaces, review this style on both and create a dedicated variant if needed.

Use one clear object or a tightly connected group to express one idea. Begin with a bold, readable silhouette. Add restrained painterly texture within the subject, with simple modeled form rather than photorealistic lighting, glossy 3D or clay.

Use deep ink navy, cobalt blue and one restrained coral accent as three color roles. Tonal shading within these roles is allowed; this is not a literal three-RGB-color limit. Navy must not carry the only identifying contour on a dark surface. Small light details may clarify the subject, such as a cloud inside a doorway; they must not form a pale background tile. Other accent families require an explicit art-direction choice for the set.

Use a square composition on a genuinely transparent background. Keep the whole silhouette within the canvas, with generous transparent margins. A small attached support or detail is allowed when it explains the subject. A contained view through an opening is allowed; a full scene, rectangular sky, broad ground plane or detached decoration is not.

Review at the intended display size, normally 64–128 px, on the actual target surface. If the same asset will be used in both themes, verify both; create a dedicated variant if its outline or details disappear. Balance the apparent scale of the set by visible silhouette, not canvas dimensions alone.

Keep the universal bans from Imagery: no text, letters, numbers, signage, watermarks, interfaces, logos, charts or product screenshots.

Prompt frame (`Subject: {subject}.` + House spot frame):

> Subject: `{subject}`. Create a HOUSE SPOT marketing illustration derived from the Lepsto blog foreground: a bold, clearly readable silhouette in deep ink navy and cobalt blue with ONE restrained coral accent, and gentle painterly brushwork contained within the subject. Simple modeled forms and printed texture; no photorealism, glossy 3D, clay or studio rendering. Square 1:1 composition. One compact subject cluster, fully visible, with generous transparent margins; aim for about 80% of the canvas's longest dimension. Keep the silhouette and key details readable at 64–128 px on the intended surface. Genuine transparent alpha background, including outer corners and gaps. A small attached support or a contained view inside the object is allowed only when needed for the metaphor. No background tile, rectangular sky, full scene, broad ground plane or detached decorations. Navy defines depth, cobalt defines the main forms, coral marks one accent. Tonal variations are allowed within these roles. Small light details may clarify the subject, without forming a pale background tile. Preserve a visible blue edge where navy would disappear against the target surface. No extra accent families. NO UI, NO logos, NO charts, NO product screenshots. NO text, NO letters, NO numbers, NO signage, NO watermark.

Put the metaphor and intended surface in the subject brief. The frame supplies the square composition and transparent-background instruction; also select a square output size and actual alpha support in the generation tool. A prompt alone does not guarantee a transparent file. Keep PNG masters with alpha; optimized WebP delivery copies may be used in the site. Inspect edges and gaps on both control surfaces and remove stray pixels before export.

The eight Infrastructure illustrations in Images → Spot illustrations are existing direction examples from [landing PR 627](https://github.com/lepsto/lepsto-landing/pull/627), not outputs from this consolidated frame. Compare apparent scale across the set; the door uses a larger coral area and the cloches and servers occupy less of their square canvas. Card padding, hover, blur and application-specific offsets belong to the consuming layout.

## 3. Author portrait (screenprint edit of a real photo)

Always an **edit** of a real photograph — likeness kept, never a from-scratch face:

> Transform this photograph into a bold editorial screenprint portrait while keeping the person's exact likeness — facial features, hairstyle, expression, and glasses if present. Style: flat screenprint / risograph illustration in deep ink navy on a warm bone-paper background, halftone shading in the shadows, crisp confident linework, a subtle cobalt-blue mid-tone, and ONE small coral accent detail (a collar edge or similar). Head-and-shoulders crop with generous negative space. NO text, NO letters, NO watermark. Square composition.

## Palette summary

Cover and House spot use **deep ink navy**, **cobalt blue** and **one coral accent**.
Portraits add warm bone paper. The retired universal spot frame used saturated
cobalt and teal with muted coral, and excludes dark navy, white and pale fills.

These are color roles, not exact HEX values or a literal three-color file limit.
Use restrained tonal variation, keep one accent family per set, and check the
intended surface. This is the imagery-side echo of [[docs/brand/tokens]] — brand
blues plus a single warm accent, never a rainbow.
