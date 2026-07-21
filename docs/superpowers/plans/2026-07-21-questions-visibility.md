# Questions Visibility & Screenshots Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make open orchestrator questions visible everywhere (kanban badge, global Questions page), fix thread formatting, and support pasting screenshots via Ctrl+V in questions and chat.

**Architecture:** Go daemon (`internal/store` SQLite + `internal/api` net/http) gains per-task open-question counts on list/board responses, a global `GET /v1/questions` endpoint, and an attachments subsystem (files in `~/.rocket/attachments`, rows in SQLite, links rewritten to absolute paths at message-enqueue time so the tmux-injected text lets the agent Read the image). React web (`web/`, react-query + msw/vitest) gains a kanban badge, a `/questions` page reusing the extracted `QuestionThread` component, CSS fixes, and a `usePasteImage` hook.

**Tech Stack:** Go 1.x + modernc.org/sqlite, net/http `ServeMux` with method patterns, React 18 + TypeScript + @tanstack/react-query + react-markdown, vitest + msw + testing-library.

**Spec:** `docs/superpowers/specs/2026-07-21-questions-visibility-design.md`

## Global Constraints

- Follow existing file/naming patterns; comments in the style already present (English, "why"-focused).
- Go tests: `go test ./internal/...` from repo root. Web tests: `npm test` from `web/`.
- Attachment MIME allowlist: `image/png`, `image/jpeg`, `image/webp`; size limit 10 MiB.
- Attachment URL shape: `/v1/attachments/{id}`; markdown inserted by web: `![screenshot](/v1/attachments/{id})`.
- Agent-facing rewrite of an attachment link: `[screenshot: <absolute file path>]`.
- API error envelope: `writeErr(w, status, code, message)` — codes are snake_case.
- Commit after every task; messages in the repo's `feat(scope):`/`fix(scope):` style.

---

### Task 1: Formatting fixes (CSS + orchestrator prompt)

**Files:**
- Modify: `web/src/screens/task/QuestionsTab.css:256-261` (context body), `:322-327` (message body)
- Modify: `docs/prompts/orchestrator.md` (question instructions around lines 55-80)

**Interfaces:**
- Consumes: nothing.
- Produces: nothing code-visible; pure presentation + prompt copy.

Root cause (from user screenshots): `.question-thread__context-body` renders Markdown output in `var(--font-mono)` with `white-space: pre-wrap` → monospace wall of text; `.question-thread__message-body` also has `white-space: pre-wrap` on a container holding rendered Markdown → source newlines display as literal blank lines *on top of* paragraph margins, doubling every gap.

- [ ] **Step 1: Fix context-body CSS**

In `web/src/screens/task/QuestionsTab.css` replace:

```css
.question-thread__context-body {
  padding: 15px 18px;
  font: 13.5px/1.7 var(--font-mono);
  color: var(--text-2);
  white-space: pre-wrap;
}
```

with:

```css
.question-thread__context-body {
  padding: 15px 18px;
  font: 13.5px/1.6 var(--font-ui);
  color: var(--text-2);
}
```

- [ ] **Step 2: Fix message-body CSS**

In the same file replace:

```css
.question-thread__message-body {
  font: 14.5px/1.75 var(--font-ui);
  color: var(--text-2-alt);
  padding-left: 33px;
  white-space: pre-wrap;
}
```

with:

```css
.question-thread__message-body {
  font: 14.5px/1.65 var(--font-ui);
  color: var(--text-2-alt);
  padding-left: 33px;
}
```

- [ ] **Step 3: Add markdown-formatting requirement to the orchestrator prompt**

In `docs/prompts/orchestrator.md`, directly after the `rocket task ask {{task_id}} "<question>" [--context "<details>"]` instruction block (around line 68, after "The question is surfaced to the human in the dashboard."), add:

```markdown
- Format the question body and --context as markdown the dashboard can
  render: use bullet or numbered lists instead of inline "(1) … (2) …"
  enumerations, put a blank line between logical blocks, and keep the body
  itself to 1-2 sentences (details belong in --context). Never send a
  single wall-of-text paragraph.
```

- [ ] **Step 4: Run web tests**

