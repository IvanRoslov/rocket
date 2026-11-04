# Creative materials API and agent publication

Creative artifacts belong to a creative run and optionally its stage/item. A named artifact is identified by `(run_id, stage_id, item_id, name, kind)`, where kind is `source` or `result`. Omitted context IDs mean zero in Go/JSON and NULL in SQLite. Names and kinds distinguish separate materials; repeated publication to the same name/context/kind appends a version.

Published version IDs, bytes and metadata never change. Every publication atomically selects its new version as current. Selecting an old version changes only `current_version_id`; history stays readable. No deletion API is provided.

## Upload and inspection

All routes are beneath `/v1/creative`. JSON uses snake_case.

| Method/path | Input | Response |
| --- | --- | --- |
| GET `/runs/{run}/artifacts` | Query `stage_id`, `item_id` optional | Array of artifacts in **exact** context |
| POST `/runs/{run}/artifacts` | Query `name`, `kind`, `filename` required, optional `stage_id`, `item_id`; raw bytes in body, `Content-Type` | 201 `{artifact, version}` |
| GET `/artifacts/{artifact}` | Artifact ID | `{artifact, versions}`; ascending version_number |
| POST `/artifacts/{artifact}/versions` | Query `filename`; raw bytes, `Content-Type` | 201 `{artifact, version}` |
| PUT `/artifacts/{artifact}/current` | JSON `{ "version_id": 123 }` | Artifact with selected current version |
| GET `/artifact-versions/{version}` | Version ID | Version metadata |
| GET or HEAD `/artifact-versions/{version}/content` | Optional `download=1`; standard Range/conditional headers | File bytes, partial content, or headers |

An artifact has `id`, `run_id`, optional `stage_id`/`item_id`, `name`, `kind`, `current_version_id`, `created_at`. Version metadata has `id`, `artifact_id`, context IDs, `version_number`, `filename`, `mime`, `size`, `sha256`, optional `author_session_id`, `created_at`, `content_url`, `download_url`. Timestamps are Unix seconds. Storage paths/keys are private and never exposed by JSON.

The content URL always names an immutable **version**. Use it directly for authenticated image, audio or video elements, fetching document text, or downloading. Byte ranges and HEAD are supported. Raster images, plain text/Markdown, audio and video permit inline use. Markdown is served as plain text for safe client-side rendering. HTML, SVG and other arbitrary files download with attachment disposition; the response also uses nosniff and restrictive CSP. Native browser codec support determines playback; there is no transcoding.

## Identity and review binding

Existing trust applies: Unix socket access uses file permissions; TCP requires a paired device token or browser cookie. Session identity headers are accepted only over Unix. Unknown session IDs fail. Humans and sessions in the artifact's project may publish/select current. Publication author is derived from the session header; callers cannot supply it as metadata. Authenticated reads follow Rocket's global dashboard access.

Store integration uses `ValidateCreativeArtifactVersions(runID, stageID, itemID int64, versionIDs []int64) error`. Run context requires run-level versions. Item context requires exact run/stage/item ownership. Stage context permits that stage's own versions and its item descendants, enabling whole-stage reviews/discussion. Other runs/stages/items fail. Listing and artifact ownership stay exact; a dashboard can aggregate its item lists for whole-stage review while preserving each item's identity.

Core submission stores explicit `artifact_version_ids` and increments `review_revision`; approval/rejection uses the shown revision. Discussion carries explicit version IDs. Never substitute current pointers when sending these references: changing current must not change what a human saw.

## Agent CLI

```sh
rocket creative artifact publish RUN --stage STAGE --item ITEM \
  --name 'Final film' --kind result --file ./film.mp4
rocket creative artifact publish RUN --name 'Source notes' --kind source --file ./notes.md
rocket creative artifact ls RUN --stage STAGE --item ITEM
rocket creative artifact show ARTIFACT
rocket creative artifact versions ARTIFACT
rocket creative artifact current ARTIFACT VERSION
```

Stage/item flags are optional; item requires stage. `--kind` defaults to result. `--mime` overrides extension-derived content type. JSON output contains the selected immutable IDs and URLs. The CLI streams the local regular file; it never asks the daemon to read a caller-selected pathname. Upload cancellation, disk errors and server errors are reported, with a separate 30-minute transfer deadline. Ordinary API request deadlines are unchanged.

## Operational limits and failures

Content is streamed with a **2 GiB per file** limit. This accommodates the existing roughly 620 MB 1080p animated-story master, while bounding shared daemon disk use. It is an operational cap, not a codec rule. Existing development screenshot attachments keep their PNG/JPEG/WebP 10 MiB behavior.

Private files live under `<Rocket home>/creative-artifacts`, independent of source files and worker worktrees. Files are synced and renamed before the version/current database transaction. Failed/incomplete uploads publish no version, preserve the previous current version and remove their temporary/new blob. A crash before the database commit can leave an unreferenced file; automatic garbage collection is outside this deliverable. Back up this directory together with Rocket's database.

Errors use Rocket's JSON error envelope: 400 invalid input/empty or interrupted transfer, 401 unknown session/unauthenticated TCP, 403 wrong-project write, 404 missing context/artifact/version, 413 over 2 GiB, 500 storage/publication/unavailable-content failure. A missing on-disk published file is a visible storage error. Clients should retain local files after an unsuccessful transfer and display the returned error.
