# Pi live recon report

Feature: `pi-and-deepseek2` (#5867), Task 1 (#5874)

Status: `DONE` — all requested DeepSeek session/pane shapes were captured. The standalone usage fixture uses Pi's public SessionManager API; native automatic cache warming remains a separately documented negative finding.

## Environment and safety

- Pi: `pi --version` → `1.4.0`; existing installer-managed release on `PATH`.
- OS: macOS 26.3.1, Darwin 25.3.0 arm64.
- `pi --help` exited 0 and confirmed `--tui-mode regular`, `--approve`, `--session-id`, `--model`, and `--thinking`.
- `pi --list-models` exited 0 in the credentialed shell and listed `deepseek/deepseek-flash` and `deepseek/deepseek-v4-pro`, both with 1M context.
- Only the human-supplied DeepSeek credential was used for successful calls. It was entered through `read -s`; its value was not read, printed, copied, or stored by the worker. Non-DeepSeek live checks were deferred as directed.
- Live probes used a throwaway git directory and isolated `PI_CODING_AGENT_DIR` under git-ignored scratch. No global package, shell, Pi setting, auth, or credential files were changed.

Launch command, run in the prepared credentialed shell:

```sh
pi --tui-mode regular --approve --session-id pi-recon --model deepseek/deepseek-v4-pro
```

The pane terminal was 140 columns by 45 rows. Raw captures remained in ignored scratch; published fixtures contain redacted paths and prompt/response text but retain the observed layout.

## Composer, paste, clear, and steering

Fixtures: `internal/runtime/testdata/pi-idle.pane`, `pi-draft.pane`, `pi-plain-draft.pane`, `pi-cleared.pane`, `pi-busy.pane`, `pi-busy-queued.pane`, and `pi-finished.pane`.

- **Idle:** startup help, an empty composer, cwd/branch row, and `deepseek-v4-pro • high` model row are visible in `pi-idle.pane`.
- **Plain input:** `tmux send-keys -l` inserted a single-line draft without submitting it; see `pi-plain-draft.pane`.
- **Bracketed paste:** `tmux set-buffer` followed by `tmux paste-buffer -p` inserted a three-line draft without submitting it. All three lines are visible between composer separators in `pi-draft.pane`.
- **Conclusion:** `paste=bracketed`. Plain literal input works for single-line text, but bracketed paste is the verified safe mode for Rocket's multiline messages.
- **Clear:** `tmux send-keys C-c` removed the draft without changing the session. This matches Pi's startup hint `ctrl+c/ctrl+d clear/exit`. Conclusion: `clear-keys=ctrl+c`; evidence is `pi-cleared.pane`.
- **Busy:** after the synthetic tool request, the composer was replaced by a `Working` row (`pi-busy.pane`). The completed bash tool call and its duration are visible in `pi-finished.pane`.
- **Queued steering:** pressing Enter while busy produced `Steering: <redacted-steering>` and `↳ Option+Up to edit all queued messages` above the `Working` row (`pi-busy-queued.pane`). Conclusion: `queue-row=Steering:`.
- **Finished:** the busy/steering rows disappeared, the empty composer returned, and usage/model attribution remained in the footer (`pi-finished.pane`).

## Session file and tree

Observation: the isolated session store contained exactly one main session file matching:

```text
$PI_CODING_AGENT_DIR/sessions/--<encoded-physical-cwd>--/<utc-timestamp>_pi-recon.jsonl
```

The filename suffix is `_<session-id>.jsonl`; the parent directory encodes the physical cwd. With `PI_CODING_AGENT_DIR` unset, the base is Pi's default `~/.pi/agent`. A locator must match both the encoded cwd and exact session ID, not merely select the newest JSONL.

The first record is `type=session`, `version=3`, with `id`, `timestamp`, and `cwd`. Subsequent records use `id`/`parentId` to form the session tree. `model_change` carries `provider` and `modelId`.

Fixture: `internal/agent/pi/testdata/pi-session.jsonl`.

This 11-record redacted excerpt preserves:

1. session header, model change, thinking-level change, and redacted system message;
2. first user message;
3. assistant tool call with `api=openai-completions`, `provider=deepseek`, `model=deepseek-v4-pro`, usage, and `stopReason=toolUse`;
4. `role=toolResult` with matching `toolCallId`, `toolName`, content, error flag, and duration;
5. queued steering persisted as a normal `role=user` record after the tool result;
6. steering assistant response with usage;
7. second ordinary user/assistant turn with usage.

The queued user record has no special `queued` flag in JSONL. Its parent is the tool result; the TUI's `Steering:` row is the pre-persistence queue evidence.

## DeepSeek usage and cache

All token counts below are unchanged in the fixtures. Costs and response IDs were also retained; only strings containing prompts, reasoning, tool I/O, paths, summaries, and provider error details were redacted.

Observed in `pi-session.jsonl`:

| Turn | `usage.input` | `usage.cacheRead` | `usage.cacheWrite` | `usage.output` |
|---|---:|---:|---:|---:|
| First assistant/tool call | 4975 | 0 | 0 | 87 |
| Queued-steering assistant | 103 | 4992 | 0 | 22 |
| Second ordinary assistant | 143 | 4992 | 0 | 7 |

The first turn is a real cache miss (`cacheRead=0`). Both subsequent turns are real cache hits (`cacheRead>0`); the second ordinary turn used the same stable session prefix and no forced cache-retention setting.

Model attribution rules observed in Pi 1.4.0:

- Assistant messages attribute usage directly through `message.provider` and `message.model`.
- Standalone `type=usage` entries also carry direct top-level `provider` and `model`; their arbitrary `kind` identifies the operation and they do not participate in LLM context.
- `compaction` and `branch_summary` keep usage at the record's top level but have **no** provider/model field. Attribute them to the session's governing `model_change` (`deepseek` / `deepseek-v4-pro` here), not to token counts or a hardcoded provider.
- Assistant and standalone usage fields map naturally to Rocket as `input → Input`, `cacheRead → CacheRead`, `cacheWrite → CacheWrite`, and `output → Output`.

## Compaction

Fixture: `internal/agent/pi/testdata/pi-compaction.jsonl`.

`/compact` initially returned `Compaction failed: Nothing to compact (session too small)`. After ordinary turns and one synthetic large-context turn exceeded Pi's compaction floor, `/compact` succeeded without changing settings.

The fixture includes the governing `model_change` and real `type=compaction` record with `firstKeptEntryId`, `fromHook`, `details`, redacted `summary`/`systemMessage`, and top-level usage:

```text
input=745 cacheRead=0 cacheWrite=0 output=3612 totalTokens=4357
```

The preceding large-context assistant recorded `input=16067`, `cacheRead=5632`, and `totalTokens=21707`; compaction reported that it compacted from 21,707 tokens.

## Abandoned branch and branch summary

Fixture: `internal/agent/pi/testdata/pi-branch-summary.jsonl`.

A previous user turn was selected through `/tree` and retried with a new synthetic prompt, creating a sibling branch. After a real assistant response on that branch, `/tree` was used to return to the main branch. Pi presented `Summarize branch?`; selecting `Summarize` generated the fixture's `type=branch_summary` record.

The fixture includes the governing `model_change`, abandoned-branch user/assistant messages, and branch summary. The assistant's usage is directly attributed to `deepseek-v4-pro`. The branch summary has `fromId`, `parentId`, `fromHook=false`, redacted summary/details, and top-level usage:

```text
input=16317 cacheRead=0 cacheWrite=0 output=213 totalTokens=16530
```

This confirms that usage remains in the JSONL even after the branch is abandoned and that branch-summary usage is separate from assistant usage.

## Provider error

Fixture: `internal/agent/pi/testdata/pi-provider-error.jsonl`.

A separate session used `deepseek/deepseek-v4-pro` with an explicitly invalid dummy API key, never the real credential. A no-tools prompt produced a real assistant record with:

```text
provider=deepseek model=deepseek-v4-pro stopReason=error
errorMessage=401: <redacted-authentication-error>
```

The fixture preserves the error record shape and redacts the provider's request ID and dummy-key suffix.

## Standalone usage entry and automatic-warming finding

### Native automatic cache warming

Normal startup, multiple turns, compaction, branch switching, and a clean resume produced no automatic cache-warming operation. Source investigation found the precise reason:

- Installed `dist/core/cache-warmer.js:26-33,123-128` requires `model.promptCache` for the active retention tier and otherwise stops with `cache lifetime unavailable`.
- Installed `pi-ai/dist/providers/data/deepseek.json` defines both DeepSeek models without `promptCache`, so the unmodified DeepSeek sessions never scheduled a refresh.
- `docs/settings.md:20-23` documents default `cacheWarming=streaming` and the `$0.05` expected-savings threshold. `dist/core/cache-warmer.js:238-250` persists `appendUsage("cache_warm", ...)` only after a successful replay.
- The installed implementation was compared with upstream Pi `v1.4.0` `src/core/cache-warmer.ts`; the relevant logic matches.

No native automatic cache-warming observation is claimed.

### Standalone usage fallback fixture

Fixture: `internal/agent/pi/testdata/pi-usage.jsonl`.

Pi 1.4.0 publicly exports `SessionManager` from `dist/index.js`. Its public API defines `appendUsage(kind, provider, model, usage, note?)` in `dist/core/session-manager.d.ts:268-269`; the implementation at `dist/core/session-manager.js:863-877` creates the standalone entry and synchronously appends it to JSONL. `appendUsage` accepts arbitrary kinds, and direct `provider`/`model` attribution is mandatory.

An isolated Node probe called the real API in this order:

1. `SessionManager.create(<throwaway-cwd>, <isolated-session-dir>, { id: "pi-recon-usage" })`;
2. `appendModelChange("deepseek", "deepseek-v4-pro")`;
3. one synthetic `appendMessage`, required by `_hasConversation()` before Pi creates a new session file;
4. `appendUsage("session_manager_probe", "deepseek", "deepseek-v4-pro", usage, "isolated public API probe")`.

The controlled usage inputs were `input=7`, `output=3`, `cacheRead=5`, `cacheWrite=2`, `reasoning=0`, `totalTokens=17`, with zero cost fields. They are API-probe values, not billed provider usage or an automatic-warming measurement. Pi generated the real `id`, `parentId`, and `timestamp`; the returned entry exactly matched the last persisted JSONL record on readback.

The fixture contains the actual four-record session. Only cwd and synthetic user text were redacted. It was not copied from the documentation example, and no credential, global setting, auth/package change, third-party extension, or record-type transformation was used.

## Artifact checks

- Redaction preserved every JSON object key, array length, numeric value, usage count, and record ID; only strings and selected fixture record ranges changed.
- Every published JSONL line parses as an object; record IDs are unique within each fixture.
- Pane and JSONL scans found no real user paths, credential values, private prompts, or dummy-key residue. The scan ran in the credentialed shell and printed only `credential_value_matches=0` and `residue_matches=0`.
- `python3 .superpowers/sdd/task-1-plan/scratch/validate_fixtures.py` → `FIXTURE VALIDATION GREEN (5 JSONL files)`.
- `git diff --check` → exit 0.
- `make test` → exit 0; all Go packages passed and web tests passed 61 files / 718 tests. The web suite emitted pre-existing, non-fatal Node `localStorage` experimental warnings unrelated to these docs/fixtures.
- Final scope inspection found 13 changed files: this report, five JSONL fixtures, and seven pane fixtures; no production Go files or unrelated changes.
- Non-DeepSeek live calls were not performed.