Run: `cd web && npm test`
Expected: PASS (CSS-only change; existing QuestionsTab tests don't assert styles)

- [ ] **Step 5: Commit**

```bash
git add web/src/screens/task/QuestionsTab.css docs/prompts/orchestrator.md
git commit -m "fix(web): render question context and replies as proper markdown, not pre-wrapped mono"
```

---

### Task 2: Store — OpenQuestionCounts

**Files:**
- Modify: `internal/store/questions.go`
- Test: `internal/store/questions_test.go`

**Interfaces:**
- Consumes: existing tables `task_questions`, `question_messages`; test helpers `openTestStore(t)`, `mustAddQuestionTask(t, s)`.
- Produces: `type QuestionCounts struct { Open, AwaitingUser int }` and `func (s *Store) OpenQuestionCounts() (map[int64]QuestionCounts, error)` — map keyed by task id, containing only tasks with ≥1 open question. "Awaiting user" mirrors `whoseTurn` in `internal/api/questions.go`: no thread messages → user's turn iff `asked_by != ''`; otherwise user's turn iff the last message's author is an orchestrator (non-NULL, non-empty).

- [ ] **Step 1: Write the failing test**

Append to `internal/store/questions_test.go`:

```go
func TestOpenQuestionCounts(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)

	// No questions at all: task absent from the map.
	counts, err := s.OpenQuestionCounts()
	if err != nil {
		t.Fatalf("OpenQuestionCounts: %v", err)
	}
	if _, ok := counts[taskID]; ok {
		t.Errorf("counts[%d] present, want absent", taskID)
	}

	// Q1: orchestrator-opened, no messages -> awaiting user.
	q1, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch-1", Body: "Q1"})
	if err != nil {
		t.Fatalf("AddQuestion q1: %v", err)
	}
	// Q2: orchestrator-opened, last message from human -> awaiting orchestrator.
	q2, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch-1", Body: "Q2"})
	if err != nil {
		t.Fatalf("AddQuestion q2: %v", err)
	}
	if _, err := s.AddQuestionMessage(QuestionMessage{QuestionID: q2, Author: "", Body: "human reply"}); err != nil {
		t.Fatalf("AddQuestionMessage q2: %v", err)
	}
	// Q3: user-opened, last message from orchestrator -> awaiting user.
	q3, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "", Body: "Q3"})
	if err != nil {
		t.Fatalf("AddQuestion q3: %v", err)
	}
	if _, err := s.AddQuestionMessage(QuestionMessage{QuestionID: q3, Author: "orch-1", Body: "orch reply"}); err != nil {
		t.Fatalf("AddQuestionMessage q3: %v", err)
	}
	// Q4: resolved -> excluded entirely.
	q4, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch-1", Body: "Q4"})
	if err != nil {
		t.Fatalf("AddQuestion q4: %v", err)
	}
	if err := s.ResolveQuestion(q4, "answered"); err != nil {
		t.Fatalf("ResolveQuestion q4: %v", err)
	}
	_ = q1

	counts, err = s.OpenQuestionCounts()
	if err != nil {
		t.Fatalf("OpenQuestionCounts: %v", err)
	}
	got := counts[taskID]
	if got.Open != 3 {
		t.Errorf("Open = %d, want 3", got.Open)
	}
	if got.AwaitingUser != 2 {
		t.Errorf("AwaitingUser = %d, want 2 (q1 no-messages + q3 orch-last)", got.AwaitingUser)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestOpenQuestionCounts -v`
Expected: FAIL — compile error `s.OpenQuestionCounts undefined`

- [ ] **Step 3: Implement**

Append to `internal/store/questions.go`:

```go
// QuestionCounts summarizes a task's open questions for board/list views.
type QuestionCounts struct {
	Open         int
	AwaitingUser int
}

// OpenQuestionCounts returns, per task with at least one open question, how
// many questions are open and how many of those await the human. "Awaiting
// the human" mirrors the whoseTurn derivation in internal/api/questions.go:
// with no thread messages the question itself counts as the last entry (so
// an orchestrator-opened question awaits the human, a user-opened one
// doesn't); otherwise the last message's author decides (orchestrator
// author -> human's turn). Computed in one query so list/board handlers can
// annotate every task without an N+1.
func (s *Store) OpenQuestionCounts() (map[int64]QuestionCounts, error) {
	rows, err := s.db.Query(`
		SELECT task_id, COUNT(*), SUM(turn_user) FROM (
			SELECT q.task_id AS task_id,
				CASE
					WHEN m.id IS NULL THEN (CASE WHEN q.asked_by != '' THEN 1 ELSE 0 END)
					WHEN m.author IS NOT NULL AND m.author != '' THEN 1
					ELSE 0
				END AS turn_user
			FROM task_questions q
			LEFT JOIN question_messages m
				ON m.question_id = q.id
				AND m.id = (SELECT MAX(id) FROM question_messages WHERE question_id = q.id)
			WHERE q.status = 'open'
		) GROUP BY task_id`)
	if err != nil {
		return nil, fmt.Errorf("query open question counts: %w", err)
	}
	defer rows.Close()

	out := make(map[int64]QuestionCounts)
	for rows.Next() {
		var taskID int64
		var c QuestionCounts
		if err := rows.Scan(&taskID, &c.Open, &c.AwaitingUser); err != nil {
			return nil, fmt.Errorf("scan open question counts: %w", err)
		}
		out[taskID] = c
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestOpenQuestionCounts -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/store/questions.go internal/store/questions_test.go
git commit -m "feat(store): OpenQuestionCounts for board question badges"
```

---

### Task 3: API — question counts on task list/board/detail

**Files:**
- Modify: `internal/api/tasks.go`
- Test: `internal/api/tasks_test.go`

**Interfaces:**
- Consumes: `Store.OpenQuestionCounts()` from Task 2.
- Produces: `taskResponse` gains `OpenQuestions int json:"open_questions"` and `QuestionsAwaitingUser int json:"questions_awaiting_user"` (no omitempty — web relies on the fields always being present). `taskDetailResponse` loses its own `OpenQuestions` field (the embedded one now carries it — same JSON shape as before). Board/list/detail all populate the two fields.

- [ ] **Step 1: Write the failing test**

Append to `internal/api/tasks_test.go`:

```go
// TestListTasks_QuestionCounts: board and list responses annotate each task
// with open_questions / questions_awaiting_user from a single counts query.
func TestListTasks_QuestionCounts(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	// One open orchestrator-asked question (awaiting user), one resolved.
	if _, err := d.Store.AddQuestion(store.Question{TaskID: taskID, AskedBy: "orch-1", Body: "open q"}); err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}
	qid, err := d.Store.AddQuestion(store.Question{TaskID: taskID, AskedBy: "orch-1", Body: "done q"})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}
	if err := d.Store.ResolveQuestion(qid, "answered"); err != nil {
		t.Fatalf("ResolveQuestion: %v", err)
	}

	resp := getJSON(t, srv.URL+"/v1/tasks?board=true")
	defer resp.Body.Close()
	var boardResp struct {
		Board struct {
			Backlog []struct {
				ID                    int64 `json:"id"`
				OpenQuestions         int   `json:"open_questions"`
				QuestionsAwaitingUser int   `json:"questions_awaiting_user"`
			} `json:"backlog"`
		} `json:"board"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&boardResp); err != nil {
		t.Fatalf("decode board: %v", err)
	}
	if len(boardResp.Board.Backlog) != 1 {
		t.Fatalf("backlog len = %d, want 1", len(boardResp.Board.Backlog))
	}
	got := boardResp.Board.Backlog[0]
	if got.OpenQuestions != 1 || got.QuestionsAwaitingUser != 1 {
		t.Errorf("counts = %d/%d, want 1/1", got.OpenQuestions, got.QuestionsAwaitingUser)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestListTasks_QuestionCounts -v`
Expected: FAIL — `counts = 0/0, want 1/1`

- [ ] **Step 3: Implement**

In `internal/api/tasks.go`:

1. Add to `taskResponse` (after `CompletedAt`):

```go
	// Open-question annotations (docs/superpowers/specs/2026-07-21-questions-
	// visibility-design.md §1): populated by list/board/detail handlers from
	// one Store.OpenQuestionCounts() call, not per-task queries.
	OpenQuestions         int `json:"open_questions"`
	QuestionsAwaitingUser int `json:"questions_awaiting_user"`
```

2. Change `taskDetailResponse` — delete its `OpenQuestions` field:

```go
type taskDetailResponse struct {
	taskResponse
	Subtasks []subtaskResponse    `json:"subtasks"`
	Session  *taskSessionResponse `json:"session,omitempty"`
}
```

3. Delete the `countOpenQuestions` helper (its only caller changes below).

4. Add an annotation helper next to `toTaskBoard`:

```go
// annotateQuestionCounts fills the open-question fields on tr from counts.
func annotateQuestionCounts(tr *taskResponse, counts map[int64]store.QuestionCounts) {
	c := counts[tr.ID]
	tr.OpenQuestions = c.Open
	tr.QuestionsAwaitingUser = c.AwaitingUser
}
```

5. In `handleListTasks`, after `tasks, err := d.Store.ListTasks(filter)` succeeds, load counts once and thread them through both branches:

```go
	counts, err := d.Store.OpenQuestionCounts()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	if q.Get("board") == "true" {
		writeJSON(w, http.StatusOK, map[string]any{"board": toTaskBoard(tasks, counts)})
		return
	}

	out := make([]taskResponse, len(tasks))
	for i, t := range tasks {
		out[i] = toTaskResponse(t)
		annotateQuestionCounts(&out[i], counts)
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": out})
```

6. Change `toTaskBoard` to accept and apply counts:

```go
func toTaskBoard(tasks []store.Task, counts map[int64]store.QuestionCounts) taskBoard {
```

and inside the loop, after `tr := toTaskResponse(t)`, add `annotateQuestionCounts(&tr, counts)`.

7. In `handleGetTask`, replace the `openQ, err := countOpenQuestions(d, id)` block and the final `writeJSON` with:

```go
	counts, err := d.Store.OpenQuestionCounts()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	tr := toTaskResponse(t)
	annotateQuestionCounts(&tr, counts)

	writeJSON(w, http.StatusOK, taskDetailResponse{
		taskResponse: tr,
		Subtasks:     subOut,
		Session:      sessResp,
	})
```

- [ ] **Step 4: Run the full api + store packages**

Run: `go test ./internal/api/ ./internal/store/`
Expected: PASS (if an existing detail test asserts `open_questions`, the JSON shape is unchanged — the field merely moved into the embedded struct)

- [ ] **Step 5: Commit**

```bash
git add internal/api/tasks.go internal/api/tasks_test.go
git commit -m "feat(api): open-question counts on task list, board and detail"
```

---

### Task 4: Web — kanban card question badge

**Files:**
- Modify: `web/src/lib/types.ts` (Task interface), `web/src/screens/kanban/TaskCard.tsx`, `web/src/mocks/fixtures.ts`
- Test: `web/src/screens/kanban/Kanban.test.tsx`

**Interfaces:**
- Consumes: `open_questions` / `questions_awaiting_user` from Task 3; `Badge` component (`tone: 'warn' | 'neutral'`).
- Produces: badge markup with test-visible texts `? N awaiting you` / `? N open`.

- [ ] **Step 1: Extend the Task type**

In `web/src/lib/types.ts`, add to `export interface Task` after `completed_at?: number`:

```ts
  /** Open-question annotations (list/board/detail all carry them). */
  open_questions: number
  questions_awaiting_user: number
```

Run `cd web && npx tsc -b --noEmit` (or `npm run build`) — fixtures will now fail the type check; fix in Step 2.

- [ ] **Step 2: Update fixtures**

In `web/src/mocks/fixtures.ts`, every `Task` literal (the `tasks` and `subtasks` arrays) needs the two new fields. Give all of them `open_questions: 0, questions_awaiting_user: 0` except one non-backlog board task (pick the task with id 12 used by the PR-badges test, or the first `in_progress` root task): give it `open_questions: 2, questions_awaiting_user: 1`.

Also, in `web/src/mocks/handlers.ts` the task-detail handler computes `open_questions: openQuestionsFor(task.id)` (line ~159) — extend that object with `questions_awaiting_user: 0` is NOT needed if the spread of the task literal already carries both fields; verify the detail response object doesn't drop them (it spreads `...task`, so it keeps them).

- [ ] **Step 3: Write the failing test**

Append to `web/src/screens/kanban/Kanban.test.tsx` (follow the file's existing render helper conventions):

```tsx
test('question badge: warn "awaiting you" when questions_awaiting_user > 0', async () => {
  renderKanban() // use the same helper the existing tests in this file use
  expect(await screen.findByText('? 1 awaiting you')).toBeInTheDocument()
})
```

Run: `cd web && npm test -- Kanban`
Expected: FAIL — text not found

- [ ] **Step 4: Implement the badge**

In `web/src/screens/kanban/TaskCard.tsx`, replace the stale comment block:

```tsx
      {/* Signals (open_questions): not on the list/board taskResponse, only
          per-task detail — skipping to avoid an N+1 fetch per card. Will
          show up once the task's own screen is open. */}
```

with:

```tsx
      {task.questions_awaiting_user > 0 ? (
        <div className="kanban-card__questions">
          <Badge tone="warn">? {task.questions_awaiting_user} awaiting you</Badge>
        </div>
      ) : task.open_questions > 0 ? (
        <div className="kanban-card__questions">
          <Badge tone="neutral">? {task.open_questions} open</Badge>
        </div>
      ) : null}
```

Add to `web/src/screens/kanban/kanban.css` (next to `.kanban-card__pr-badges`):

```css
.kanban-card__questions {
  display: flex;
  gap: 6px;
  margin-top: 7px;
}
```

- [ ] **Step 5: Run tests**

Run: `cd web && npm test`
Expected: PASS (all suites — fixtures change affects other tests only via added fields)

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/types.ts web/src/mocks/fixtures.ts web/src/mocks/handlers.ts web/src/screens/kanban/
git commit -m "feat(web): open-question badge on kanban cards"
```

---

### Task 5: API — global GET /v1/questions

**Files:**
- Modify: `internal/store/questions.go`, `internal/api/questions.go`
- Test: `internal/store/questions_test.go`, `internal/api/questions_test.go`

**Interfaces:**
- Consumes: `buildQuestionResponse`, `registerQuestionRoutes`, `Store.GetTask/GetProject/GetSession`.
- Produces: `Store.ListAllOpenQuestions() ([]Question, error)`; `GET /v1/questions` → `200 {"questions": [globalQuestionResponse]}` where `globalQuestionResponse` = `questionResponse` + `task_title`, `project_id`, `project_name`, `orchestrator_name` (omitempty, = the orchestrator session's `tmux_name`).

- [ ] **Step 1: Write the failing store test**

Append to `internal/store/questions_test.go`:

```go
func TestListAllOpenQuestions(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)

	q1, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch-1", Body: "open"})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}
	q2, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch-1", Body: "closed"})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}
	if err := s.ResolveQuestion(q2, "answered"); err != nil {
		t.Fatalf("ResolveQuestion: %v", err)
	}

	got, err := s.ListAllOpenQuestions()
	if err != nil {
		t.Fatalf("ListAllOpenQuestions: %v", err)
	}
	if len(got) != 1 || got[0].ID != q1 {
		t.Fatalf("got %+v, want exactly q1(%d)", got, q1)
	}
}
```

Run: `go test ./internal/store/ -run TestListAllOpenQuestions -v`
Expected: FAIL — compile error, method undefined

- [ ] **Step 2: Implement the store method**

Append to `internal/store/questions.go`:

```go
// ListAllOpenQuestions returns every open question across all tasks,
// ascending by id — the backing query for the dashboard's global
// Questions page.
func (s *Store) ListAllOpenQuestions() ([]Question, error) {
	rows, err := s.db.Query(
		`SELECT id, task_id, asked_by, body, context, status, resolution, asked_at, resolved_at
		 FROM task_questions WHERE status = 'open' ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query all open questions: %w", err)
	}
	defer rows.Close()

	var out []Question
	for rows.Next() {
		q, err := scanQuestion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
```

Run: `go test ./internal/store/ -run TestListAllOpenQuestions -v`
Expected: PASS

- [ ] **Step 3: Write the failing API test**

Append to `internal/api/questions_test.go`:

```go
// TestGetAllQuestions: the global list enriches each open question with its
// task title, project and orchestrator tmux name.
func TestGetAllQuestions(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	if _, err := d.Store.AddQuestion(store.Question{TaskID: taskID, AskedBy: "orch-1", Body: "open q"}); err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}
	qid, err := d.Store.AddQuestion(store.Question{TaskID: taskID, AskedBy: "orch-1", Body: "resolved q"})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}
	if err := d.Store.ResolveQuestion(qid, "answered"); err != nil {
		t.Fatalf("ResolveQuestion: %v", err)
	}

	resp := getJSON(t, srv.URL+"/v1/questions")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Questions []struct {
			questionResponse
			TaskTitle        string `json:"task_title"`
			ProjectID        string `json:"project_id"`
			ProjectName      string `json:"project_name"`
			OrchestratorName string `json:"orchestrator_name"`
		} `json:"questions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Questions) != 1 {
		t.Fatalf("len = %d, want 1 (resolved excluded)", len(body.Questions))
	}
	got := body.Questions[0]
	if got.Body != "open q" || got.TaskID != taskID {
		t.Errorf("question mismatch: %+v", got.questionResponse)
	}
	if got.TaskTitle != "Root" || got.ProjectID != "proj1" {
		t.Errorf("task enrichment = %q/%q, want Root/proj1", got.TaskTitle, got.ProjectID)
	}
	if got.OrchestratorName == "" {
		t.Errorf("orchestrator_name empty, want the orch-1 session tmux name")
	}
}
```

Run: `go test ./internal/api/ -run TestGetAllQuestions -v`
Expected: FAIL — 404 (route not registered)

Note: check what `addTestProject`/`addTestSession` in `tasks_test.go` actually set for project `Name` and session `TmuxName`, and align the two assertions above with those values before running (the point of the assertions is "enrichment flows through", not specific strings).

- [ ] **Step 4: Implement the handler**

In `internal/api/questions.go`:

1. Add to `registerQuestionRoutes`:

```go
	mux.HandleFunc("GET /v1/questions", func(w http.ResponseWriter, r *http.Request) {
		handleGetAllQuestions(w, r, d)
	})
```

2. Add the response type and handler:

```go
// globalQuestionResponse is one entry of GET /v1/questions: a full question
// thread plus the task/project context the per-task endpoints get for free
// from their URL.
type globalQuestionResponse struct {
	questionResponse
	TaskTitle        string `json:"task_title"`
	ProjectID        string `json:"project_id"`
	ProjectName      string `json:"project_name"`
	OrchestratorName string `json:"orchestrator_name,omitempty"`
}

// handleGetAllQuestions serves GET /v1/questions: every open question across
// all tasks, enriched with task title, project and orchestrator name for the
// dashboard's global Questions page. Questions whose task has vanished are
// skipped rather than failing the whole listing.
func handleGetAllQuestions(w http.ResponseWriter, r *http.Request, d Deps) {
	qs, err := d.Store.ListAllOpenQuestions()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	tasks := map[int64]store.Task{}
	projectNames := map[string]string{}
	orchNames := map[string]string{}

	out := make([]globalQuestionResponse, 0, len(qs))
	for _, q := range qs {
		task, ok := tasks[q.TaskID]
		if !ok {
			task, err = d.Store.GetTask(q.TaskID)
			if errors.Is(err, store.ErrNotFound) {
				continue
			}
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
				return
			}
			tasks[q.TaskID] = task
		}

		resp, err := buildQuestionResponse(d, q)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}

		g := globalQuestionResponse{
			questionResponse: resp,
			TaskTitle:        task.Title,
			ProjectID:        task.ProjectID,
		}
		if name, ok := projectNames[task.ProjectID]; ok {
			g.ProjectName = name
		} else if p, err := d.Store.GetProject(task.ProjectID); err == nil {
			projectNames[task.ProjectID] = p.Name
			g.ProjectName = p.Name
		}
		if task.SessionID != "" {
			if name, ok := orchNames[task.SessionID]; ok {
				g.OrchestratorName = name
			} else if sess, err := d.Store.GetSession(task.SessionID); err == nil {
				orchNames[task.SessionID] = sess.TmuxName
				g.OrchestratorName = sess.TmuxName
			}
		}
		out = append(out, g)
	}
	writeJSON(w, http.StatusOK, map[string]any{"questions": out})
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/api/ ./internal/store/`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/store/questions.go internal/store/questions_test.go internal/api/questions.go internal/api/questions_test.go
git commit -m "feat(api): GET /v1/questions — all open questions across projects"
```

---

### Task 6: Web — extract QuestionThread component

**Files:**
- Create: `web/src/components/QuestionThread.tsx`, `web/src/components/questionthread.css`
- Modify: `web/src/screens/task/QuestionsTab.tsx`, `web/src/screens/task/QuestionsTab.css`

**Interfaces:**
- Consumes: existing `ThreadCard` code in `QuestionsTab.tsx` (moved verbatim), `useReplyQuestion`, `useAnswerQuestion`, `Markdown`, `timeAgo`.
- Produces: `export function QuestionThread(props: { taskId: number; question: Question; orchestratorName?: string })` — the former `ThreadCard`, renamed; `export function authorLabel(author: string | undefined, orchestratorName?: string): string` (still needed by `ResolvedThreadRow`). Pure refactor: no behavior change, all existing tests stay green.

- [ ] **Step 1: Create the component**

Create `web/src/components/QuestionThread.tsx`. Move from `QuestionsTab.tsx`, verbatim except for the renames and imports: the functions `whoseTurnLabel`, `authorLabel` (add `export`), `askerLabel`, and `ThreadCard` renamed to `QuestionThread` (its `ThreadCardProps` renamed to `QuestionThreadProps`, exported). Header comment:

```tsx
// One open-question thread card (yellow header, collapsible context, reply
// thread, reply form). Extracted from QuestionsTab so the global /questions
// page can render the same threads outside a task screen.
```

Imports adjust to the new location: `'./Markdown'`, `'../lib/format'`, `'../lib/queries'`, `'../lib/types'`, `'./questionthread.css'`.

- [ ] **Step 2: Move the CSS**

Cut every `.question-thread*` rule from `web/src/screens/task/QuestionsTab.css` (the whole `/* ---- thread card ---- */` section, lines ~171-384) into the new `web/src/components/questionthread.css`, unchanged (including the Task-1 fixes). `QuestionsTab.css` keeps the `.questions-tab*` rules — note its `ResolvedThreadRow` detail styles reference `question-thread__*` classes; those now come from `questionthread.css`, which is loaded because `QuestionsTab.tsx` imports `QuestionThread`.

- [ ] **Step 3: Rewire QuestionsTab**

In `web/src/screens/task/QuestionsTab.tsx`: delete the moved code, add

```tsx
import { QuestionThread, authorLabel } from '../../components/QuestionThread'
```

and render `<QuestionThread key={q.id} taskId={taskId} question={q} orchestratorName={orchestratorName} />` where `<ThreadCard …>` was. Remove now-unused imports (`useReplyQuestion`, `useAnswerQuestion` stay only if still referenced — they aren't; `Markdown`/`timeAgo` stay for `ResolvedThreadRow`).

- [ ] **Step 4: Run tests**

Run: `cd web && npm test`
Expected: PASS — `Task.test.tsx` exercises the questions tab and must not notice the refactor

- [ ] **Step 5: Commit**

```bash
git add web/src/components/QuestionThread.tsx web/src/components/questionthread.css web/src/screens/task/QuestionsTab.tsx web/src/screens/task/QuestionsTab.css
git commit -m "refactor(web): extract QuestionThread from QuestionsTab for reuse"
```

---

### Task 7: Web — /questions page + nav badge

**Files:**
- Create: `web/src/screens/questions/QuestionsScreen.tsx`, `web/src/screens/questions/QuestionsScreen.css`
- Modify: `web/src/lib/types.ts`, `web/src/lib/queries.ts`, `web/src/routes.tsx`, `web/src/components/AppShell.tsx`, `web/src/mocks/handlers.ts`
- Test: `web/src/screens/questions/Questions.test.tsx`

**Interfaces:**
- Consumes: `GET /v1/questions` from Task 5; `QuestionThread` from Task 6.
- Produces: `GlobalQuestion` type, `useOpenQuestions(): UseQueryResult<GlobalQuestion[]>` with queryKey `['questions', 'open']`, route `/questions`, nav item `Questions` with awaiting-you counter.

- [ ] **Step 1: Type + hook + invalidation**

In `web/src/lib/types.ts`, after the `Question` interface:

```ts
/**
 * `globalQuestionResponse` — GET /v1/questions entry: a question plus the
 * task/project context the global Questions page needs to label and link it.
 */
export interface GlobalQuestion extends Question {
  task_title: string
  project_id: string
  project_name: string
  orchestrator_name?: string
}
```

In `web/src/lib/queries.ts`:

1. Import `GlobalQuestion` from `./types`.
2. After `useTaskQuestions`:

```ts
/** `GET /v1/questions` — all open questions across all projects, for the
 * global Questions page and the AppShell nav counter. */
export function useOpenQuestions(): UseQueryResult<GlobalQuestion[]> {
  return useQuery({
    queryKey: ['questions', 'open'],
    queryFn: async () => {
      const res = await api.get<{ questions: GlobalQuestion[] }>('/v1/questions')
      return res.questions
    },
  })
}
```

3. In `wireInvalidation`, inside the `event.type.startsWith('task.')` branch, add:

```ts
      queryClient.invalidateQueries({ queryKey: ['questions'] })
```

4. In `useReplyQuestion` and `useAnswerQuestion` `onSuccess`, add the same
   `queryClient.invalidateQueries({ queryKey: ['questions'] })` line.

- [ ] **Step 2: msw handler**

In `web/src/mocks/handlers.ts`, next to the existing `/v1/tasks/:id/questions` handler:

```ts
  // Global open-questions list (internal/api/questions.go
  // handleGetAllQuestions): open only, enriched with task/project context.
  http.get('/v1/questions', () => {
    const open = questionsState.filter((q) => q.status === 'open')
    return HttpResponse.json({
      questions: open.map((q) => {
        const task = tasksState.find((t) => t.id === q.task_id)
        return {
          ...q,
          task_title: task?.title ?? '',
          project_id: task?.project_id ?? 'demo',
          project_name: 'Demo',
          orchestrator_name: 'demo-orch',
        }
      }),
    })
  }),
```

- [ ] **Step 3: Write the failing test**

Create `web/src/screens/questions/Questions.test.tsx` (mirror the render scaffolding of `Kanban.test.tsx`: QueryClientProvider + MemoryRouter with route `/questions`, msw server from the shared test setup):

```tsx
import { screen } from '@testing-library/react'
// …same test harness imports/setup as Kanban.test.tsx…

test('groups open questions and links each to its task', async () => {
  renderQuestions() // helper: render <QuestionsScreen /> at /questions inside providers
  expect(await screen.findByText('Awaiting you')).toBeInTheDocument()
  // Fixture questions carry task_id → the context row links to the task page.
  const link = screen.getAllByRole('link', { name: /#\d+/ })[0]
  expect(link).toHaveAttribute('href', expect.stringMatching(/\/p\/.+\/tasks\/\d+/))
})
```

Adapt the assertions to the actual fixture data (`web/src/mocks/fixtures.ts` `questions` array: at least one open question with `whose_turn: 'user'` must exist — check, and if not, set one fixture question's thread so its `whose_turn` is `'user'`).

Run: `cd web && npm test -- Questions`
Expected: FAIL — screen/module not found

- [ ] **Step 4: Implement the screen**

`web/src/screens/questions/QuestionsScreen.tsx`:

```tsx
// Global Questions page (/questions): every open question across all
// projects, grouped by whose turn it is, answerable in place via the shared
// QuestionThread. Spec: docs/superpowers/specs/2026-07-21-questions-
// visibility-design.md §2.

import { Link } from 'react-router-dom'
import { QuestionThread } from '../../components/QuestionThread'
import { useOpenQuestions } from '../../lib/queries'
import type { GlobalQuestion } from '../../lib/types'
import './QuestionsScreen.css'

function GlobalThread({ q }: { q: GlobalQuestion }) {
  return (
    <div className="questions-screen__item">
      <Link to={`/p/${q.project_id}/tasks/${q.task_id}`} className="questions-screen__task">
        {q.project_name || q.project_id} · #{q.task_id} {q.task_title}
      </Link>
      <QuestionThread taskId={q.task_id} question={q} orchestratorName={q.orchestrator_name} />
    </div>
  )
}

export function QuestionsScreen() {
  const { data: questions, isLoading } = useOpenQuestions()
  const awaitingYou = (questions ?? []).filter((q) => q.whose_turn === 'user')
  const awaitingOrch = (questions ?? []).filter((q) => q.whose_turn !== 'user')

  return (
    <div className="questions-screen">
      <h1 className="questions-screen__title">Open questions</h1>
      {isLoading && <p className="questions-screen__empty">Loading…</p>}
      {!isLoading && (questions ?? []).length === 0 && (
        <p className="questions-screen__empty">No open questions.</p>
      )}
      {awaitingYou.length > 0 && (
        <>
          <div className="questions-screen__label">Awaiting you</div>
          {awaitingYou.map((q) => (
            <GlobalThread key={q.id} q={q} />
          ))}
        </>
      )}
      {awaitingOrch.length > 0 && (
        <>
          <div className="questions-screen__label">Awaiting orchestrator</div>
          {awaitingOrch.map((q) => (
            <GlobalThread key={q.id} q={q} />
          ))}
        </>
      )}
    </div>
  )
}
```

`web/src/screens/questions/QuestionsScreen.css`:

```css
.questions-screen {
  max-width: 860px;
  margin: 0 auto;
  padding: 26px 24px 60px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.questions-screen__title {
  font: 650 20px var(--font-ui);
  color: var(--text);
  letter-spacing: -0.01em;
  margin: 0 0 4px;
}

.questions-screen__label {
  font: 600 11px var(--font-ui);
  color: var(--text-4);
  text-transform: uppercase;
  letter-spacing: 0.05em;
  margin-top: 10px;
}

.questions-screen__empty {
  font: 13.5px var(--font-ui);
  color: var(--text-4);
}

.questions-screen__item {
  display: flex;
  flex-direction: column;
  gap: 7px;
}

.questions-screen__task {
  font: 600 12.5px var(--font-ui);
  color: var(--text-3);
}
.questions-screen__task:hover {
  color: var(--text);
}
```

- [ ] **Step 5: Route + nav badge**

`web/src/routes.tsx` — import `QuestionsScreen` and add inside the `AppShell` children, after the kanban task route:

```tsx
      { path: '/questions', element: <QuestionsScreen /> },
```

`web/src/components/AppShell.tsx` — import `useOpenQuestions` from `../lib/queries`; inside `AppShell()`:

```tsx
  const { data: questions } = useOpenQuestions()
  const awaitingCount = (questions ?? []).filter((q) => q.whose_turn === 'user').length
```

and in the `<nav>`, between the Kanban and System links:

```tsx
          <NavLink to="/questions" style={navLinkStyle}>
            Questions
            {awaitingCount > 0 && (
              <span
                style={{
                  marginLeft: 6,
                  padding: '1px 7px',
                  borderRadius: 999,
                  background: 'var(--warn-bg)',
                  color: 'var(--warn-text-2)',
                  font: '600 11px var(--font-ui)',
                }}
              >
                {awaitingCount}
              </span>
            )}
          </NavLink>
```

- [ ] **Step 6: Run tests**

Run: `cd web && npm test`
Expected: PASS, including `AppShell.test.tsx` (the new fetch is served by the msw handler from Step 2)

- [ ] **Step 7: Commit**

```bash
git add web/src/screens/questions/ web/src/lib/types.ts web/src/lib/queries.ts web/src/routes.tsx web/src/components/AppShell.tsx web/src/mocks/handlers.ts web/src/mocks/fixtures.ts
git commit -m "feat(web): global Questions page with awaiting-you nav counter"
```

---

### Task 8: Attachments backend (config + migration + store + API)

**Files:**
- Modify: `internal/config/config.go`
- Create: `internal/store/migrations/0004_attachments.sql`, `internal/store/attachments.go`, `internal/api/attachments.go`
- Modify: `internal/api/server.go` (route registration)
- Test: `internal/store/attachments_test.go`, `internal/api/attachments_test.go`

**Interfaces:**
- Consumes: `config.Load` defaults/tilde-expansion pattern; migration numbering (next free: 0004); `Deps.Cfg`.
- Produces: `Cfg.AttachmentsDir string`; `store.Attachment{ID, MIME string, Size, CreatedAt int64}`, `Store.AddAttachment(a Attachment) (int64, error)`, `Store.GetAttachment(id int64) (Attachment, error)`; `POST /v1/attachments` (raw image body) → `201 {id, url}`; `GET /v1/attachments/{id}` → file; helper `attachmentFilePath(cfg *config.Config, a store.Attachment) string`.

- [ ] **Step 1: Config field**

In `internal/config/config.go`: add to the `Config` struct after `WorktreesDir`:

```go
	AttachmentsDir       string        `yaml:"attachments_dir"`
```

In `Load`, add to the defaults block: `AttachmentsDir: filepath.Join(home, "attachments"),` and after the WorktreesDir tilde-expansion block:

```go
	// Expand ~ in AttachmentsDir
	if cfg.AttachmentsDir != "" {
		cfg.AttachmentsDir = expandTilde(cfg.AttachmentsDir, home)
	} else {
		cfg.AttachmentsDir = filepath.Join(home, "attachments")
	}
```

- [ ] **Step 2: Migration**

Create `internal/store/migrations/0004_attachments.sql`:

```sql
-- Uploaded dashboard attachments (pasted screenshots). The bytes live on
-- disk under cfg.AttachmentsDir as <id>.<ext>; this table holds identity and
-- metadata so /v1/attachments/{id} can serve with the right Content-Type.
CREATE TABLE attachments (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  mime       TEXT NOT NULL,                  -- image/png|image/jpeg|image/webp
  size       INTEGER NOT NULL,               -- bytes
  created_at INTEGER NOT NULL
);
```

- [ ] **Step 3: Write the failing store test**

Create `internal/store/attachments_test.go`:

```go
package store

import (
	"errors"
	"testing"
)

func TestAttachmentRoundTrip(t *testing.T) {
	s := openTestStore(t)

	id, err := s.AddAttachment(Attachment{MIME: "image/png", Size: 1234})
	if err != nil {
		t.Fatalf("AddAttachment: %v", err)
	}

	got, err := s.GetAttachment(id)
	if err != nil {
		t.Fatalf("GetAttachment: %v", err)
	}
	if got.ID != id || got.MIME != "image/png" || got.Size != 1234 {
		t.Errorf("mismatch: %+v", got)
	}
	if got.CreatedAt == 0 {
		t.Errorf("CreatedAt not defaulted")
	}
}

func TestGetAttachment_NotFound(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.GetAttachment(999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
```

Run: `go test ./internal/store/ -run TestAttachment -v`
Expected: FAIL — compile error

- [ ] **Step 4: Implement the store DAO**

Create `internal/store/attachments.go`:

```go
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Attachment is an uploaded dashboard file (pasted screenshot). Bytes live
// on disk (see internal/api/attachments.go); this row is identity+metadata.
type Attachment struct {
	ID        int64
	MIME      string
	Size      int64
	CreatedAt int64
}

// AddAttachment inserts a new attachment row, defaulting CreatedAt to now.
// Returns the assigned id (which also names the file on disk).
func (s *Store) AddAttachment(a Attachment) (int64, error) {
	if a.CreatedAt == 0 {
		a.CreatedAt = time.Now().Unix()
	}
	res, err := s.db.Exec(
		`INSERT INTO attachments (mime, size, created_at) VALUES (?, ?, ?)`,
		a.MIME, a.Size, a.CreatedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("insert attachment: %w", err)
	}
	return res.LastInsertId()
}

// GetAttachment returns the attachment with the given id, or ErrNotFound.
func (s *Store) GetAttachment(id int64) (Attachment, error) {
	var a Attachment
	err := s.db.QueryRow(
		`SELECT id, mime, size, created_at FROM attachments WHERE id = ?`, id,
	).Scan(&a.ID, &a.MIME, &a.Size, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Attachment{}, ErrNotFound
	}
	if err != nil {
		return Attachment{}, fmt.Errorf("scan attachment: %w", err)
	}
	return a, nil
}
```

Run: `go test ./internal/store/ -run TestAttachment -v`
Expected: PASS

- [ ] **Step 5: Write the failing API test**

Create `internal/api/attachments_test.go`:

```go
package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// attachmentsTestDeps: messagesTestDeps plus an attachments dir under the
// test home.
func attachmentsTestDeps(t *testing.T) Deps {
	t.Helper()
	d := messagesTestDeps(t)
	d.Cfg.AttachmentsDir = filepath.Join(d.Cfg.Home, "attachments")
	return d
}

func postAttachment(t *testing.T, url, contentType string, body []byte) *http.Response {
	t.Helper()
	resp, err := http.Post(url+"/v1/attachments", contentType, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /v1/attachments: %v", err)
	}
	return resp
}

func TestPostAttachment_HappyPath(t *testing.T) {
	d := attachmentsTestDeps(t)
	srv := newTestServer(t, d)

	payload := []byte("fake-png-bytes")
	resp := postAttachment(t, srv.URL, "image/png", payload)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var got struct {
		ID  int64  `json:"id"`
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.URL != "/v1/attachments/"+itoa(got.ID) {
		t.Errorf("url = %q", got.URL)
	}

	// File landed on disk under <id>.png.
	onDisk, err := os.ReadFile(filepath.Join(d.Cfg.AttachmentsDir, itoa(got.ID)+".png"))
	if err != nil {
		t.Fatalf("read stored file: %v", err)
	}
	if !bytes.Equal(onDisk, payload) {
		t.Errorf("stored bytes differ")
	}

	// And GET serves it back with the right Content-Type.
	getResp := getJSON(t, srv.URL+"/v1/attachments/"+itoa(got.ID))
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", getResp.StatusCode)
	}
	if ct := getResp.Header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}
	served, _ := io.ReadAll(getResp.Body)
	if !bytes.Equal(served, payload) {
		t.Errorf("served bytes differ")
	}
}

func TestPostAttachment_UnsupportedMime(t *testing.T) {
	d := attachmentsTestDeps(t)
	srv := newTestServer(t, d)

	resp := postAttachment(t, srv.URL, "application/pdf", []byte("%PDF"))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", resp.StatusCode)
	}
}

func TestPostAttachment_TooLarge(t *testing.T) {
	d := attachmentsTestDeps(t)
	srv := newTestServer(t, d)

	resp := postAttachment(t, srv.URL, "image/png", make([]byte, maxAttachmentBytes+1))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", resp.StatusCode)
	}
}

func TestGetAttachment_NotFound(t *testing.T) {
	d := attachmentsTestDeps(t)
	srv := newTestServer(t, d)

	resp := getJSON(t, srv.URL+"/v1/attachments/999")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}
```

Run: `go test ./internal/api/ -run TestPostAttachment -v`
Expected: FAIL — 404 (routes missing)

- [ ] **Step 6: Implement the API**

Create `internal/api/attachments.go`:

```go
package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/store"
)

// attachmentExts maps the accepted upload MIME types to on-disk extensions.
// Anything else is rejected with 415 — attachments exist for pasted
// screenshots, not general file storage.
var attachmentExts = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
}

// maxAttachmentBytes bounds POST /v1/attachments bodies (10 MiB).
const maxAttachmentBytes = 10 << 20

func registerAttachmentRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("POST /v1/attachments", func(w http.ResponseWriter, r *http.Request) {
		handlePostAttachment(w, r, d)
	})
	mux.HandleFunc("GET /v1/attachments/{id}", func(w http.ResponseWriter, r *http.Request) {
		handleGetAttachment(w, r, d)
	})
}

// attachmentFilePath returns the absolute on-disk path for a: the row id
// plus the extension derived from its MIME type, under cfg.AttachmentsDir.
func attachmentFilePath(cfg *config.Config, a store.Attachment) string {
	return filepath.Join(cfg.AttachmentsDir, fmt.Sprintf("%d%s", a.ID, attachmentExts[a.MIME]))
}

// handlePostAttachment serves POST /v1/attachments: the request body IS the
// file (no multipart), typed by the Content-Type header. On success the
// bytes land in cfg.AttachmentsDir and the response is 201 {id, url}.
func handlePostAttachment(w http.ResponseWriter, r *http.Request, d Deps) {
	mime, _, _ := strings.Cut(r.Header.Get("Content-Type"), ";")
	mime = strings.TrimSpace(mime)
	if _, ok := attachmentExts[mime]; !ok {
		writeErr(w, http.StatusUnsupportedMediaType, "unsupported_media_type",
			"only image/png, image/jpeg and image/webp are accepted")
		return
	}

	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAttachmentBytes))
	if err != nil {
		// The only expected failure here is the MaxBytesReader limit; treat
		// read errors uniformly as too-large rather than leaking internals.
		writeErr(w, http.StatusRequestEntityTooLarge, "too_large", "attachment exceeds 10 MB")
		return
	}
	if len(data) == 0 {
		writeErr(w, http.StatusBadRequest, "empty_body", "attachment body must not be empty")
		return
	}

	id, err := d.Store.AddAttachment(store.Attachment{MIME: mime, Size: int64(len(data))})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if err := os.MkdirAll(d.Cfg.AttachmentsDir, 0700); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	path := attachmentFilePath(d.Cfg, store.Attachment{ID: id, MIME: mime})
	if err := os.WriteFile(path, data, 0600); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":  id,
		"url": fmt.Sprintf("/v1/attachments/%d", id),
	})
}

