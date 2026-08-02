# T6 — Docs and prompts: the participant model

> **For agentic workers:** REQUIRED SUB-SKILL: use `superpowers:executing-plans`
> for the tasks below. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring the product documentation and the rocket-owned prompts in line
with the multi-participant question threads that landed in subtasks #730
(store) and #731 (API), so a reader of `docs/` and an orchestrator reading its
system prompt both describe the behaviour the daemon actually has.

**Architecture:** Documentation-only change. One new shared section in
`docs/12-tasks.md` describes the participant model once (participants,
`--to`, `waiting_on` / `your_turn`, answer rights, fan-out); `docs/10-agents.md`,
`docs/03-daemon-api.md` and `docs/11-dashboard.md` are corrected against the
code and link to it instead of restating it. The orchestrator prompt gains the
two behaviours it can now exercise (being pulled into somebody else's thread,
addressing a question with `--to`), and the agent-side CLAUDE.md snippet in
`docs/10-agents.md` gains `answer`.

**Tech Stack:** Markdown. `internal/prompts` templates are embedded via
`go:embed`, so `go test ./internal/prompts/...` covers the prompt edits.

## Global Constraints

- **The code is the source of truth, spec v1 second, spec v2 last.** Spec v2's
  only behavioural change (`--to` on `ask` narrows the turn) is subtask #737,
  which is in `backlog` and still waiting on the human's Q2 — it is NOT on
  `main` and must NOT be documented as current behaviour.
- Product docs (`docs/*.md`) are in Russian, prompts
  (`internal/prompts/templates/*.md`) in English. Keep each as it is.
