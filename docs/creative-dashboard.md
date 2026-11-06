# Creative web dashboard

Open `/creative/` on the same Rocket origin as the development dashboard. This is a separate application and build; `/` remains the development application. The creative app uses the existing browser pairing cookie. If pairing is required, the shared login screen returns to the original creative deep link after pairing.

Open Pipelines to create or edit the shared process library, even before selecting or creating a project. Switching projects keeps the same templates and any unsaved editor draft. Select an existing project to create creative work with a name, brief and any shared pipeline. Each run snapshots the selected process. Pipeline edits affect new runs only. The saved process rail selects the run, stage or concrete item; results and discussion follow that selection. Pending repeated items can be added, edited or removed in the selected stage.

The artifact viewer retains published versions and labels the current version. Stage views aggregate results from their concrete items while keeping each item's identity. During review, the viewer starts with the submitted immutable versions. If a submission includes multiple versions of one artifact, each submitted version has its own immutable card. Browsing history does not change the submitted set in the review card. Reopening a stage or item keeps later results and requires another normal submission/approval according to the saved process.

The orchestrator submits exact published versions. The card above the selected stage/item tabs names the work and review revision, links each submitted version (including descendant item results), and is the only formal decision location. HTML links use isolated viewing; other files open the existing artifact media/document viewer. Already-pending reviews need no resubmission. Artifacts retains browsing and uploads, with no human Submit or duplicate decision buttons.

Approve completes the displayed revision. Return for changes opens What to fix and requires nonblank feedback; it is persisted with the decision/revision/versions and queued to the orchestrator through existing notifications. Failed requests keep the draft and show the error. Drafts never cross scope/revision boundaries; successful requests remove obsolete decision buttons immediately. Withdrawal returns the card to in-progress state, and a new submission displays its new revision. Start and Reopen remain available according to existing lifecycle rules; automatic stages gain no human gate.

Successful stage/item action requests publish `creative.stage_action` / `creative.item_action` through the existing Rocket event bus after the transition commits. Event data includes project/run/stage/item IDs, action, resulting status, review revision and immutable artifact version IDs; returns also include the required `reason`. The bus supplies server time. A resolved caller session appears in `session_id` and `actor_session_id`, with `actor_kind=session` and `actor_source=session_header`. Requests without a session header use an empty session ID, `actor_kind=unattributed` and `actor_source=no_session_header`; this does not establish a person's identity. Failed requests retain their HTTP error and emit no successful transition event. Existing event persistence/fanout failure semantics apply; these events do not backfill or explain historical actions.

Upload sources in their intended run/stage/item context. Text up to 1 MiB is displayed inline; raster images and supported audio/video use browser media support. All files have download links; unsupported formats/codecs remain downloadable. Uploads are limited by the artifact service to 2 GiB each.

Discussion automatically includes the selected context and displayed artifact versions. A draft keeps its original target and versions when selection changes; discard it to begin a draft in the new context. Question replies and answers preserve the question's original context. A deliberate Go option or written Go reply uses the existing question/message flow; stage approval is separate. The orchestrator must describe exact paid calls and cost basis and wait for the human's explicit Go. The dashboard adds no payment execution or technical spending gate.

Launch the creative orchestrator using an existing enabled model profile, or restore its existing session. The workspace has Questions & discussion and Artifacts tabs. Exact HTML artifact links still use the isolated viewing service; there is no Landing preview tab. See [creative-previews.md](creative-previews.md) for viewing configuration and operator setup.

## Shared presentation

Creative imports the development dashboard’s tokens, base styles and actual Button, Badge, Card, Dot, Tabs and EmptyState components directly from `web/src`. Both headers use the same presentation-only RocketBrand; Creative adds a small label and restrained indigo accent. Creative keeps its own workspace layout and native form fields styled with Rocket tokens. Vite deduplicates React for sibling imports; no UI package or development navigation is included.

## Development and build

- `cd creative-web && npm ci && npm run dev`: independent Vite application at `/creative/`, with the same `/v1` daemon API proxy as the development app.
- `cd creative-web && npm test`: focused component and API-boundary tests, using the sibling contracts.
- `make creative-web-build`: production build with `/creative/` asset base.
- `make build`: both application builds followed by the shared daemon binary; no restart.
- `make test`: Go, development web, and creative web tests.

After building, install the binary through the normal Rocket installation procedure and let the operator apply any required daemon restart. An already running daemon serves its embedded build; changing build files does not update it. Open `/creative/` on the existing Rocket origin after that operator-managed update. This task does not restart the daemon.

`creative-web/embed.go` embeds its own `dist`; the tracked placeholder keeps plain `go build` usable before a web build. Production asset files are ignored. Do not commit generated index files; `make go-build` restores both tracked placeholders after embedding.

The UI consumes the core, assets, agents and preview implementations for feature creative-work (5986), deliverables 5987–5990. It does not provide alternate storage, simulated results, generation providers, mobile screens or a development-dashboard creative mode.

## Validation limits

Component/API-boundary checks and production builds were verified. Browser visual behavior was not validated because the browser host was unavailable. Real creative cases are reserved for human acceptance; this work ran no paid calls, live generation, end-to-end demo projects or daemon restart.