// handleGetAttachment serves GET /v1/attachments/{id}. Content is immutable
// (an id is never rewritten), hence the aggressive cache header.
func handleGetAttachment(w http.ResponseWriter, r *http.Request, d Deps) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusNotFound, "attachment_not_found", "attachment not found")
		return
	}
	a, err := d.Store.GetAttachment(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "attachment_not_found", "attachment not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	w.Header().Set("Content-Type", a.MIME)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeFile(w, r, attachmentFilePath(d.Cfg, a))
}
```

In `internal/api/server.go` `NewHandler`, after `registerQuestionRoutes(mux, d)`:

```go
	registerAttachmentRoutes(mux, d)
```

- [ ] **Step 7: Run tests**

Run: `go test ./internal/...`
Expected: PASS (config tests too — new default is additive)

- [ ] **Step 8: Commit**

```bash
git add internal/config/config.go internal/store/migrations/0004_attachments.sql internal/store/attachments.go internal/store/attachments_test.go internal/api/attachments.go internal/api/attachments_test.go internal/api/server.go
git commit -m "feat(api): image attachments — upload, store on disk, serve"
```

---

### Task 9: Rewrite attachment links when delivering to agents

**Files:**
- Modify: `internal/api/attachments.go`, `internal/api/questions.go`, `internal/api/messages.go`
- Test: `internal/api/attachments_test.go`

**Interfaces:**
- Consumes: `attachmentFilePath`, `Store.GetAttachment` from Task 8; `deliverToOrchestrator` (questions.go), `handlePostMessage` (messages.go).
- Produces: `rewriteAttachmentLinks(d Deps, body string) string`. Rewrite happens at enqueue time: the `messages` table stores the rewritten body (this is the copy injected into tmux and echoed in the agent transcript); `question_messages`/`task_questions` rows keep the original markdown so the web renders inline images.

- [ ] **Step 1: Write the failing unit test**

Append to `internal/api/attachments_test.go`:

```go
func TestRewriteAttachmentLinks(t *testing.T) {
	d := attachmentsTestDeps(t)
	id, err := d.Store.AddAttachment(store.Attachment{MIME: "image/png", Size: 3})
	if err != nil {
		t.Fatalf("AddAttachment: %v", err)
	}

	in := "see ![screenshot](/v1/attachments/" + itoa(id) + ") and ![gone](/v1/attachments/999) and [a link](https://x.test)"
	got := rewriteAttachmentLinks(d, in)

	wantPath := filepath.Join(d.Cfg.AttachmentsDir, itoa(id)+".png")
	if !strings.Contains(got, "[screenshot: "+wantPath+"]") {
		t.Errorf("known link not rewritten: %q", got)
	}
	if !strings.Contains(got, "![gone](/v1/attachments/999)") {
		t.Errorf("unknown id must stay untouched: %q", got)
	}
	if !strings.Contains(got, "[a link](https://x.test)") {
		t.Errorf("non-attachment link must stay untouched: %q", got)
	}
}
```

(add `"strings"` and `"github.com/IvanRoslov/rocket/internal/store"` to the file's imports)

Run: `go test ./internal/api/ -run TestRewriteAttachmentLinks -v`
Expected: FAIL — compile error, function undefined

- [ ] **Step 2: Implement**

Append to `internal/api/attachments.go` (add `"regexp"` import):

```go
// attachmentLinkRe matches markdown image links pointing at the attachments
// API, as inserted by the dashboard's paste handler.
var attachmentLinkRe = regexp.MustCompile(`!\[[^\]]*\]\(/v1/attachments/(\d+)\)`)