- `docs/prompts/*.md` are human-readable mirrors of the embedded templates:
  a mirror carries a Russian preamble and fences the template in a ``` block.
  Any template edit must be mirrored.
- No behaviour changes: do not touch Go, web, mobile or CLI sources. The only
  non-`docs/` files in scope are `internal/prompts/templates/*.md`.
- Branch `feature/reply-answer/docs-participants`, one PR referencing feature
  `reply-answer` and subtask #735.

## Ground truth extracted from the code

Everything documented below was read off `main` at commit `6fb20cc`. Cite these
when writing; do not paraphrase from the spec.

| Fact | Source |
|---|---|
| Participant ids: `human`, agent id, session id | `internal/store/questions.go:11-22` |
| Tables `questions` / `question_messages` / `question_participants`; `agent_questions` dropped | `internal/store/migrations/0009_threads.sql` |
| You join by being in `--to` or by writing into the thread | `0009_threads.sql:75-76`, `internal/api/questions.go:493` |
| Both sides are seeded at creation: `human` + author + `task.SessionID` / `role_id`, only if set | `internal/api/questions.go:311-316`, `internal/api/agent_questions.go:233-234` |
| `waiting_on`: resolved → empty; last message's non-empty `addressed_to` → that; else all participants but the last author | `internal/api/threads.go:74-97` |
| A thread with no messages derives its turn from `asked_by` — `questions` has **no** `addressed_to` column, so `ask --to` does not narrow the turn | `internal/api/threads.go:79-96`, `0009_threads.sql:16-27` |
| `your_turn` is caller-relative; `whose_turn` (`user`/`orchestrator`/`role`) is derived from `waiting_on` for old clients | `internal/api/questions.go:106-107`, `internal/api/threads.go:99-111` |
| `answer`/`dismiss`: human and `kind=agent` only; others 403 "only the human user or a persistent agent may answer; use reply" | `internal/api/threads.go:126-128`, `internal/api/questions.go:563-567` |
| `ask`: human, any persistent agent, or the subject's own counterpart | `internal/api/threads.go:141-145` |
| `reply`: any participant (plus the counterpart, even before it has spoken) | `internal/api/threads.go:152-157` |
| Reply into a resolved thread: human → `409`, anybody else → reopen | `internal/api/questions.go:452-459`, `internal/api/agent_questions.go:310-317` |
| Read: human and `kind=agent` see everything; an orchestrator/worker sees threads it participates in, plus threads of its **root** task | `internal/api/threads.go:244-266` |
| Fan-out goes to every participant except the author, never narrowed by `to`; the human is never injected into | `internal/api/threads.go:177-226` |
| Delivery frame `[task #N Q<n> kind from <id>]` / `[role <id> Q<n> kind from <id>]`, uniform ` from`, including `from human` | `internal/api/threads.go:165-170` |
| Wire compatibility: `asked_by` and `messages[].author` stay `""` for the human, while `participants`/`waiting_on`/`addressed_to` carry `human` | `internal/api/questions.go:25-35` |

## File Structure

| File | Change |
|---|---|
| `docs/12-tasks.md` | Rewrite the Q&A section around participants; add the `--to` flag and the answer-rights table to the CLI block. This is where the model is described in full. |
| `docs/10-agents.md` | Replace the stale «Q&A-треды» section (it still names `agent_questions`/`agent_question_messages` and claims only the human closes a thread); add `answer` and `--to` to the CLAUDE.md snippet. |
| `docs/03-daemon-api.md` | Correct the question/agent-question route rows: new response fields, the `to` request field, the widened permissions, the dropped tables. |
| `docs/11-dashboard.md` | Correct the badge/banner wording to `your_turn` and participants. |
| `internal/prompts/templates/orchestrator.md` | Add the participant behaviours; correct the delivery frames quoted in the prompt. |
| `docs/prompts/orchestrator.md` | Mirror of the above. |

**Where the «persistent-agent prompt» is.** The brief asks for «the prompts
under `internal/prompts/` (orchestrator prompt and persistent-agent prompt)».
There is no such template: `internal/prompts/prompts.go:110` lists exactly
`kickoff`, `orchestrator`, `worker`, and `session.StartAgent`
(`internal/session/agent.go:19-23`) sets no system prompt at all — «rocket sets
no system prompt and manages no lifecycle — the agent's context is its own
CLAUDE.md». The only prompt text rocket ships for a persistent agent is the
CLAUDE.md snippet at `docs/10-agents.md:165-189`, so that snippet is what
Task 2 rewrites. The orchestrator was told (message 8145 and the follow-up).

`internal/prompts/templates/worker.md` is deliberately **not** touched: spec v1
«Не входит в скоуп» says workers do not become participants by default, and the
worker prompt makes no claim about Q&A that has become false.

---

### Task 1: The participant model in `docs/12-tasks.md`

**Files:**
- Modify: `docs/12-tasks.md:64-91` (section «Вопросы и ответы через задачу») and
  `docs/12-tasks.md:93-114` (CLI block)

**Interfaces:**
- Produces: the section other docs link to as
  `[12-tasks.md](12-tasks.md)#вопросы-и-ответы-через-задачу`. Tasks 2–4 link
  here instead of restating the model.

- [ ] **Step 1: Rewrite the Q&A section around participants**

Keep the existing narrative spine (a question is a thread, not one pair;
reopening is a narrow tool) — that is all still true — and replace the
two-party framing. The section must state, each backed by the ground-truth
table above:

- a thread has a list of participants; an id is `human`, a persistent agent id
  (`cto`), or a session id (`reply-answer-orch`);
- you join by being named in `--to`, or by writing into the thread; at creation
  both sides are seeded — `human` always, and the task's orchestrator
  (`task.SessionID`) when the task has one;
- `waiting_on` — resolved thread waits on nobody; a message sent with `--to`
  puts exactly its addressees in the queue; otherwise everyone but the last
  author. `your_turn` is that list intersected with whoever is reading;
- **a caveat stated plainly:** `--to` on `ask` adds the addressees and notifies
  them, but does not narrow the turn of a thread that has no messages yet —
  there the turn still follows `asked_by`. (Do not mention #737 or spec v2;
  document what is true.)
- `answer` / `dismiss` close a thread and are available to the human **and to
  persistent agents** — an orchestrator or worker gets `403` telling it to use
  `reply`. Replace the current claim «Закрывает вопрос только пользователь».
- every new entry is delivered to every participant except its author, framed
  `[task #12 Q3 reply from cto] <текст>` — the ` from <id>` part is uniform,
  including `from human`. `--to` never narrows delivery, only the turn.
- the reopen rule is now «любой участник, кроме человека» rather than «свой
  оркестратор» (`internal/api/questions.go:452-459`).

Keep the last paragraph's point that workers do not use the mechanism, but
correct it: a worker is not a participant by default, yet it may be pulled into
a thread with `--to`, and it may read its root task's threads.

- [ ] **Step 2: Update the CLI block**

The CLI block at `docs/12-tasks.md:93-110` gains `--to`:

```
rocket task ask <id> "<вопрос>" [--context <md>] [--to a,b]
rocket task reply <question-id> "<текст>" [--to a,b]
rocket task answer <question-id> "<ответ>"          # человек и постоянный агент
rocket task answer <question-id> --dismiss
```

Note under it: `--to` — comma-separated participant ids; the named ids join the
thread and are notified. Absent `--to`, behaviour is unchanged.

- [ ] **Step 3: Extend the rights table**

The «Кто что может с задачами» table at `docs/12-tasks.md:116-123` is about
tasks, not threads. Add a second, separate table for threads with the four
actions (`ask`, `reply`, `answer`/`dismiss`, чтение) and four callers (человек,
постоянный агент, оркестратор, воркер), filled from the ground-truth table.

- [ ] **Step 4: Verify the section against the code**

Run: `grep -n "addressed_to" internal/store/migrations/*.sql`
Expected: hits only in the `question_messages` block of `0009_threads.sql` —
confirming the `ask --to` caveat in Step 1 is still accurate. If a `0010`
migration adding `questions.addressed_to` has appeared (i.e. #737 merged),
stop and ask the orchestrator before writing the caveat.

- [ ] **Step 5: Commit**

```bash
git add docs/12-tasks.md
git commit -m "docs: describe the participant model in the task Q&A section"
```

---

### Task 2: `docs/10-agents.md` — role threads and the agent snippet

**Files:**
- Modify: `docs/10-agents.md:147-163` (section «Q&A-треды»),
  `docs/10-agents.md:165-189` (снippet «Сниппет для CLAUDE.md агента»)

**Interfaces:**
- Consumes: the section anchor produced by Task 1.

- [ ] **Step 1: Fix the «Q&A-треды» section**

Three statements there are now false and must be replaced:

1. «таблицы `agent_questions` / `agent_question_messages`» — those tables were
   dropped in `0009_threads.sql:68-69`; a role thread is a row in `questions`
   with `role_id` set.
2. «Закрывает тред только человек» — `canAnswerThread`
   (`internal/api/threads.go:126-128`) also allows a persistent agent, so an
   agent can close a thread on its own role.
3. «Агент может оспорить закрытый тред» — true, but the rule is now general:
   any participant except the human reopens by replying.

Also record that a role thread has participants like any other, that the
delivery frame keeps the `role` token and gains ` from <id>`
(`[role cto Q2 reply from reply-answer-orch]`), and that `--to` works on
`rocket agent ask|reply|answer`. Link the full model to Task 1's section rather
than restating it.

- [ ] **Step 2: Update the CLAUDE.md snippet**

The snippet at `docs/10-agents.md:170-189` is the only prompt a persistent agent
gets from rocket, and it currently omits everything this feature added. It must
tell the agent, in the same terse voice as the surrounding bullets:

- it is a full participant: it can be pulled into a task thread with
  `--to <its-id>` and will be notified of every entry in threads it takes part
  in, live in the session or via the inbox when it is down;
- `rocket task questions` / `rocket task reply <qid> "..."` — it may reply to
  task threads, not just to its own role threads;
- `rocket task answer <qid> "<ответ>" | --dismiss` — it **may** close a task
  thread; this is the decision an orchestrator was blocked on, so it should use
  it rather than leaving the orchestrator waiting for the human;
- the workaround it used to need (`unset ROCKET_SESSION_ID` to pass as the
  human) is no longer necessary and must not be used — it destroys authorship.

- [ ] **Step 3: Commit**

```bash
git add docs/10-agents.md
git commit -m "docs: role threads and the agent CLAUDE.md snippet on participants"
```

---

### Task 3: `docs/03-daemon-api.md` — the route contract

**Files:**
- Modify: `docs/03-daemon-api.md:95-101` (task question routes),
  `docs/03-daemon-api.md:121-130` (agent question routes)

- [ ] **Step 1: Document the response shape once**

Above the task-question rows, add a short paragraph naming the fields added by
#731 — `participants[]`, `waiting_on[]`, `your_turn`, and `messages[].addressed_to`
— plus the compatibility note that `asked_by` and `messages[].author` still
carry `""` for the human while the participant fields carry `human`
(`internal/api/questions.go:25-35`), and that `whose_turn` survives as derived
from `waiting_on`.

- [ ] **Step 2: Correct the three task-question rows**

- `POST /v1/tasks/{id}/questions` — body is `{body, context?, to?}`; the caller
  may be the human, a persistent agent, or the task's own orchestrator; the
  entry is delivered to every seeded participant except the author.
  The current row's claim that an orchestrator-opened question goes out «без
  доставки» is false now: fan-out delivers it to every other participant.
- `POST /v1/questions/{id}/reply` — `{body, to?}`; any participant; resolved →
  `409` for the human, reopen for anybody else.
- `POST /v1/questions/{id}/answer` — `{body|dismiss, to?}`; **человек и
  постоянный агент**; others get `403 forbidden` with the "use reply" message.

- [ ] **Step 3: Correct the agent-question rows and the paragraph under them**

Same three corrections for `/v1/agents/{id}/questions`,
`/v1/agent-questions/{qid}/reply` and `.../answer`. The paragraph at line 126
must drop the `agent_questions`/`agent_question_messages` table names, and the
permissions paragraph at line 130 must be replaced with the participant rule:
`kind=agent` callers and the human read and post everywhere; an orchestrator or
worker only where it participates or on its root task
(`internal/api/threads.go:244-266`).

- [ ] **Step 4: Commit**

```bash
git add docs/03-daemon-api.md
git commit -m "docs: question routes carry participants, waiting_on and to"
```

---

### Task 4: `docs/11-dashboard.md` — badges and banner

**Files:**
- Modify: `docs/11-dashboard.md:73-90`, `docs/11-dashboard.md:144`

- [ ] **Step 1: Restate the badge in participant terms**

Line 77 says the badge means «оркестратор задал вопрос через `task ask` и ждёт
вас». It now means `your_turn` — the reader is in `waiting_on` — which can come
from any participant, not only the task's orchestrator. Rewrite accordingly and
mention that the thread shows its participant list and that a `--to` reply from
the human moves the badge to that addressee instead.

- [ ] **Step 2: Correct the banner actions**

Line 90 says «Ответить и закрыть» is the human's. Keep it, but note the same
action exists for a persistent agent through the API/CLI, so a thread may close
without the human. Do not invent UI that does not exist: web and mobile are
subtasks #733/#734 — describe only the model, and leave the concrete controls
to whatever those land.

- [ ] **Step 3: Commit**

```bash
git add docs/11-dashboard.md
git commit -m "docs: dashboard question badges follow your_turn"
```

---

### Task 5: The orchestrator prompt

**Files:**
- Modify: `internal/prompts/templates/orchestrator.md:54-96`
- Modify: `docs/prompts/orchestrator.md` (mirror)
- Test: `internal/prompts/prompts_test.go` (run only; no new test needed —
  the suite renders every template and fails on an unresolved placeholder)

- [ ] **Step 1: Update the delivery frames quoted in the prompt**

Lines 63, 67 and 89-90 quote `[task #{{task_id}} QN reply]`,
`... QN answer]` and `... QN question]`. The frame now carries an author:
`[task #{{task_id}} QN reply from <id>]`. Update all three so the orchestrator
recognises what actually arrives.

- [ ] **Step 2: Add the two new behaviours**

In «Asking the human», after the existing `rocket task ask` bullet:

- a thread may have more participants than you and the human. `--to <ids>`
  (comma-separated) on `ask` or `reply` pulls somebody in — typically a
  persistent agent like `cto` — and they are notified. Prefer addressing the
  standing role that owns the decision over escalating to the human;
- caveat, so the prompt does not lie: on `ask` the addressees are added and
  notified, but the human is still shown as waiting until somebody replies. Use
  `--to` on a `reply` when you specifically need the turn to move;
- **a persistent agent may close your thread with `answer`.** A resolved thread
  is a decision regardless of who resolved it — the existing reopen rules apply
  unchanged.

In «The human asking you», the sentence «only the human resolves a thread they
opened» becomes «only the human or a persistent agent resolves it — you still
cannot: `rocket task answer` returns `403` telling you to use `reply`». That is
the literal server message (`internal/api/questions.go:565-566`).

Also: you may be pulled into a thread you did not open — a role thread of a
persistent agent, for instance. Those arrive framed `[role cto Q2 reply from ...]`.
Reply in the thread (`rocket agent reply <qid>`), do not answer in the terminal.

- [ ] **Step 3: Render the template and check no placeholder broke**

Run: `go test ./internal/prompts/...`
Expected: PASS. (The suite renders the templates; a stray `{{...}}` typed into
the new text would fail it.)

- [ ] **Step 4: Mirror into `docs/prompts/orchestrator.md`**

The mirror is the same text inside the existing ``` fence, under the existing
Russian preamble. Note that the mirror is currently stale in one place
(`docs/prompts/orchestrator.md:69-73` has a markdown-formatting bullet the
template lacks). Re-sync the whole fenced block from the template so the two
stop drifting, keeping the preamble.

Run: `diff <(sed -n '/^```$/,$p' docs/prompts/orchestrator.md) internal/prompts/templates/orchestrator.md`
Expected: differences only in the fence lines themselves.

- [ ] **Step 5: Commit**

```bash
git add internal/prompts/templates/orchestrator.md docs/prompts/orchestrator.md
git commit -m "prompts: orchestrator knows about thread participants and --to"
```

---

### Task 6: Verification and PR

- [ ] **Step 1: Full test run**

Run: `go build ./... && go test ./internal/prompts/... ./internal/api/... ./internal/store/...`
Expected: PASS. (`go test ./...` is known to fail in `internal/cli` for an
unrelated reason recorded on task #722: `TestLoadConfigNoOverrideWhenSocketFlagEmpty`
reads the real `~/.rocket/rocket.sock`. Confirm that is the only failure and do
not try to fix it here.)

- [ ] **Step 2: Re-read every claim against the code**

For each ground-truth row in the table at the top of this plan, confirm the
statement in the docs matches the cited file. Use
`superpowers:verification-before-completion`; a documentation change has no
tests, so this reading IS the verification.

- [ ] **Step 3: Open the PR**

```bash
git push -u origin feature/reply-answer/docs-participants
gh pr create --title "docs: multi-participant question threads" --body "..."
```

The body references feature `reply-answer` and subtask #735, lists the files,
and calls out explicitly that spec v2's `ask --to` turn-narrowing (#737) is NOT
documented because it is not merged.
