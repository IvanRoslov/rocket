# Gate298 materials cleanup evidence

## Diagnosis

Read-only CLI metadata (`rocket creative run ls`, `rocket creative artifact ls 1 --stage 1`, `rocket creative artifact show 2`) identifies:

- Artifact 2, name `paid-generation-proposal`, run 1 / stage 1.
- Selected/current immutable version ID 2, version number 1.
- Filename `paid-generation-proposal.md`, MIME `application/octet-stream`, 10,931 bytes.
- SHA256 `c4daf93a5d30e1c334409ef848d022365deb6a5f94bd4992e4b1ee3d51fb05ab`.

Observed metadata explains the missing preview: `Media` previously admitted only explicit text MIME values and otherwise displayed an unsupported-file fallback. Even explicit `text/markdown` was displayed in a raw `<pre>`, not formatted Markdown. The content API intentionally serves generic MIME as an attachment; fetch can safely retrieve those same immutable bytes for client-side rendering. The HTML `/view` endpoint supports only HTML and is unrelated to Markdown viewing. Publication accepts the supplied MIME; CLI extension inference is host-dependent. The metadata does not establish which publisher/host inference produced the generic value.

No live content was fetched or evaluated. This diagnosis addresses the rendering/MIME path; it makes no claim about the editorial quality of the proposal's contents.

## Implementation and scope

The central Messages query/feed and general-message composer mode are removed. Question creation remains bound to the draft's original scope and selected version IDs, and existing thread reply/answer/dismiss actions and resolved history remain available. Backend message storage/delivery and right-rail session chat/term/attach are unchanged.

Markdown renders with the existing GFM renderer. The document-only resource policy leaves raw HTML inactive, blocks unsafe and relative navigation, and renders image alt text without loading embedded resources. Explicit HTTP(S)/mailto links remain user-initiated. Explicit Markdown MIME and `.md`/`.markdown` filenames with generic/plain MIME qualify; HTML and media keep their existing handling. Preview remains capped at 1 MiB, and failed/oversized loads explain the download fallback. Stored artifact name is unchanged; selected filename/version appears alongside it. Selection, download, and review context still use the same immutable version ID.

Existing published names, content, versions, approval state and spending permission were not changed. No live mutations, configuration changes, paid calls or restart occurred. The sibling generation-questions task owns agent-instruction changes.

## Verification

- Question/draft regression cycle: 4 expected failures before implementation; 21/21 question/workspace tests pass after it.
- Artifact regression cycle: 3 expected failures before implementation; 12/12 artifact tests pass after it.
- Full Creative frontend: 40/40 tests pass; production build passes.
- Full shared dashboard: 722/722 tests pass; production build passes.
- Full `go test ./...` and focused API/store/CLI/client package suites pass; Rocket binary builds to a temporary path without installation or restart.
- Existing Node localStorage experimental warnings and dashboard chunk-size warning remain; no test failures are hidden by them.

The automated workspace tests exercise question creation, scope/version draft preservation, clarification, answer/close and session access; artifact tests exercise formatted GFM, malicious input, generic MIME, immutable selection/download, load failure/size fallback, HTML isolation and image/video/audio behavior. Human real-case evaluation remains outstanding by design.

Independent code review found no acceptance-breaking findings or spec contradictions. Review covered code and regression tests; it did not independently execute the suites or inspect live content.
