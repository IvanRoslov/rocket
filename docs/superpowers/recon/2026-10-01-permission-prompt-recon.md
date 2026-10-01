# Permission dialogs in Claude Code — recon report

Scope: empirical facts for `runtime.ParsePermissionPrompt` and for answering
permission dialogs from chat (feature #4881). Claude Code **2.1.286**,
tmux 3.6a, pane 120×40 (plus one 60-column capture). Scratch git repo in the
worker's scratchpad; the throwaway `claude` was killed at the end, no real
repo or daemon state touched.

Launch variants used:

| variant | command |
|---|---|
| user config (rocket's own flags) | `claude --dangerously-skip-permissions --settings '{"crossSessionInbound":"accept"}'` |
| clean config, bypass | `claude --dangerously-skip-permissions --setting-sources project,local --settings '{…}'` |
| clean config, manual | `claude --setting-sources project,local --permission-mode default` |

`--setting-sources project,local` drops `~/.claude/settings.json`, whose
`permissions.allow` (`Bash(*)`, `Edit`, `Write`, …) otherwise suppresses
nearly every prompt even outside bypass mode. Without
`--permission-mode default` a clean config starts in *auto* mode.

## 1. Which dialogs appear under `--dangerously-skip-permissions`

**Ordinary permission dialogs do not appear.** In bypass mode, with or
without the user config, all of these ran without a prompt: Edit of
`.claude/settings.json`, Write to `.claude/commands/hello.md`, Edit of
`.git/info/exclude`, Write of `.zshrc`, `.mcp.json`, `.vscode/settings.json`,
Bash `rm -rf ./build`, `ls /etc`, `sudo -n true`. rocket never sets
`LaunchSpec.PermissionMode`, so a freshly launched agent starts in bypass.

**What does appear in bypass:** the ExitPlanMode approval (the agent calls
`EnterPlanMode` itself, then `ExitPlanMode`):

```
────────────────────────────────────────────────────────────────────────
 Ready to code?

 Here is Claude's plan:
╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌
 Create file plan.txt containing ok
╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌
────────────────────────────────────────────────────────────────────────
 Claude has written up a plan and is ready to execute. Would you like to proceed?
 ❯ 1. Yes, and switch to BYPASS PERMISSIONS (no further prompts) for this session
   2. Yes, manually approve edits
   3. Tell Claude what to change
      shift+tab to approve with this feedback

 ctrl+g to edit in VS Code · ~/.claude/plans/parsed-dancing-fern.md
```

**How a rocket agent ends up with ordinary dialogs:** answering `2. Yes,
manually approve edits` (or shift+tab in the terminal) takes the session out
of bypass; from then on every Edit/Write/Bash prompts with the dialogs in §2.
Reproduced: after `2` the very next Write showed «Do you want to create
plan.txt?» (`permission-after-plan-create.pane`).

## 2. Layout (2.1.286)

The dialog replaces the composer at the bottom of the pane. No `│` box any
more (pre-2.1 layouts had one; the parser still accepts it):

```
⏺ Update(.claude/settings.json)
────────────────────────────────────────────────  ← full-width rule opens the dialog
 Edit file                                        ← tool header
 .claude/settings.json                            ← path / description
╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌  ← dashed rules frame the body
 1 -{}
 1 +{"env": {"FOO": "1"}}
╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌
 Do you want to make this edit to settings.json?  ← title, ends with «?»
 ❯ 1. Yes                                         ← cursor ❯ on one option
   2. Yes, and allow Claude to edit files in this project's .claude folder for this session
   3. No

 Esc to cancel · Tab to amend                     ← footer (absent on WebFetch)
```

Titles seen: «Do you want to make this edit to <file>?», «Do you want to
create <file>?», «Do you want to proceed?» (Bash), «Do you want to allow
Claude to fetch this content?» (WebFetch), «Claude has written up a plan and
is ready to execute. Would you like to proceed?» (plan).

Bash dialogs carry a «Tip: auto mode handles these prompts…» row (wraps in
narrow panes) and a 3rd option «Yes, and switch to auto mode · …». A
multi-line command is shown with a `│ ` gutter.

Option labels wrap: at a word boundary when the next word does not fit, and
**mid-word** when a single word (a long path) is longer than the row —
e.g. `…rocket-task-4881-promp` / `t-parser/…`. Continuation rows are indented
past the option number.

## 3. Keys

| key | ordinary dialog (Edit/Create/Bash/WebFetch) | plan dialog |
|---|---|---|
| digit `N` (in range) | selects option N **and confirms immediately**, no Enter needed; nothing leaks into the composer | options 1–2: confirm immediately (verified `2`) |
| digit on a free-text option | — | `3` only **moves the cursor** to «Tell Claude what to change»; dialog stays open |
| digit out of range (`9`) | ignored, dialog stays | — |
| Enter | confirms the highlighted option (default `1. Yes`) | same |
| Esc | cancels and interrupts the turn: «Interrupted · What should Claude do instead?» | rejects the plan; session stays in plan mode |
| «No» option (`3`/`4`) | rejects at once, no text field («User rejected write to …»); on WebFetch «No, and tell Claude what to do differently (esc)» behaves like Esc | — |

So "one digit, no Enter" holds for every choice that is not a free-text
field. The parser drops a trailing free-text choice from `Options` and
returns empty `Options` (Raw + Esc fallback) if one is not last.

## 4. Look-alikes the parser must reject

- AskUserQuestion widget — same «question? / ❯ 1. …» shape; told apart by
  `LooksLikeQuizWidget` (footer «Enter to select · …»). In 2.1.286 a rule
  also separates «Type something.» from «Chat about this».
- Held-peer-message dialog — unnumbered options; `LooksLikeHeldPeerDialog`.
- Folder-trust dialog — unnumbered «❯ No, exit / Yes, I trust this folder»,
  its question is mid-paragraph.
- A numbered list in the agent's reply, or the dialog's own wording echoed
  in output — always followed by the composer's `────` rules, which never
  sit below a live dialog.

## 5. Notes for the daemon (permission-daemon task)

- `Capture` is plain text (no `-e`); the parser also strips escapes, so
  `CaptureEscaped` works too.
- The monitor's current `inputWaitCaptureLines = 15` is enough for title and
  options of every captured dialog, but cuts `Context` for big diffs or
  multi-line commands. ~40 rows gives the full header.
- Title is stable while the dialog is open (moving the cursor does not change
  it), so it is a good `prompt_changed` key; two consecutive Bash prompts may
  share the title «Do you want to proceed?» — compare Context as well if that
  matters.

## Fixtures

`internal/runtime/testdata/`: `permission-edit-settings.pane` (+ `-ansi`,
captured with `-e`), `permission-edit-file.pane`, `permission-create-file.pane`,
`permission-bash-outside-cwd.pane`, `permission-bash-rm.pane`,
`permission-bash-multiline.pane`, `permission-narrow-60col.pane`,
`permission-webfetch.pane`, `permission-exit-plan.pane`,
`permission-exit-plan-cursor3.pane`, `permission-after-plan-create.pane`;
negatives `permission-neg-quiz.pane`, `permission-neg-numbered-output.pane`,
`permission-neg-idle.pane`, `permission-neg-trust.pane` (plus the existing
`held-dialog-open.pane`). All captured with `tmux capture-pane -p -S -40`.
