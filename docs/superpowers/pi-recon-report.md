# Pi live recon report

Feature: `pi-and-deepseek2` (#5867), Task 1 (#5874)

Status: `DONE_WITH_CONCERNS` — all requested DeepSeek session/pane shapes were captured except standalone cache-warming usage, which Pi did not emit.

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
- `compaction` and `branch_summary` keep usage at the record's top level but have **no** provider/model field. Attribute them to the session's governing `model_change` (`deepseek` / `deepseek-v4-pro` here), not to token counts or a hardcoded provider.
- Assistant usage fields map naturally to Rocket as `input → Input`, `cacheRead → CacheRead`, `cacheWrite → CacheWrite`, and `output → Output`.

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

## Cache-warming limitation

Normal startup, multiple turns, compaction, branch switching, and a clean resume produced no standalone `type=usage` record and no identifiable cache-warming operation. Therefore no cache-warming fixture is published. The brief's fallback could not be applied because it also requires a really observed `type=usage`; inventing that shape would violate the evidence requirement.

## Artifact checks

- Redaction preserved every JSON object key, array length, numeric value, usage count, and record ID; only strings and selected fixture record ranges changed.
- Every published JSONL line parses as an object; record IDs are unique within each fixture.
- Pane and JSONL scans found no real user paths, credential values, private prompts, or dummy-key residue. The scan ran in the credentialed shell and printed only `credential_value_matches=0` and `residue_matches=0`.
- `python3 .superpowers/sdd/task-1-plan/scratch/validate_fixtures.py` → `FIXTURE VALIDATION GREEN (4 JSONL files)`.
- `git diff --check` → exit 0.
- `make test` → exit 0; all Go packages passed and web tests passed 61 files / 718 tests. The web suite emitted only Node's non-fatal `localStorage` experimental warnings.
- Final scope inspection found 12 changed files: this report, four JSONL fixtures, and seven pane fixtures; no production Go files or unrelated changes.
- Non-DeepSeek live calls were not performed.
