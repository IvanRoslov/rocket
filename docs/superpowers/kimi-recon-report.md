# Kimi Code CLI 2.4.0 live recon for the Rocket adapter

Observed on macOS with `kimi --version` = `2.4.0` and `tmux -V` = `tmux 3.6a` on 2026-10-28. Every interactive run used a disposable Git repository under this worktree's ignored `.rocket/scratch/kimi-recon/`. The main session used `kimi-code/k3.1-pro`; the short print-mode probes used the same model. No global Kimi configuration was changed.

The committed JSONL files are selected **real** records from these runs, in source order, except `failed-turn-synthetic.wire.jsonl`. Model thinking text was replaced with `[redacted reasoning]`; the records were re-serialized as compact JSON. IDs, record types, field names, timestamps, user prompts, and tool results are otherwise retained. The state, CLI list, and cache samples came from the manual question probe. No login or credential files were copied.

## 1. Launch and pane states

In the disposable `trusted` Git repository, this detached tmux command reached the normal composer without a start dialog:

```sh
env KIMI_CODE_TRUST_WORKSPACE=1 KIMI_CODE_NO_AUTO_UPDATE=1 KIMI_DISABLE_TELEMETRY=1 \
  ~/.kimi-code/bin/kimi --auto -m kimi-code/k3.1-pro
```

The idle footer read `Never Ask  K3.1 Pro thinking: high`; the welcome card displayed version `2.4.0` and `No session yet — one will be created on your first message.` The composer was `│ > ... │`. Typing without Enter kept the draft in the composer. During a tool-using turn, submitting `reply QUEUED_OK` showed a separate pending row, `❯ reply QUEUED_OK`, with `↑ to edit · ctrl-s to steer immediately`. After both turns finished, the pending row disappeared and the composer was empty. These are the captured [idle](../../internal/runtime/testdata/kimi-idle.pane), [draft](../../internal/runtime/testdata/kimi-draft.pane), [busy with queue](../../internal/runtime/testdata/kimi-busy-queued.pane), and [finished](../../internal/runtime/testdata/kimi-finished.pane) panes (`tmux capture-pane -p -S -200`). `tmux` reported `#{alternate_on}=0` for the live panes.

The first immediate `tmux send-keys -l TEXT` followed by `send-keys Enter` added a blank composer line without submitting. A later Enter submitted it; later probes with a pause after text did submit. Headless delivery should check for the resulting `turn.prompt`, rather than treating an Enter keystroke as proof of delivery.

## 2. Trust and other start screens