// rewriteAttachmentLinks replaces dashboard attachment links in body with
// bracketed absolute file paths so the receiving agent can open the image
// from disk (agents get text injected into a TUI — a URL is useless there,
// a path is Read-able). Called at message-enqueue time only; question
// threads keep the original markdown for the web to render. Unknown ids
// pass through untouched.
func rewriteAttachmentLinks(d Deps, body string) string {
	return attachmentLinkRe.ReplaceAllStringFunc(body, func(link string) string {
		idStr := attachmentLinkRe.FindStringSubmatch(link)[1]
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			return link
		}
		a, err := d.Store.GetAttachment(id)
		if err != nil {
			return link
		}
		return "[screenshot: " + attachmentFilePath(d.Cfg, a) + "]"
	})
}
```

- [ ] **Step 3: Apply at both enqueue points**

In `internal/api/questions.go` `deliverToOrchestrator`, as the first line of the function body:

```go
	body = rewriteAttachmentLinks(d, body)
```

In `internal/api/messages.go` `handlePostMessage`, directly before the `d.Store.AddMessage(...)` call:

```go
	// Attachment links pasted in the dashboard are rewritten to on-disk
	// paths here, at enqueue time, so the injected copy (and the transcript
	// echo) is what the agent can actually open.
	req.Body = rewriteAttachmentLinks(d, req.Body)
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/api/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/attachments.go internal/api/attachments_test.go internal/api/questions.go internal/api/messages.go
git commit -m "feat(api): rewrite attachment links to file paths on delivery to agents"
```

---

### Task 10: Web — Ctrl+V paste-to-upload + inline images

**Files:**
- Modify: `web/src/lib/api.ts`, `web/src/components/Markdown.tsx`, `web/src/components/markdown.css`, `web/src/components/QuestionThread.tsx`, `web/src/screens/task/QuestionsTab.tsx` (AskOrchestratorForm), `web/src/screens/chat/ChatScreen.tsx`, `web/src/mocks/handlers.ts`
- Create: `web/src/lib/usePasteImage.ts`
- Test: `web/src/lib/usePasteImage.test.tsx`

**Interfaces:**
- Consumes: `POST /v1/attachments` from Task 8; `Dispatch<SetStateAction<string>>` setters already present in all three forms.
- Produces: `api.upload(file: Blob): Promise<{ id: number; url: string }>`; `usePasteImage(setBody): { onPaste: (e: ClipboardEvent<HTMLTextAreaElement>) => void; error?: string }`.

- [ ] **Step 1: api.upload**

In `web/src/lib/api.ts`, add below `req`:

```ts
/** Raw-body upload to POST /v1/attachments (the body IS the file; no JSON,
 * no multipart). Same error-envelope handling as `req`. */
