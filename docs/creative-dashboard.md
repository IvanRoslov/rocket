# Creative web dashboard

Open `/creative/` on the same Rocket origin as the development dashboard. This is a separate application and build; `/` remains the development application. The creative app uses the existing browser pairing cookie. If pairing is required, the shared login screen returns to the original creative deep link after pairing.

Select an existing project, define a pipeline, and create creative work with a name and brief. Each run snapshots the selected process. Pipeline edits affect new runs only. The saved process rail selects the run, stage or concrete item; results and discussion follow that selection. Pending repeated items can be added, edited or removed in the selected stage.

The artifact viewer retains published versions and labels the current version. Stage views aggregate results from their concrete items while keeping each item's identity. During review, the viewer starts with the submitted immutable versions. Browsing another version requires returning to the submitted set before approval. Reopening a stage or item keeps later results and requires another normal submission/approval according to the saved process.

Upload sources in their intended run/stage/item context. Text up to 1 MiB is displayed inline; raster images and supported audio/video use browser media support. All files have download links; unsupported formats/codecs remain downloadable. Uploads are limited by the artifact service to 2 GiB each.

Discussion automatically includes the selected context and displayed artifact versions. A draft keeps its original target and versions when selection changes; discard it to begin a draft in the new context. Question replies and answers preserve the question's original context. A deliberate Go option or written Go reply uses the existing question/message flow; stage approval is separate. The orchestrator must describe exact paid calls and cost basis and wait for the human's explicit Go. The dashboard adds no payment execution or technical spending gate.

Launch the creative orchestrator using an existing enabled model profile, or restore its existing session. Landing preview uses the preview service's availability and access URL for both its sandboxed iframe and separate-tab link. When preview setup or its local server is missing, the service's actionable message is shown. See `creative-previews.md` from deliverable 5990 for operator setup.

## Development and build

- `cd creative-web && npm ci && npm run dev`: independent Vite application at `/creative/`, with the same `/v1` daemon API proxy as the development app.
- `cd creative-web && npm test`: focused component and API-boundary tests, using the sibling contracts.
- `make creative-web-build`: production build with `/creative/` asset base.
- `make build`: both application builds followed by the shared daemon binary; no restart.
- `make test`: Go, development web, and creative web tests.

`creative-web/embed.go` embeds its own `dist`; the tracked placeholder keeps plain `go build` usable before a web build. Production asset files are ignored. Do not commit generated index files; `make go-build` restores both tracked placeholders after embedding.

The UI consumes the core, assets, agents and preview implementations for feature creative-work (5986), deliverables 5987–5990. It does not provide alternate storage, simulated results, generation providers, mobile screens or a development-dashboard creative mode.
