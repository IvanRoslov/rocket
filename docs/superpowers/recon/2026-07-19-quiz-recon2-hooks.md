# Quiz recon #2 — does PreToolUse hook execute while AskUserQuestion is pending?

## Setup

`/private/tmp/claude-501/-Users-ivanroslov-projects-rocket/92e737e1-8670-425a-a0cd-33c6fd3828fc/scratchpad/quiz-recon2/.claude/settings.local.json`:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "AskUserQuestion",
        "hooks": [
          { "type": "command", "command": "IN=$(cat); echo \"PRE $(date +%s) $IN\" >> .../hook-log.txt; exit 0" }
        ]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "AskUserQuestion",
        "hooks": [
          { "type": "command", "command": "IN=$(cat); echo \"POST $(date +%s) $IN\" >> .../hook-log.txt; exit 0" }
        ]
      }
    ]
  }
}
```

Ran `claude --dangerously-skip-permissions` inside tmux session `rocket-quiz-recon2`, cwd = the scratch dir (so settings.local.json applies). Prompted it to call `AskUserQuestion` with one single-select question ("Which color?": Red/Green/Blue) and one multiSelect question ("Which fruits?": Apple/Banana/Cherry) in a single tool call.

## Step 3 — is PRE written while the quiz is pending?

Captured pane right after the tool fired — quiz visible, unanswered, cursor on "Which color?" question:

```
❯  1. Red
      The color red
   2. Green
      The color green
   3. Blue
      The color blue
   4. Type something.
5. Chat about this
Enter to select · Tab/Arrow keys to navigate · Esc to cancel
```

Polled `hook-log.txt` 5x over ~8s while this screen was showing and untouched. **PRE line was present on the very first poll** (written before or essentially at prompt-render time):

```
PRE 1784485352 {"session_id":"ce22567b-69b4-4ea1-ba27-3665304ea0bf", ... ,"hook_event_name":"PreToolUse","tool_name":"AskUserQuestion","tool_input":{"questions":[{"question":"Which color?","header":"Color","options":[{"label":"Red","description":"The color red"},{"label":"Green","description":"The color green"},{"label":"Blue","description":"The color blue"}],"multiSelect":false},{"question":"Which fruits?","header":"Fruits","options":[{"label":"Apple","description":"Apples"},{"label":"Banana","description":"Bananas"},{"label":"Cherry","description":"Cherries"}],"multiSelect":true}]},"tool_use_id":"toolu_017AB88T3MrhfRZrc3MQgyTP"}
```

Timestamp 1784485352 stayed identical across all 5 polls (1784485359 → 1784485367) — the PRE hook fired exactly once, well before the quiz was answered (quiz was answered ~58s later, POST at 1784485410).

**Confirmed: the PreToolUse hook script executes synchronously at tool-invocation time, in parallel with (or before) rendering the quiz on screen — independent of whether/when the human answers.** Its stdin payload contains the **complete `tool_input`**: both questions, all option labels+descriptions, and each question's `multiSelect` flag. This is exactly the data a poller needs to detect and describe a pending quiz, and it's available immediately, unlike the transcript JSONL record (which flushes only after the answer per the prior recon finding).

## Step 4 — does Space toggle checkboxes on the multiSelect question?

Advanced to the "Which fruits?" multiSelect question by pressing `1` on the color question (selects Red, auto-advances). Pane before Space (cursor on option 1, Apple, unchecked):

```
❯ 1. [ ] Apple
  Apples
  2. [ ] Banana
  Bananas
  3. [ ] Cherry
  Cherries
```

Pressed `Space`. Pane after:

```
❯ 1. [✔] Apple
  Apples
  2. [ ] Banana
```

**Confirmed: Space toggles the checkbox** on the currently highlighted option of a multiSelect question (prior recon only verified digit-key toggling; this confirms Space works too). Followed up by pressing `2` which also toggled Banana to `[✔]`, confirming digit-key toggling still works alongside Space.

## Step 5 — does POST fire after answering, and does it carry the answers?

Answered via: `1` (Color→Red, auto-advance), `Space` (toggle Apple), `2` (toggle Banana), `Tab` (advance to Submit tab), `Enter` (submit).

Claude's transcript echoed:
```
User answered Claude's questions:
 · Which color? → Red
 · Which fruits? → Apple, Banana
```

`hook-log.txt` POST line appeared immediately after submission:

```
POST 1784485410 {"session_id":"ce22567b-...", ... ,"hook_event_name":"PostToolUse","tool_name":"AskUserQuestion","tool_input":{...,"answers":{"Which color?":"Red","Which fruits?":"Apple, Banana"},"annotations":{}},"tool_response":{"questions":[...],"answers":{"Which color?":"Red","Which fruits?":"Apple, Banana"},"annotations":{}},"tool_use_id":"toolu_017AB88T3MrhfRZrc3MQgyTP","duration_ms":0}
```

**Confirmed: POST fires right after submission and its stdin carries both `tool_input.answers` and `tool_response.answers`** — a full map of question → chosen answer(s).

## Cleanup

Sent `/exit` in the session, killed tmux session `rocket-quiz-recon2` (verified gone from `tmux list-sessions`), verified no leftover `claude` process referencing `quiz-recon2` cwd. Did not touch rocketd (port 4477), `~/.rocket`, the rocket repo, or any other tmux session (unrelated sessions `heavy-stream-test-orch` and `platform-issue-178-orch` were left untouched).

## STATUS: SUCCESS