async function upload(file: Blob): Promise<{ id: number; url: string }> {
  const res = await fetch('/v1/attachments', {
    method: 'POST',
    headers: { 'Content-Type': file.type },
    body: file,
  })
  if (!res.ok) {
    const payload = (await res.json().catch(() => null)) as ErrorEnvelope | null
    throw new ApiError(
      res.status,
      payload?.error?.code ?? 'unknown',
      payload?.error?.message ?? res.statusText,
    )
  }
  return res.json()
}
```

and register it: `export const api = { …existing…, upload }`.

- [ ] **Step 2: msw handler**

In `web/src/mocks/handlers.ts`:

```ts
  // Attachment upload (internal/api/attachments.go): raw image body -> id+url.
  http.post('/v1/attachments', () =>
    HttpResponse.json({ id: 1, url: '/v1/attachments/1' }, { status: 201 }),
  ),
```

- [ ] **Step 3: Write the failing hook test**

Create `web/src/lib/usePasteImage.test.tsx` (msw server comes from the shared vitest setup, same as other lib tests):

```tsx
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useState } from 'react'
import { expect, test } from 'vitest'
import { usePasteImage } from './usePasteImage'

function Harness() {
  const [body, setBody] = useState('before after')
  const paste = usePasteImage(setBody)
  return (
    <>
      <textarea aria-label="field" value={body} onChange={(e) => setBody(e.target.value)} onPaste={paste.onPaste} />
      {paste.error && <div role="alert">{paste.error}</div>}
    </>
  )
}

