# tools — image and video generation

An npm zone, like `storybook/`. The docs linter does not reach in here.

Five providers, five entry points.

| Entry | Provider | For |
|---|---|---|
| `src/cli.ts` | Replicate | anything Replicate hosts, images and video |
| `src/laozhang-cli.ts` | laozhang.ai | images, through an OpenAI-compatible endpoint |
| `src/kie-cli.ts` | kie.ai | video, on an async task API |
| `src/reapi-cli.ts` | reapi.ai | images, video and audio, on an async task API |
| `src/byteplus-cli.ts` | BytePlus ModelArk | Seedance first-party, from ByteDance itself |

The three proxies resell the same handful of models; BytePlus is where Seedance actually
comes from, which is why its request shape looks nothing like theirs.

The first two share `styles.ts`, so `--style cover --subject … --sky …` means the same
thing on both. Available styles are `cover`, `spot`, `house-spot` and `portrait`.
Use `spot` for universal light/dark inline art; use `house-spot` for compact
painterly marketing objects. The canonical frames live in [[docs/brand/imagery]].
For House spot, include the intended surface in the subject, choose square output
and request real transparency through the provider's supported inputs. Review the
result at 64–128 px; `--sky` applies only to covers.

For laozhang.ai, the existing output flags can express that request:

    node src/laozhang-cli.ts --style house-spot --subject "one open coral door in a cobalt frame, a small sky view contained inside the opening, for a dark navy surface" --size 1024x1024 --background transparent --format png

For Replicate, select a model that supports alpha output and set its documented
size and transparency inputs through `--input`. The style frame does not set
provider-specific output parameters.

## Run — Replicate

    node src/cli.ts video --model bytedance/seedance-2.5 --prompt "a key turning in a lock" --input '{"duration":10}'
    node src/cli.ts image --model <owner/name> --style cover --subject "a coral key" --sky twilight

Outputs land in `out/` alongside a `.json` sidecar recording the model, the full
input and the prediction id, so a run can be repeated.

## Run — laozhang.ai

    node src/laozhang-cli.ts --prompt "a coral key on a twilight sky" --quality low --size 1536x1024
    node src/laozhang-cli.ts --style cover --subject "a coral key" --sky twilight --quality high

`gpt-image-1` by default; `--model` takes anything the proxy serves. Outputs land in
`out/` with the same sidecar as the Replicate path, so a run stays repeatable.

**On the prompt cap:** the docs page states 1000 characters. It is wrong — a
2950-character prompt went through `gpt-image-2` verbatim on 2026-08-31, with usage
reporting 900 text tokens, so nothing was trimmed. The guard in `laozhang.ts` is set to
OpenAI's own ceiling instead, and it stays there because a silently truncated prompt
loses its trailing negative list first, and that loss never shows up as an error.

Image models the proxy serves, as of 2026-08-31: `gpt-image-2` (plus `-all`, `-vip`,
`-web`), `gpt-image-1.5`, `gpt-image-1`, `gpt-image-1-mini`, `gemini-3.1-flash-image`
and `gemini-3-pro-image` (the nano-banana family), `flux-2-pro`, `seedream-5-0-260128`,
`dall-e-3`. `GET /v1/models` lists all 236.

| Flag | Values |
|---|---|
| `--size` | `1024x1024`, `1536x1024`, `1024x1536`, `auto` |
| `--quality` | `low`, `medium`, `high`, `auto` |
| `--format` | `png`, `jpeg`, `webp` |
| `--background` | `transparent`, `opaque`, `auto` |
| `--n` | 1-10 |
| `--base-url` | defaults to `https://api2.laozhang.ai/v1`, or `LAOZHANG_BASE_URL` |

The base URL is a flag because the two published sources disagree: the API docs give
`api2.laozhang.ai`, the marketing pages give `api.laozhang.ai`. Point it at whichever
answers, rather than editing the default.

Key: `LAOZHANG_API_KEY`, read from `tools/.env` automatically (gitignored). The
1Password reference in `src/laozhang-auth.ts` is still a `<TODO:` — fill it in once the
key has a home in the vault and the CLI prefers it over the file.

## Run — kie.ai

    node src/kie-cli.ts --credit true
    node src/kie-cli.ts --prompt "$(cat shot.txt)" --first-frame card.png \
      --duration 25 --resolution 480p --aspect adaptive --out out --label ship
    node src/kie-cli.ts --status <taskId>

kie.ai is asynchronous: `createTask` returns a task id and the CLI then polls
`recordInfo` until the task lands, printing each state change. If your shell dies
mid-generation the work is not lost — `--status <taskId>` picks the same task back up.

`--first-frame` and `--reference` are **mutually exclusive**; the model takes an
image-to-video scenario or a multimodal-reference one, never both, and the CLI refuses
the combination rather than letting the endpoint do it. `--last-frame` needs a first
frame beside it. Local paths are uploaded to kie's own file host first and the returned
link is what the task references; a URL is passed through untouched.

`--aspect` does not offer `3:2`. The accepted set is `1:1`, `4:3`, `3:4`, `16:9`, `9:16`,
`21:9`, `adaptive` — use `adaptive` to follow the first frame. `--duration` is 4-30
seconds. Audio defaults to off, because it costs more and a film usually gets its sound
in the edit.

