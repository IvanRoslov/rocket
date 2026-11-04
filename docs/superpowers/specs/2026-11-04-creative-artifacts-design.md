# Creative artifact storage (5988 / creative-work)

Durable human source uploads and agent results belong to a creative run and optionally a stage/item, using the core context resolver. Named artifacts retain every published version and explicitly identify the current version. No development attachment behavior changes, frontend media UI, generation, deployment, transcoding, or daemon restart.

## Approach

Use Rocket SQLite metadata and streamed files beneath `<cfg.Home>/creative-artifacts`, independent of worker worktrees. SQLite BLOBs would enlarge the shared operational database; external object storage would add an unnecessary provider platform. Local durable files match existing Rocket attachment infrastructure while streaming avoids its screenshot-only memory/size limits. This is an implementation choice within the brief.

Each upload is limited to **2 GiB** of file content, an operational limit for current short video outputs, including the existing roughly 620 MB 1080p master, not a media-format limit. Stream to a private temporary file, hash, sync, close and rename it before publishing metadata transactionally. On transfer failure, publish nothing; on metadata failure, remove the new blob. A process crash between file and metadata publication may leave an unreferenced blob, never a readable partial version. No automatic history deletion or garbage collection is included.

## Metadata and identity

Migration 0029 adds `creative_artifacts` (integer ID, run_id, nullable stage_id/item_id, name, kind=`source|result`, current_version_id, created_at) and `creative_artifact_versions` (integer ID, artifact_id, version_number, filename, mime, size, sha256, storage_key, author_session_id, created_at). Context/name/kind uniquely identify a named artifact; creation and publication serialize in a write transaction. New versions atomically become current; explicit selection can restore an older version without deleting or editing anything. Bytes and version metadata are immutable. Filenames never form filesystem paths.

Export `GetCreativeArtifactVersion(id int64) (CreativeArtifactVersion,error)` with joined RunID/StageID/ItemID, and `ValidateCreativeArtifactVersions(runID,stageID,itemID int64,versionIDs []int64) error`; validate exact context ancestry and every version. Core stores immutable review IDs and review revision; discussion preserves shown IDs. A changed current pointer cannot change either binding. Core may query the joined tables within its own transition transaction and must fail closed when assets are unavailable.

## API contract

All routes under `/v1/creative`, snake_case JSON; existing Unix trust and authenticated TCP device/cookie access apply. Reads follow existing global dashboard access. Mutations resolve the caller session and require human or same-project agent/session; author comes from the caller, never query fields.

- `GET /runs/{run}/artifacts?stage_id=&item_id=` lists named artifacts in the exact context (omitted IDs mean run context).
- `POST /runs/{run}/artifacts?stage_id=&item_id=&name=&kind=source|result&filename=` sends raw bytes with Content-Type, creates the named artifact or publishes its next version, and returns 201 `{artifact,version}`. Name, kind and filename required.
- `GET /artifacts/{artifact}` returns `{artifact,versions}` with retained versions in ascending version order.
- `POST /artifacts/{artifact}/versions?filename=` streams another version to that fixed context, returning 201 `{artifact,version}`.
- `PUT /artifacts/{artifact}/current` with `{version_id}` selects a version of that artifact, returns the artifact.
- `GET /artifact-versions/{version}` returns version metadata.
- `GET|HEAD /artifact-versions/{version}/content[?download=1]` serves immutable bytes with Range/Content-Length/ETag support and private caching. Version JSON includes `content_url` and `download_url`.

Valid media types are accepted, with application/octet-stream fallback when absent; known document, raster image, audio and video types can display/play inline. Active formats such as HTML/SVG and other arbitrary files use attachment disposition, nosniff and restrictive CSP. MIME declarations do not guarantee browser codec support; no transcoding.

Failures use Rocket JSON errors: 400 malformed/empty input or transfer failure, 403 wrong project, 404 missing context/artifact/version, 413 content over 2 GiB, 500 storage/publication failure. Incomplete uploads never change current version.

## Agent command

`rocket creative artifact publish RUN --stage ID --item ID --name NAME --kind result --file PATH [--mime TYPE]` streams a local file to the daemon and prints the publication JSON. `--kind source` supports human material. `ls RUN`, `show ID`, `versions ID`, `current ID VERSION` expose metadata and selection. File paths remain client-local; the daemon never reads caller-selected paths. The stream client uses a dedicated bounded transfer timeout, preserving ordinary request deadlines.

## Verification

Store tests exercise context ownership, retained versions/current selection, concurrent publication, and review/discussion validation. HTTP tests exercise human/agent identity, supported media above screenshot limits, transfer/storage errors leaving no published rows, streamed range playback/download, and immutable history. CLI/client checks cover file streaming, header propagation and failures. Run existing Go/web tests, vet/build without touching the live daemon. No paid calls, demo projects, or simulated generation suite.