function pasteImage(el: HTMLElement) {
  const file = new File([new Uint8Array([1, 2, 3])], 'shot.png', { type: 'image/png' })
  fireEvent.paste(el, {
    clipboardData: {
      items: [{ type: 'image/png', getAsFile: () => file }],
    },
  })
}

test('pasting an image uploads it and inserts markdown at the cursor', async () => {
  render(<Harness />)
  const field = screen.getByLabelText<HTMLTextAreaElement>('field')
  field.setSelectionRange(7, 7) // between "before " and "after"
  pasteImage(field)
  await waitFor(() =>
    expect(field.value).toBe('before ![screenshot](/v1/attachments/1)after'),
  )
})

test('non-image paste is ignored', () => {
  render(<Harness />)
  const field = screen.getByLabelText<HTMLTextAreaElement>('field')
  fireEvent.paste(field, { clipboardData: { items: [] } })
  expect(field.value).toBe('before after')
})
```

Run: `cd web && npm test -- usePasteImage`
Expected: FAIL — module not found

- [ ] **Step 4: Implement the hook**

Create `web/src/lib/usePasteImage.ts`:

```ts
// Paste-to-upload for textareas: on image paste, uploads the clipboard
// image to POST /v1/attachments and inserts `![screenshot](url)` markdown
// at the cursor. While the upload runs a placeholder sits in the text; on
// failure the placeholder is removed and `error` set (cleared on the next
// paste). Text pastes pass through untouched.