Verified live on 2026-08-31: credit, upload, createTask and recordInfo were each
exercised against the running service. The docs site renders as an SPA and returns HTML
to a fetch, so the contract here came from probing, not from reading.

Key: `KIE_API_KEY`, read from `tools/.env`.

## Run — reapi.ai

    node src/reapi-cli.ts video --model doubao-seedance-2.5-face --prompt "$(cat shot.txt)" \
      --image "https://host/card1.png,https://host/card2.png" \
      --input '{"duration":25,"resolution":"480p","size":"adaptive","generate_audio":false}'
    node src/reapi-cli.ts image --model gpt-image-2 --prompt "..." --input '{"size":"3:2"}'
    node src/reapi-cli.ts --status <taskId>

**Nothing about a model is hardcoded here.** reAPI publishes an agent-readable schema and
live price per model at `https://reapi.ai/models/<slug>/llms.txt`, and says outright that
the catalog moves and that prices must not be inferred from anywhere else. Fetch that file
before a run; pass whatever it documents through `--input`.

`--model` takes the **wire id**, not the page slug — the `seedance-2-5` page is
`doubao-seedance-2.5-face`. The full enum lives in `https://reapi.ai/openapi.json`.

**Media inputs must be public URLs.** base64 and `data:` URIs are rejected platform-wide,
and reAPI has no upload endpoint of its own. `--host gcs` copies each local path to our
refs bucket, `gs://lepsto-marketing-gen-refs` (project `less-staging-489114-t7`,
europe-west4), under a random 24-hex name and passes its public URL. The bucket is
public-read and deletes objects after 1 day; it holds generation references only. It needs
`gcloud` logged in with write access to the bucket. `--host true` instead pushes the file
through the kie.ai file host, which needs `KIE_API_KEY` beside `REAPI_API_KEY`.

Polling defaults to 2 seconds, inside their published limit of 5 requests a second per
user, polling included. A failed task refunds its reserved credits in full, so a lost
shell costs nothing — and `--status <taskId>` picks a live one back up.

Key: `REAPI_API_KEY`, an `rk_live_…` token, from `tools/.env`.

## Run — BytePlus ModelArk

    node src/byteplus-cli.ts --model dreamina-seedance-2-5-260628 --prompt "$(cat shot.txt)" \
      --ratio 16:9 --resolution 480p --duration 25 --watermark false \
      --image "https://host/card1.png" --out out --label ship
    node src/byteplus-cli.ts --status <taskId>

**Settings are not JSON fields here.** They are appended to the prompt text as
command-line style flags — `... --ratio 16:9 --resolution 720p --duration 5` — and the
model parses them out of the sentence. The CLI does that assembly, so pass them as flags
and keep the prompt free of them; a prompt that already carries `--flags` is refused
rather than sent, because two sets of settings leave the model to pick a winner.

**The model id is dated and account-scoped.** Seedance 2.5 is
`dreamina-seedance-2-5-260628` — the `dreamina-` prefix is what a plain
`seedance-2-5-…` guess misses, and the trailing date moves with each release, so read the
current id off the model page in the ModelArk console rather than carrying this one
forward. Two error codes tell the two failures apart, and the distinction is worth
knowing:
`InvalidEndpointOrModel.NotFound` means the id is wrong, while `ModelNotOpen` means the id
is right but the model is not activated for this account — that one is fixed in the
console, not in the command.

Both regions are reachable: `--host byteplus` (`ark.ap-southeast.bytepluses.com`, the
default) and `--host volcengine` (`ark.cn-beijing.volces.com`), or pass a full base URL.

Images must be public URLs. `--host-upload true` pushes a local file through the kie.ai
file host and passes the returned link, which needs `KIE_API_KEY` beside the BytePlus one.
`--last-frame` is sent as an image carrying `role: last_frame`.

Verified live on 2026-08-31: both the create and the retrieve paths, on both hosts, and
the key itself against the task-list endpoint.

Key: `BYTEPLUS_API_KEY`, from `tools/.env` (mirrored from
`op://Lessly Platform/Byteplus api token/password`).

## Token — Replicate

1Password first — `op read "op://Lessly Platform/Replicate/password"` — falling
back to `REPLICATE_API_TOKEN` in the environment. The CLI prints which of the two
it used on stderr.

## Models

`bytedance/seedance-2.5`, `bytedance/seedance-2.0`, `alibaba/wan-3` are known by
name. Their input keys were read from `GET /v1/models/{owner}/{name}` on
2026-08-30, so a misspelled `--input` key fails before the call is billed. Their
presets stay empty on purpose: a default belongs to the model, and a copy here
would start lying the moment Replicate changes one.

| Model | Notable inputs beyond `prompt`, `image`, `duration`, `resolution`, `aspect_ratio`, `seed` |
|---|---|
| `bytedance/seedance-2.5` | `last_frame_image`, `generate_audio`, `watermark`, `output_format`, `reference_images` (30), `reference_videos` (10), `reference_audios` (10) |
| `bytedance/seedance-2.0` | `last_frame_image`, `generate_audio`, `reference_images` (9), `reference_videos` (3), `reference_audios` (3) |
| `alibaba/wan-3` | `negative_prompt`, `enable_prompt_expansion` |

`--image` sends the reference frame as the `image` field, which is what all three
models call it. Any other model runs through `--model` plus `--input`, unchecked —
the registry has no verified field list for it.

## Test

    npm test
