# Full remote quiz — recon report

Scope: empirical facts for designing remote AskUserQuestion answering via rocketd.
Live experiment ran in tmux session `rocket-quiz-recon`, scratch dir
`/private/tmp/claude-501/-Users-ivanroslov-projects-rocket/92e737e1-8670-425a-a0cd-33c6fd3828fc/scratchpad/quiz-recon`,
transcript `~/.claude/projects/-private-tmp-claude-501--Users-ivanroslov-projects-rocket-92e737e1-8670-425a-a0cd-33c6fd3828fc-scratchpad-quiz-recon/67ef9c36-592f-4004-89ad-f9bb671ff039.jsonl`.
Session and `claude` process were killed at the end; no live daemon/repo state touched.

## 1. Transcript corpus study

Searched `~/.claude/projects/*/*.jsonl` (real production transcripts, mostly agent-orchestrator
worker/orchestrator sessions). `AskUserQuestion` is common; found via
`grep -rl "AskUserQuestion" ~/.claude/projects/*/*.jsonl`.

### tool_use record (the quiz itself)

Appears as an `assistant` message content block, `type:"tool_use"`, `name:"AskUserQuestion"`.
Real single-select example:

```json
{"type":"tool_use","id":"toolu_015zsKDEjrHA5UsEhoAForyv","name":"AskUserQuestion","input":{
  "questions":[
    {"question":"Движок lessly-memory готов на ветках ...","header":"Движок","multiSelect":false,
     "options":[
       {"label":"Сначала консолидировать движок","description":"Отдельной задачей слить..."},
       {"label":"Привязать к Python hy-memory","description":"..."},
       {"label":"Биндить к ветке движка as-is","description":"..."}
     ]},
    {"question":"Насколько далеко двигаем brain-extension...","header":"Объём","multiSelect":false,
     "options":[{"label":"Только Phase 0","description":"..."}, ...]}
  ]}}
```

Multi-select example (`multiSelect:true`, same shape otherwise):

```json
{"question":"Какие настройки демона входят в v1? (можно несколько)","header":"Настройки v1",
 "multiSelect":true,
 "options":[
   {"label":"Категории: markdown / issues / PR (тогглы)","description":"..."},
   {"label":"Фильтры путей (include/exclude globs)","description":"..."},
   {"label":"Ветка (branch)","description":"..."},
   {"label":"Enable/disable (пауза)","description":"..."}
 ]}
```

Schema fields confirmed:
- `questions[]`: array, each with `question` (string, the prompt), `header` (short tab label,
  ≤ ~12 chars — becomes the tab caption in the TUI), `multiSelect` (bool), `options[]`.
- `options[]`: each `{label, description}` — `label` is the value that ends up in the answer;
  `description` is subtitle/help text shown under the option (often a translation/gloss in this
  corpus, since orchestrator prompts are Russian and options were sometimes bilingual).
- No explicit "Other" field in the schema — free text is an always-present implicit affordance in
  the rendered TUI (see live experiment), not something the tool_use JSON declares.