import { useCallback, useState, type ClipboardEvent, type Dispatch, type SetStateAction } from 'react'
import { api } from './api'

const PLACEHOLDER = '![uploading…]()'

export function usePasteImage(setBody: Dispatch<SetStateAction<string>>): {
  onPaste: (e: ClipboardEvent<HTMLTextAreaElement>) => void
  error?: string
} {
  const [error, setError] = useState<string>()

  const onPaste = useCallback(
    (e: ClipboardEvent<HTMLTextAreaElement>) => {
      const item = Array.from(e.clipboardData.items).find((i) => i.type.startsWith('image/'))
      if (!item) return
      const file = item.getAsFile()
      if (!file) return
      e.preventDefault()

      const start = e.currentTarget.selectionStart ?? e.currentTarget.value.length
      const end = e.currentTarget.selectionEnd ?? start
      setError(undefined)
      setBody((prev) => prev.slice(0, start) + PLACEHOLDER + prev.slice(end))

      api.upload(file).then(
        ({ url }) => setBody((prev) => prev.replace(PLACEHOLDER, `![screenshot](${url})`)),
        (err: Error) => {
          setBody((prev) => prev.replace(PLACEHOLDER, ''))
          setError(err.message)
        },
      )
    },
    [setBody],
  )

  return { onPaste, error }
}
```

Run: `cd web && npm test -- usePasteImage`
Expected: PASS

- [ ] **Step 5: Render images in Markdown**

In `web/src/components/Markdown.tsx`, add to the `components` map:

```tsx
          img: ({ src, alt }) => (
            <a href={src} target="_blank" rel="noopener noreferrer">
              <img src={src} alt={alt ?? ''} className="markdown__img" />
            </a>
          ),