With `KIMI_CODE_TRUST_WORKSPACE` unset in a fresh `untrusted` Git repository, startup stopped at [this captured trust pane](../../internal/runtime/testdata/kimi-trust.pane). Its heading was `Trust this folder?`; the choices were `❯ Trust and continue` and `Exit`, **without numbers**. The screen also showed the full workspace path, a project integration disclosure, and the instruction `↑↓ navigate · Enter select · Esc exit`. This matches the [2.4.0 trust renderer](https://github.com/MoonshotAI/kimi-code/blob/%40moonshot-ai%2Fkimi-code%402.4.0/apps/kimi-code/src/tui/components/dialogs/trust-prompt.ts#L99-L135).

Other screens were not reproducible in these fresh sessions. The 2.4.0 source gives their layouts, so these are source observations rather than pane captures:

- The [survey panel](https://github.com/MoonshotAI/kimi-code/blob/%40moonshot-ai%2Fkimi-code%402.4.0/apps/kimi-code/src/tui/components/panes/survey-panel.ts#L38-L69) renders a question and options; its [constants](https://github.com/MoonshotAI/kimi-code/blob/%40moonshot-ai%2Fkimi-code%402.4.0/apps/kimi-code/src/tui/constant/survey.ts#L13-L15) say `How is Kimi doing this session? (optional)` and `1: Bad`, `2: Fine`, `3: Good`, `0: Dismiss`.
- The [cache-expired dialog](https://github.com/MoonshotAI/kimi-code/blob/%40moonshot-ai%2Fkimi-code%402.4.0/apps/kimi-code/src/tui/components/dialogs/cache-hint-dialog.ts#L32-L49) has four choices: compact, new session, continue, and never ask again; its [rendered heading and warning](https://github.com/MoonshotAI/kimi-code/blob/%40moonshot-ai%2Fkimi-code%402.4.0/apps/kimi-code/src/tui/components/dialogs/cache-hint-dialog.ts#L87-L118) include idle time and token count.
- A [model migration prompt](https://github.com/MoonshotAI/kimi-code/blob/%40moonshot-ai%2Fkimi-code%402.4.0/apps/kimi-code/src/tui/controllers/model-migration.ts#L225-L245) uses `Model migration available` with `Switch to …` and `Keep current model`.
- The [update prompt renderer](https://github.com/MoonshotAI/kimi-code/blob/%40moonshot-ai%2Fkimi-code%402.4.0/apps/kimi-code/src/cli/update/prompt.ts#L70-L119) has a `Kimi Code Update Available` title and selected choice marked `❯`. This run deliberately set `KIMI_CODE_NO_AUTO_UPDATE=1`.

## 3. Agent-file prompt and Rocket prompt decision

A valid explicit file was Markdown with YAML frontmatter (`name` and required `description` in this probe) and a body that included `${base_prompt}` followed by `If asked for the agent-file marker, reply exactly AGENT_FILE_MARKER_742.` The [2.4.0 agent-file documentation](https://moonshotai.github.io/kimi-code/en/customization/agents) describes the frontmatter and `${base_prompt}` template variable. A print-mode launch with `-m kimi-code/k3.1-pro --agent-file ../recon-agent.md -p 'What is the agent-file marker? Reply only the marker.'` returned `AGENT_FILE_MARKER_742`; its `profile.bind.profileName` was `rocket-recon`, and its `systemPrompt` contained the marker. Thus the file parsed and affected a model response in print mode.

The TUI accepted `--auto -m kimi-code/k3.1-pro --agent-file …` syntactically, but **did not bind that agent on the first turn** in four live variants: relative and absolute paths outside the Git root, a file inside the Git root, and a no-`--auto` control. Each resulting `profile.bind.profileName` was `agent`, and `systemPrompt` lacked the marker. The first [TUI response](../../internal/runtime/testdata/kimi-agentfile-ignored.pane) was `AGENTS.md`, not the marker. This contradicts the intended startup binding shown in the [2.4.0 TUI source](https://github.com/MoonshotAI/kimi-code/blob/%40moonshot-ai%2Fkimi-code%402.4.0/apps/kimi-code/src/tui/kimi-tui.ts#L2769-L2813); the live cause inside the CLI remains unresolved. Also, `--auto` and `-p` together exit with `error: Cannot combine --prompt with --auto.`

**Decision for the Rocket TUI adapter:** put the Rocket prompt in a managed `AGENTS.md` block, not `--agent-file`. A control TUI run with `--auto -m kimi-code/k3.1-pro` and an `AGENTS.md` in the disposable Git root recorded that path in `profile.bind.agentsMdPaths`, included `AGENTS_MARKER_318` in `systemPrompt`, and [answered with the marker](../../internal/runtime/testdata/kimi-agentsmd.pane). An agent-file may still be useful in print mode, but it is not dependable for the observed interactive launch.

## 4. Thinking effort environment variable

The models-only [config excerpt](../../internal/agent/kimicode/testdata/config-models.toml) lists `support_efforts = [ "low", "high", "max" ]` and `default_effort = "high"` for `kimi-code/k3.1-pro`. Three fresh print-mode requests, each with `KIMI_MODEL_THINKING_EFFORT` set to one value and `-m kimi-code/k3.1-pro -p 'reply OK'`, recorded matching `llm.request.thinkingEffort`: [low](../../internal/agent/kimicode/testdata/effort-low.wire.jsonl), [high](../../internal/agent/kimicode/testdata/effort-high.wire.jsonl), [max](../../internal/agent/kimicode/testdata/effort-max.wire.jsonl). The low probe's `profile.bind.thinkingEffort` still said `high`; a low TUI run also displayed `thinking: high` in its footer. Use `llm.request`, rather than the binding record or TUI footer, as evidence of the actual request effort.

## 5. Session and journal format

Before the first prompt, the trusted TUI said `No session yet`, and the new workspace session directory was absent. Immediately after submission it created `~/.kimi-code/sessions/wd_trusted_2fd29ffe6ac9/session_3df4260f-3c18-4fbe-a02a-f2788102bc6f/`, with `state.json` and `agents/main/wire.jsonl`. The first journal record was exactly `{"type":"metadata","protocol_version":"1.5","created_at":1793194497730}`. The `metadata` record has no cwd; the actual [state.json](../../internal/agent/kimicode/testdata/state.json) uses `cwd`, and `kimi session list --json` uses `workDir` and `sessionDir` (see [sample](../../internal/agent/kimicode/testdata/session-list.json)).

No `sessions/session_index.jsonl` existed during the turn or after killing the tmux session. A complete recursive search found no such file for these fresh v2 sessions. The observed index data was `sessions/.index-cache/scan.json` ([one real entry](../../internal/agent/kimicode/testdata/session-index-cache.json)); the [2.4.0 `session_index.jsonl` writer](https://github.com/MoonshotAI/kimi-code/blob/%40moonshot-ai%2Fkimi-code%402.4.0/packages/migration-legacy/src/session-index.ts#L20-L36) is in the legacy migration package. **Decision for session discovery:** do not require `session_index.jsonl`; find native session directories and read `state.json`/`agents/main/wire.jsonl`. A `session_index.jsonl` fixture would misrepresent this live version, so the committed state, CLI list, and cache samples replace it.

Representative exact record shapes, drawn from [the completed tool turn](../../internal/agent/kimicode/testdata/completed-tool-turn.wire.jsonl):

```json
{"type":"turn.prompt","agentId":"main","input":[{"type":"text","text":"Use shell to run sleep 12 then printf tool-ok; reply DONE."}],"origin":{"kind":"user"},"promptId":"msg_01M61BH5QGKT8FBM2RRVSR2DDY","turnId":0,"time":1793194497785}
{"type":"context.append_message","agentId":"main","message":{"role":"user","content":[{"type":"text","text":"Use shell to run sleep 12 then printf tool-ok; reply DONE."}],"id":"msg_01M61BH5QGKT8FBM2RRVSR2DDY","toolCalls":[],"origin":{"kind":"user"}},"time":1793194497786}
{"type":"usage.record","agentId":"main","model":"kimi-code/k3.1-pro","usage":{"inputOther":4075,"output":164,"inputCacheRead":19200,"inputCacheCreation":0},"usageScope":"turn","time":1793194508934}
{"type":"turn.ended","agentId":"main","turnId":0,"reason":"completed","durationMs":47486,"traceId":"a37434090bbdeaf76f3053aaf0c76b59","time":1793194545270}
```

The `context.append_message` records in every observed main-agent journal had `message.role = "user"`; no assistant or tool role appeared in that record type. Assistant and tool messages instead appeared in `agent.message.appended.message.message`, and step events such as `tool.call`, `tool.result`, and `content.part` appeared under `context.append_loop_event.event`. The completed fixture contains a real `Bash` call (`sleep 12 && printf tool-ok`), its `tool-ok` result, assistant message, usage, and terminal records. The [open/busy fixture](../../internal/agent/kimicode/testdata/open-busy-turn.wire.jsonl) was captured while turn 0 was active and has no `turn.ended`.

The second message submitted during the first turn stayed in the pane's queue. The journal did not record that user text at enqueue time. Once turn 0 ended, it recorded a new `agent.message.appended` user event, `agent.turn.started` with `turnId: 1`, a separate `turn.prompt` with `input[0].text: "reply QUEUED_OK"`, and eventually `turn.ended` for turn 1 ([queued fixture](../../internal/agent/kimicode/testdata/queued-second-message.wire.jsonl)). The first turn also contained `prompt.steered`/`turn.steer` from **Superpowers skill activation** (`origin.kind = "skill_activation"`), not from this queued user message. Delivery confirmation can match the user-origin `turn.prompt` after dequeue.

A manual-permission TUI probe explicitly called `AskUserQuestion`, yielding [this pane](../../internal/runtime/testdata/kimi-question.pane) and a real [open interaction fixture](../../internal/agent/kimicode/testdata/interaction-open.wire.jsonl). Its request record was:

```json
{"type":"interaction.request","agentId":"main","id":"question_b7e82c21-0849-4476-a8d8-74a22735be3d","kind":"question","toolCallId":"tool_ItYHXZVMPuevLGC59eckDY9w","request":{"turnId":0,"toolCallId":"tool_ItYHXZVMPuevLGC59eckDY9w","questions":[{"question":"Proceed?","options":[{"label":"Yes"},{"label":"No"}]}]},"time":1793194987558}
```

`Ctrl-C` dismissed that question. The [dismissed interaction fixture](../../internal/agent/kimicode/testdata/interaction-dismissed.wire.jsonl) then has `interaction.resolved` with the same `id` and `response: null`, followed by `turn.ended reason: "completed"`.

No cheap live `reason: "failed"` turn was produced. The clearly named [synthetic failure fixture](../../internal/agent/kimicode/testdata/failed-turn-synthetic.wire.jsonl) follows the [2.4.0 `TurnEnded` schema](https://github.com/MoonshotAI/kimi-code/blob/%40moonshot-ai%2Fkimi-code%402.4.0/packages/agent-core-v2/src/agent/loop/turnOps.ts#L94-L134) and [error payload shape](https://github.com/MoonshotAI/kimi-code/blob/%40moonshot-ai%2Fkimi-code%402.4.0/packages/agent-core-v2/src/_base/errors/serialize.ts#L5-L14). Its 429 message is illustrative, not an observed quota response. The schema permits `completed`, `cancelled`, `failed`, and `blocked`.

A one-line `Delegate to an explore subagent to calculate 1+1, then reply OK` probe created `agents/agent-0/wire.jsonl` beside `agents/main/wire.jsonl` in the same session directory. The [parent excerpt](../../internal/agent/kimicode/testdata/subagent-parent.wire.jsonl) contains `subagent.spawned`, `subagent.started`, and `subagent.completed` for `agent-0`; `subagent.completed.usage` repeats the child's totals. The [child excerpt](../../internal/agent/kimicode/testdata/subagent-child.wire.jsonl) has its own `metadata`, `turn.prompt`, `usage.record` with `agentId: "agent-0"`, and `turn.ended`. To sum usage across agents, include each agent journal's `usage.record` once; adding `subagent.completed.usage` as well would double-count the child.

With the shell's logical `PWD` set to a symlink named `trusted-link`, `kimi session list --cwd trusted-link --json` returned `[]`; querying the physical `trusted` directory returned that session with `workDir` set to the physical path. Session cwd matching therefore needs symlink canonicalization.

## 6. Bracketed multiline paste

A 5,007 byte, 91-line UTF-8 file was sent with `tmux load-buffer -b kimi-recon-paste …` and `tmux paste-buffer -p -b kimi-recon-paste`, then Enter. The composer showed `[paste #2 +91 lines]`. Turn 2 contained one `turn.prompt` whose `input[0].text` encoded to exactly 5,007 bytes; a byte comparison with the source file returned `True`. The full prompt is in [paste-5007.wire.jsonl](../../internal/agent/kimicode/testdata/paste-5007.wire.jsonl). A previous 4,182 byte paste was cleared before submission.

## 7. Project skill beside Superpowers

The disposable Git root contained `.kimi-code/skills/rocket-recon-skill/SKILL.md`, with a `name` and `description` frontmatter and instruction to return `PROJECT_SKILL_MARKER_519`. A fresh `kimi -m kimi-code/k3.1-pro -p 'Invoke rocket-recon-skill and report its marker; name one superpowers skill you can see.'` answered with `PROJECT_SKILL_MARKER_519` and `superpowers:brainstorming`. The journal also recorded skill activation. Thus project skill discovery coexisted with the installed Superpowers plugin in this fresh session.

## 8. Other implications for a headless tmux worker

- `--auto` is visible as `Never Ask` in the footer and `permission.set_mode` in the journal, but it cannot be combined with `-p`. The adapter's interactive tmux launch is the relevant path.
- The TUI may be ready before any session journal exists. Startup and message-delivery checks need distinct signals.
- The busy queue is visible in the pane before the journal contains the queued text. A `turn.prompt` later confirms model delivery; absence while still queued is not a failure.
- The trust dialog and question dialog have different shapes, and the trust choices are unnumbered. A parser copied from Codex's numbered start screen will miss it.
- The live model request effort can differ from the TUI footer and `profile.bind`; inspect `llm.request` when diagnosing settings.
- Tips, spinner text, terminal width truncation, and the `No session yet` welcome line varied during the run. Match stable structural rows for pane classification.