- Real records also carry a wrapping hook lifecycle: `PreToolUse:AskUserQuestion` (attachment,
  runs before the human sees/answers) and `PostToolUse:AskUserQuestion` (attachment, after answer)
  — both are project hooks (`.claude/activity-updater.sh`, warp-notify), not part of Claude Code
  core, but their presence/absence in a given transcript is a weak secondary "quiz happened" signal
  IF the project has such hooks configured (rocketd should not rely on this — it's project-specific).

### tool_result record (the answer)

Appears as a `user` message, `content:[{"type":"tool_result", ...}]`, matched to the tool_use via
`tool_use_id`. Two representations of the same info:
1. `content` (string, this is what the agent LLM actually reads back): a flattened summary,
   `Your questions have been answered: "<question1>"="<answer1>", "<question2>"="<answer2>". You
   can now continue with these answers in mind.`
2. `toolUseResult` (structured, sibling of `message`, this is what a daemon should parse): full
   echo of `questions` (schema as above, minus `multiSelect` sometimes omitted when false —
   inconsistent presence, see below) plus an `answers` map keyed by the **question text** (not an
   index or option id) → **the label string** (or, for multi-select, a joined string of labels).

Real single-select answer:
```json
"toolUseResult":{"questions":[...], "answers":{"Движок lessly-memory ...":"разработка движка идет
паралельно ...ты ошибся команда...", "Насколько далеко двигаем ...":"Весь роадмап 0→5"},
"annotations":{}}
```
Note: this example shows the answer value can be **free text that doesn't match any option label**
("разработка движка идет паралельно...") even though this record's `multiSelect` was false and no
"Other" option was declared in the schema — i.e. the human (via the dashboard/orchestrator Q&A
funnel, not the raw TUI) supplied arbitrary text as the answer. This confirms the daemon-side
answer channel is not required to be constrained to option labels — but the **raw Claude Code TUI**
still requires going through its own "Type something" row for genuine free text (see live section).

Real multi-select answer (labels joined, **no separator space** in this corpus record from
CLI v2.1.185):
```json
"answers":{"Какие настройки демона входят в v1? (можно несколько)":
  "Категории: markdown / issues / PR (тогглы),Фильтры путей (include/exclude globs),Enable/disable (пауза)"}
```
Contrast with the live experiment on CLI v2.1.215, where the join uses `", "` (comma+space) — see
below. **The join format is not a stable contract; do not parse it by splitting on a fixed
delimiter without also cross-checking against `questions[].options[].label`.**

### Ordering / timestamps

In the corpus, `tool_use` and its `tool_result` are always adjacent-ish in the file (tool_result's
`parentUuid` chains to the tool_use's `uuid`), and timestamps show a real gap while the human
thought: e.g. tool_use at `2026-06-21T15:38:52.603Z`, tool_result at `2026-06-21T15:44:24.655Z` (~5m32s
later). This is consistent with (but does not by itself prove) "tool_use written first, pending
visible on disk, tool_result appended once answered" — the live experiment (section 3) disproves
that reading: **both are appended to the file in the same flush, at answer time.**

No "Other"/free-text example was found anywhere in this real corpus — all found answers matched a
declared option label or (in the orchestrator-funneled case above) were relayed by the orchestrator
Q&A mechanism rather than the raw AskUserQuestion "Type something" TUI row.

## 2. Live TUI experiment (Claude Code v2.1.215, plain `claude --dangerously-skip-permissions`)

### Rendering
- Multi-question quiz renders as **tabs**: a row like `←  ☐ Color  ☐ Fruits  ✔ Submit  →` above the
  active question. `☐`/`☒` mark whether that question tab has been answered. `✔ Submit` is always
  present as the last tab, becoming the review/confirm screen.
- Each question screen: numbered options `1..N`, each with a one-line `description` shown indented
  below the label (smaller/dim). Cursor is a `❯` prefix on the highlighted row.
- Options list always ends with an extra unnumbered-looking-but-numbered row **"Type something."**
  (the "Other" free-text row) as option `N+1`, and after that a final row **"Chat about this"**
  (drops out of the quiz to let you type a normal chat message instead of answering) as `N+2`.
- Multi-select renders checkboxes `[ ]` / `[✔]` in front of each option instead of plain numbering;
  single-select has no checkbox, just the numbered label.
- Footer hint line changes contextually: `Enter to select · Tab/Arrow keys to navigate · Esc to
  cancel` on a single/first question with multiple questions pending; `Enter to select · ↑/↓ to
  navigate · Esc to cancel` on a lone question; adds `ctrl+g to edit in VS Code` once cursor is on
  the "Type something" row.
- Final "Submit" tab shows a **review screen**: `Review your answers`, then per question
  `● <question text>` / `  → <chosen label(s)>`, then `Ready to submit your answers?` with
  `1. Submit answers` / `2. Cancel`.

### Reliable keystrokes (all via `tmux send-keys`)

| Action | Keystroke | Effect observed |
|---|---|---|
| Select option N (single-select) | digit `N` | Immediately selects option N **and auto-advances** to the next unanswered question tab (or to Submit if it was the last). No separate Enter needed. |
| Move cursor without selecting | `Down`/`Up` (arrows) | Moves `❯ ` highlight row by row; does **not** select/toggle. |
| Toggle a checkbox (multi-select) | digit `N` | Toggles that option's `[ ]`↔`[✔]` in place; does **not** advance to next tab (stays on the same multi-select question so you can toggle more). Space was not tested but digit is confirmed reliable. |
| Move between questions once answered | `Tab` | Cycles forward through tabs; once all questions have an answer, `Tab` lands you on the **Submit/review** screen directly. |
| Confirm/submit final review | `Enter` (with `1. Submit answers` highlighted, the default) | Submits; TUI returns to normal chat view; transcript is written (see §3). |
| Select "Type something" (Other) **and actually get a text field** | Navigate cursor to the "Type something" row (arrows or digit `N+1`), then **type text directly** (e.g. `tmux send-keys -l "Parrot"`) — do **not** press Enter first | Typing while that row is highlighted turns the row into a live-editing text field (row visibly changes from "Type something." to showing the typed text, e.g. `❯ 4. Parrot`). Then `Enter` submits that free text as the answer. |
| Select "Type something" then press Enter with **no text typed** | digit/arrow to row, `Enter` | **Declines/cancels the whole quiz** — transcript shows `tool_result` with `is_error:true`, content `"The user doesn't want to proceed with this tool use. The tool use was rejected ..."`. This is a trap: Enter-on-empty-Other is NOT "confirm empty answer", it's "cancel the tool call". A remote client must never send bare Enter on the Other row without text. |
| Cancel entire quiz | `Esc` | Not exercised in detail but documented in the footer hint on every screen; expected same "user declined" outcome as above. |

Timing/settle notes: after each `tmux send-keys`, a `sleep 0.3–1s` was enough for the pane to
settle before the next `capture-pane`; no case needed more than ~1s. Sending the *prompt itself*
(asking Claude to call the tool) took several seconds (5–17s, LLM generation time) before the quiz
UI appeared — that's normal generation latency, unrelated to quiz mechanics.

### Verified pane transcripts (trimmed)

Before selecting Q1 option 2 (single-select, first quiz):
```
Pick a color
❯ 1. Red / Красный
  2. Green / Зелёный
  3. Blue / Синий
  4. Type something.
```
After sending `"2"`:
```
←  ☒ Color  ☐ Fruits  ✔ Submit  →
Pick fruits
❯ 1. [ ] Apple ...
```
(auto-advanced to next question tab, Color tab now `☒`).

Multi-select after toggling options 1 and 3 via digits, then `Tab`:
```
Review your answers
 ● Pick a color
   → Green
 ● Pick fruits
   → Apple, Cherry
Ready to submit your answers?
❯ 1. Submit answers
  2. Cancel
```

## 3. Detection question (the crux) — answered empirically

**The `tool_use` record for AskUserQuestion is NOT written to the transcript file while the quiz
is pending on screen.** Verified directly:

- Captured the transcript file size (`wc -c`) at the moment the quiz TUI was visible on the pane
  (`Pick a color` screen up, waiting for input): `100499` bytes.
- Polled every second for 5+ seconds while the quiz remained visible, untouched: file size stayed
  exactly `100499` — **zero bytes written**, and `grep -c '"type":"tool_use"'` on the file was `0`
  the whole time.
- The moment `Enter` was sent on the final "Submit answers" confirmation, the file jumped from
  `100499` → `104690` (within ~1s) → `108276` (by ~3s), and `grep -n "AskUserQuestion"` now showed
  the full `tool_use` record (`timestamp: 18:14:16`) **and** its `PreToolUse` hook attachment
  **and** the `tool_result`/`toolUseResult` (`timestamp: 18:15:28`) **and** `PostToolUse` hook — all
  four appended together, in the same flush, at answer time. The tool_use's own recorded timestamp
  (`18:14:16`, when Claude decided to call the tool) is ~72s **earlier** than when it actually hit
  disk (`18:15:28`, when the human finished answering) — Claude Code buffers the whole
  tool_use→tool_result round trip in memory and writes it atomically once resolved, not
  incrementally.

**Consequence for rocketd:** transcript polling (the existing `TranscriptTail`/`chat.go` mechanism)
structurally **cannot** detect "a quiz is currently pending" — there is no on-disk signal at all
until after the human (or a remote answerer) has already answered. Any "quiz pending" detector must
use a different signal, e.g.:
- **Pane-content inspection** (`tmux capture-pane -p` on the agent's pane, pattern-matching the
  known TUI markers: a line matching `Enter to select · `, the tab row `☐/☒ ... ✔ Submit`, or the
  numbered-options-with-"Type something."/"Chat about this" footer) — the only signal actually
  available while pending, but couples the daemon to Claude Code's TUI rendering (fragile across
  CLI versions/terminal widths; note the footer hint text differs between single-question
  `↑/↓ to navigate` and multi-question `Tab/Arrow keys to navigate`).
- **`PreToolUse:AskUserQuestion` hook**, if rocket ships one project-wide: this hook *does* fire
  before the human sees the prompt in principle, but in this experiment its `attachment` record
  was ALSO only flushed to the transcript in the same post-answer batch write (see line 19 in the
  captured transcript, timestamped at `18:15:28`, same instant as PostToolUse and tool_result) —
  so **the hook attachment is equally invisible on disk while pending**; a hook script would need
  to signal rocketd out-of-band (e.g. write to a side-channel file/socket/HTTP call) at hook-execution
  time rather than relying on transcript content, since Claude Code appears to hold the whole
  transcript segment for a tool call in memory until the call resolves.

## Key open questions / risks for design

- Space toggle for multi-select checkboxes was not tested (only digit-toggle confirmed); worth a
  follow-up check since Space is the more standard convention and dashboard remote-answer mapping
  may want to mirror whichever key is most robust.
- Multi-select answer join delimiter is inconsistent across CLI versions observed (`","` with no
  space in one real v2.1.185 transcript vs `", "` with space in this v2.1.215 experiment) — a
  daemon parser must match by label set membership, not by splitting on a hardcoded delimiter.
- No corpus example of "Other" was found in real historical transcripts — behavior here is entirely
  from the live experiment; worth validating again after Claude Code updates.
- The `PreToolUse`/`PostToolUse` hook attachments being flushed only at resolution (not at
  invocation) needs confirmation across more scenarios (e.g. a longer wait, or a differently
  configured hook) before ruling out hook-based detection entirely — this experiment only tested
  the default plain `claude` CLI hook stack from this scratch dir's ambient `~/.claude/settings*`
  hooks (activity-updater.sh, warp-notify.sh), not a custom rocket-authored hook that might flush
  synchronously via its own I/O.
- Translating a dashboard-submitted answer into keystrokes has a real footgun: sending bare Enter
  on the "Type something" row (without prior text) cancels the *entire* quiz rather than doing
  nothing — the daemon's remote-answer translator must never emit that sequence unless text is
  actually queued first.