```

In `web/src/components/markdown.css`:

```css
.markdown__img {
  display: block;
  max-width: 100%;
  max-height: 360px;
  border-radius: 8px;
  border: 1px solid var(--border);
  margin: 2px 0 8px;
}
```

- [ ] **Step 6: Wire the hook into the three surfaces**

1. `web/src/components/QuestionThread.tsx` — in `QuestionThread`:

```tsx
  const paste = usePasteImage(setBody)
```

on the reply `<textarea …>` add `onPaste={paste.onPaste}`, and directly after it:

```tsx
          {paste.error && <div className="question-thread__paste-error">Upload failed: {paste.error}</div>}
```

with CSS in `questionthread.css`:

```css
.question-thread__paste-error {
  font: 12px var(--font-ui);
  color: var(--err-text, #c0392b);
  margin-bottom: 8px;
}
```

2. `web/src/screens/task/QuestionsTab.tsx` — in `AskOrchestratorForm`:

```tsx
  const pasteBody = usePasteImage(setBody)
  const pasteContext = usePasteImage(setContext)
```

add `onPaste={pasteBody.onPaste}` to the body textarea, `onPaste={pasteContext.onPaste}` to the context textarea, and after the actions row:

```tsx
      {(pasteBody.error ?? pasteContext.error) && (
        <div className="question-thread__paste-error">Upload failed: {pasteBody.error ?? pasteContext.error}</div>
      )}
```

3. `web/src/screens/chat/ChatScreen.tsx` — in the component owning the composer `setBody`:

```tsx
  const paste = usePasteImage(setBody)
```

add `onPaste={paste.onPaste}` to the composer `<textarea>`; show the error inside the composer block:

```tsx
            {paste.error && <div className="chat-screen__paste-error">Upload failed: {paste.error}</div>}
```

with CSS in the chat stylesheet (`chat.css` next to the other `chat-screen__*` rules):

```css
.chat-screen__paste-error {
  font: 12px var(--font-ui);
  color: var(--err-text, #c0392b);
  padding: 2px 4px;
}
```

- [ ] **Step 7: Run all web tests + typecheck**

Run: `cd web && npm test && npm run build`
Expected: PASS / clean build

- [ ] **Step 8: Commit**

```bash
git add web/src/lib/api.ts web/src/lib/usePasteImage.ts web/src/lib/usePasteImage.test.tsx web/src/components/ web/src/screens/task/QuestionsTab.tsx web/src/screens/chat/ web/src/mocks/handlers.ts
git commit -m "feat(web): paste screenshots via Ctrl+V in questions and chat"
```

---

## Final verification

- [ ] `go test ./...` — all green
- [ ] `cd web && npm test && npm run build` — all green
- [ ] Manual smoke (optional but recommended): run the daemon + web dev server, open a task with an open question — context renders as proportional markdown; paste a screenshot into a reply — image uploads, renders inline, and the injected message (visible via `rocket attach`/chat) carries `[screenshot: /path/to/file.png]`.
