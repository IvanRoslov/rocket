# Persistent Agents — Role Q&A Threads Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give agent roles the same Q&A threads tasks already have — both directions (human ↔ role), reachable over REST and CLI, with open-question counts in the agents list.

**Architecture:** New tables `agent_questions` / `agent_question_messages` mirror
`task_questions` / `question_messages` one-for-one, only keyed by `role_id TEXT`
instead of `task_id INTEGER`. The store, API and CLI layers each get an
agent-flavoured twin of the existing questions file. Direction is derived from
the caller exactly as for tasks: a request with no `X-Rocket-Session` header is
the human (`asked_by`/`author` = `""`), a request from a role instance session
(`<role>-run-<n>`) is the role. Human-authored entries additionally enqueue a
`question` inbox event so the runtime layer (#642) wakes the role; role-authored
entries are thread-only and the human sees them in the CLI/dashboard.

**Tech Stack:** Go 1.x single module, SQLite (modernc), `net/http` ServeMux
routing, cobra CLI, `go test ./...`.

## Global Constraints

- Subtask #644 of feature `task-639`; branch `feature/task-639/qa-threads`; one PR.
- Follow existing package patterns: `internal/store` per-entity file + embedded
  SQL migration, `internal/api` handler+test per resource, cobra CLI in
  `internal/cli`. English identifiers and commit messages; Russian user-facing
  CLI strings (matching `internal/cli/agent.go`).
- Role ids match `^[a-z0-9-]+$`; role instance sessions are named `<role>-run-<n>`
  and are the only link between a run and its role.
- The daemon never writes to GitHub; nothing in this task talks to GitHub.
- No changes to task questions behaviour — the existing `internal/store/questions.go`,
  `internal/api/questions.go` and their tests must keep passing untouched.

## Contract decisions (locked)

- Routes: `GET /v1/agents/{id}/questions?status=open`, `POST /v1/agents/{id}/questions`,
  `POST /v1/agent-questions/{qid}/reply`, `POST /v1/agent-questions/{qid}/answer`.
  Replies/answers are flat (not nested under the role) so `rocket agent reply <qid>`
  needs only the question id — an exact mirror of `/v1/questions/{id}/reply`.
- `whose_turn` values are `user` | `role` (task threads use `user` | `orchestrator`).
- Authorization: the human, or an instance of that very role. Any other session
  (orchestrator/worker/another role) gets 403 — cross-agent traffic goes through
  `rocket send`.
- EVERY human entry (the opening question, later replies, and the final answer)
  enqueues one `question` inbox event with payload
  `{"question_id":N,"role_id":"sre","ordinal":K,"entry":"question|reply|answer","text":"..."}`
  — the role must wake for follow-ups, not only for the opening message.
  Spawning/waking is the runtime layer's job; this task only enqueues.
- In addition, if a live instance of the role exists (a non-terminal session of
  kind `agent` named `<role>-run-<n>`), the human entry is also delivered into
  that session's message queue, prefixed like task threads:
  `[role sre Q2 question] ...` / `[role sre Q2 reply] ...` / `[role sre Q2 answer] ...`.
  Role-authored entries are thread-only — the human reads them via API/CLI.
- A resolved thread can be reopened by a role reply, exactly as an orchestrator
  can reopen a resolved task question.

## File Structure

- Create `internal/store/migrations/0006_agent_questions.sql` — the two tables + index.
- Create `internal/store/agent_questions.go` — `AgentQuestion`, `AgentQuestionMessage`
  and their CRUD/count helpers. Mirrors `questions.go` + `questions_reopen.go`
  (kept in one file: it is a single entity's storage, ~250 lines).
- Create `internal/store/agent_questions_test.go`.
- Create `internal/api/agent_questions.go` — response shapes, `whoseTurn`, route
  registration for the four endpoints, inbox-event delivery helper.
- Create `internal/api/agent_questions_test.go`.
- Modify `internal/api/agents.go` — register the new routes from
  `registerAgentRoutes`, add `open_questions` / `awaiting_user` to `agentResponse`.
- Modify `internal/api/agents_test.go` — assert the new list fields.
- Create `internal/cli/agent_questions.go` — `ask`, `reply`, `answer`, `questions`
  subcommands + rendering (keeps `agent.go` from growing past ~700 lines).
- Create `internal/cli/agent_questions_test.go`.
- Modify `internal/cli/agent.go:19-27` — wire the four subcommands.
- Modify `docs/10-agents.md` — Q&A section.
- Modify `docs/03-daemon-api.md`, `docs/04-cli.md` — endpoint and command reference.

---

### Task 1: Store layer — tables and CRUD

**Files:**
- Create: `internal/store/migrations/0006_agent_questions.sql`
- Create: `internal/store/agent_questions.go`
- Test: `internal/store/agent_questions_test.go`

**Interfaces:**
- Consumes: `store.Store`, `store.ErrNotFound`, `store.ErrQuestionResolved`,
  `store.ErrQuestionOpen`, `nullIfEmpty`, `nullIfZero` (all existing).
- Produces:
  ```go
  type AgentQuestion struct {
      ID         int64
      RoleID     string
      AskedBy    string // session id of the asking role instance; "" = human
      Body       string
      Context    string
      Status     string // open|resolved
      Resolution string // answered|dismissed
      AskedAt    int64
      ResolvedAt int64
  }
  type AgentQuestionMessage struct {
      ID         int64
      QuestionID int64
      Author     string // role instance session id; "" = human
      Kind       string // reply|answer
      Body       string
      CreatedAt  int64
  }
  func (s *Store) AddAgentQuestion(q AgentQuestion) (int64, error)
  func (s *Store) GetAgentQuestion(id int64) (AgentQuestion, error)
  func (s *Store) ListAgentQuestions(roleID string, openOnly bool) ([]AgentQuestion, error)
  func (s *Store) ResolveAgentQuestion(id int64, resolution string) error
  func (s *Store) ReopenAgentQuestion(id int64) error
  func (s *Store) AddAgentQuestionMessage(m AgentQuestionMessage) (int64, error)
  func (s *Store) ListAgentQuestionMessages(questionID int64) ([]AgentQuestionMessage, error)
  func (s *Store) AgentQuestionOrdinal(q AgentQuestion) (int, error)
  func (s *Store) OpenAgentQuestionCounts() (map[string]QuestionCounts, error)
  func (s *Store) DeleteAgent(id string) error // extended to purge threads
  ```

- [ ] **Step 1: Write the failing test**

`internal/store/agent_questions_test.go`:

```go
package store

import "testing"

func seedAgentForQuestions(t *testing.T, s *Store, id string) {
	t.Helper()
	if err := s.AddProject(Project{ID: "p1", Name: "P1"}); err != nil && err != ErrExists {
		t.Fatalf("add project: %v", err)
	}
	if err := s.AddAgent(Agent{ID: id, ProjectID: "p1", PromptPath: "/tmp/role.md", Enabled: true}); err != nil {
		t.Fatalf("add agent: %v", err)
	}
}

func TestAgentQuestionThreadLifecycle(t *testing.T) {
	s := newTestStore(t)
	seedAgentForQuestions(t, s, "sre")

	id, err := s.AddAgentQuestion(AgentQuestion{RoleID: "sre", Body: "как быть?"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	q, err := s.GetAgentQuestion(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if q.Status != "open" || q.RoleID != "sre" || q.AskedBy != "" || q.AskedAt == 0 {
		t.Fatalf("unexpected question: %+v", q)
	}

	if _, err := s.AddAgentQuestionMessage(AgentQuestionMessage{QuestionID: id, Author: "sre-run-1", Body: "смотрю"}); err != nil {
		t.Fatalf("add message: %v", err)
	}
	msgs, err := s.ListAgentQuestionMessages(id)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Kind != "reply" || msgs[0].Author != "sre-run-1" {
		t.Fatalf("unexpected messages: %+v", msgs)
	}

	if err := s.ResolveAgentQuestion(id, "answered"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if err := s.ResolveAgentQuestion(id, "answered"); err != ErrQuestionResolved {
		t.Fatalf("double resolve = %v, want ErrQuestionResolved", err)
	}
	if err := s.ReopenAgentQuestion(id); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	q, _ = s.GetAgentQuestion(id)
	if q.Status != "open" || q.Resolution != "" || q.ResolvedAt != 0 {
		t.Fatalf("after reopen: %+v", q)
	}

	if _, err := s.GetAgentQuestion(9999); err != ErrNotFound {
		t.Fatalf("missing question = %v, want ErrNotFound", err)
	}
}

func TestListAgentQuestionsAndOrdinal(t *testing.T) {
	s := newTestStore(t)
	seedAgentForQuestions(t, s, "sre")
	seedAgentForQuestions(t, s, "triage")

	q1, _ := s.AddAgentQuestion(AgentQuestion{RoleID: "sre", Body: "q1"})
	q2, _ := s.AddAgentQuestion(AgentQuestion{RoleID: "sre", Body: "q2"})
	if _, err := s.AddAgentQuestion(AgentQuestion{RoleID: "triage", Body: "other"}); err != nil {
		t.Fatalf("add other: %v", err)
	}
	if err := s.ResolveAgentQuestion(q1, "dismissed"); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	all, err := s.ListAgentQuestions("sre", false)
	if err != nil || len(all) != 2 {
		t.Fatalf("list all = %+v, %v", all, err)
	}
	open, err := s.ListAgentQuestions("sre", true)
	if err != nil || len(open) != 1 || open[0].ID != q2 {
		t.Fatalf("list open = %+v, %v", open, err)
	}

	second, _ := s.GetAgentQuestion(q2)
	n, err := s.AgentQuestionOrdinal(second)
	if err != nil || n != 2 {
		t.Fatalf("ordinal = %d, %v; want 2", n, err)
	}
}

func TestOpenAgentQuestionCounts(t *testing.T) {
	s := newTestStore(t)
	seedAgentForQuestions(t, s, "sre")

	// Role-opened, unanswered: awaits the human.
	if _, err := s.AddAgentQuestion(AgentQuestion{RoleID: "sre", AskedBy: "sre-run-1", Body: "нужно решение"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	// Human-opened, unanswered: awaits the role.
	humanQ, _ := s.AddAgentQuestion(AgentQuestion{RoleID: "sre", Body: "как дела?"})
	// Human-opened but last word is the role's: awaits the human again.
	replied, _ := s.AddAgentQuestion(AgentQuestion{RoleID: "sre", Body: "и ещё"})
	if _, err := s.AddAgentQuestionMessage(AgentQuestionMessage{QuestionID: replied, Author: "sre-run-1", Body: "ответ"}); err != nil {
		t.Fatalf("add message: %v", err)
	}
	// Resolved threads never count.
	done, _ := s.AddAgentQuestion(AgentQuestion{RoleID: "sre", Body: "старое"})
	if err := s.ResolveAgentQuestion(done, "answered"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	_ = humanQ

	counts, err := s.OpenAgentQuestionCounts()
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	got := counts["sre"]
	if got.Open != 3 || got.AwaitingUser != 2 {
		t.Fatalf("counts = %+v; want {Open:3 AwaitingUser:2}", got)
	}
}

func TestDeleteAgentPurgesQuestions(t *testing.T) {
	s := newTestStore(t)
	seedAgentForQuestions(t, s, "sre")

	qid, _ := s.AddAgentQuestion(AgentQuestion{RoleID: "sre", Body: "q"})
	if _, err := s.AddAgentQuestionMessage(AgentQuestionMessage{QuestionID: qid, Body: "m"}); err != nil {
		t.Fatalf("add message: %v", err)
	}
	if err := s.DeleteAgent("sre"); err != nil {
		t.Fatalf("delete agent: %v", err)
	}
	if _, err := s.GetAgentQuestion(qid); err != ErrNotFound {
		t.Fatalf("question survived delete: %v", err)
	}
	msgs, err := s.ListAgentQuestionMessages(qid)
	if err != nil || len(msgs) != 0 {
		t.Fatalf("messages survived delete: %+v, %v", msgs, err)
	}
}
```

Check `newTestStore` is the helper name used by `internal/store/store_test.go`; if
the existing helper differs, use that one verbatim instead.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run 'AgentQuestion|DeleteAgentPurges' -v`
Expected: compile failure — `AddAgentQuestion` undefined.

- [ ] **Step 3: Write the migration**

`internal/store/migrations/0006_agent_questions.sql`:

```sql
-- Q&A-треды ролей (задача #639): полный аналог task_questions/question_messages,
-- только адресат — роль, а не задача. Направление треда выводится из авторства:
-- пустой author/asked_by = человек, иначе session id инстанса роли.
CREATE TABLE agent_questions (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  role_id     TEXT NOT NULL REFERENCES agents(id),
  asked_by    TEXT NOT NULL DEFAULT '',      -- session id инстанса роли; '' = человек
  body        TEXT NOT NULL,
  context     TEXT,                          -- опциональный markdown-контекст
  status      TEXT NOT NULL DEFAULT 'open',  -- open|resolved
  resolution  TEXT,                          -- answered|dismissed (когда resolved)
  asked_at    INTEGER NOT NULL,
  resolved_at INTEGER
);

CREATE TABLE agent_question_messages (       -- тред вопроса роли
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  question_id INTEGER NOT NULL REFERENCES agent_questions(id),
  author      TEXT,                          -- session id инстанса роли; NULL = человек
  kind        TEXT NOT NULL DEFAULT 'reply', -- reply|answer
  body        TEXT NOT NULL,
  created_at  INTEGER NOT NULL
);

CREATE INDEX idx_agent_questions ON agent_questions(role_id, status);
CREATE INDEX idx_agent_question_messages ON agent_question_messages(question_id, id);
```

- [ ] **Step 4: Write the store implementation**

`internal/store/agent_questions.go` — mirror `questions.go`: `scanAgentQuestion`
helper, `AddAgentQuestion` (defaults `AskedAt=now`, `Status="open"`),
`GetAgentQuestion`, `ListAgentQuestions(roleID, openOnly)` ordered by id,
`ResolveAgentQuestion` (update `... WHERE id=? AND status='open'`, distinguish
`ErrNotFound` vs `ErrQuestionResolved` exactly as `ResolveQuestion` does),
`ReopenAgentQuestion` (mirror `questions_reopen.go`, return `ErrQuestionOpen`),
`AddAgentQuestionMessage` (defaults `CreatedAt=now`, `Kind="reply"`),
`ListAgentQuestionMessages`, `AgentQuestionOrdinal`
(`COUNT(*) WHERE role_id=? AND id<=?`), and:

```go
// OpenAgentQuestionCounts returns, per role with at least one open question,
// how many are open and how many await the human. Mirrors OpenQuestionCounts:
// with no thread messages the question itself is the last entry (a
// role-opened question awaits the human, a human-opened one doesn't);
// otherwise the last message's author decides.
func (s *Store) OpenAgentQuestionCounts() (map[string]QuestionCounts, error) {
	rows, err := s.db.Query(`
		SELECT role_id, COUNT(*), SUM(turn_user) FROM (
			SELECT q.role_id AS role_id,
				CASE
					WHEN m.id IS NULL THEN (CASE WHEN q.asked_by != '' THEN 1 ELSE 0 END)
					WHEN m.author IS NOT NULL AND m.author != '' THEN 1
					ELSE 0
				END AS turn_user
			FROM agent_questions q
			LEFT JOIN agent_question_messages m
				ON m.question_id = q.id
				AND m.id = (SELECT MAX(id) FROM agent_question_messages WHERE question_id = q.id)
			WHERE q.status = 'open'
		) GROUP BY role_id`)
	if err != nil {
		return nil, fmt.Errorf("query open agent question counts: %w", err)
	}
	defer rows.Close()

	out := make(map[string]QuestionCounts)
	for rows.Next() {
		var roleID string
		var c QuestionCounts
		if err := rows.Scan(&roleID, &c.Open, &c.AwaitingUser); err != nil {
			return nil, fmt.Errorf("scan open agent question counts: %w", err)
		}
		out[roleID] = c
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
```

Then extend `DeleteAgent` in `internal/store/agents.go` (inside its existing
transaction, before deleting the role row):

```go
	if _, err := tx.Exec(
		`DELETE FROM agent_question_messages WHERE question_id IN
		 (SELECT id FROM agent_questions WHERE role_id = ?)`, id); err != nil {
		return fmt.Errorf("delete agent question messages: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM agent_questions WHERE role_id = ?`, id); err != nil {
		return fmt.Errorf("delete agent questions: %w", err)
	}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/store/`
Expected: PASS (all pre-existing store tests included).

- [ ] **Step 6: Commit**

```bash
git add internal/store/agent_questions.go internal/store/agent_questions_test.go \
        internal/store/migrations/0006_agent_questions.sql internal/store/agents.go
git commit -m "store: agent_questions/agent_question_messages for role Q&A threads"
```

---

### Task 2: REST endpoints

**Files:**
- Create: `internal/api/agent_questions.go`
- Modify: `internal/api/agents.go` (route registration in `registerAgentRoutes`)
- Test: `internal/api/agent_questions_test.go`

**Interfaces:**
- Consumes: Task 1's store methods; existing `Deps`, `writeJSON`, `writeErr`,
  `callerSession`, `writeCallerErr`, `callerAuthor`, `callerLabel`,
  `roleFromSessionID` (already in `internal/api/agents.go`), `lookupAgent`.
- Produces:
  ```go
  type agentQuestionResponse struct {
      ID         int64                     `json:"id"`
      RoleID     string                    `json:"role_id"`
      Ordinal    int                       `json:"ordinal"`
      AskedBy    string                    `json:"asked_by"`
      Body       string                    `json:"body"`
      Context    string                    `json:"context,omitempty"`
      Status     string                    `json:"status"`
      Resolution string                    `json:"resolution,omitempty"`
      WhoseTurn  string                    `json:"whose_turn,omitempty"` // user|role
      AskedAt    int64                     `json:"asked_at"`
      ResolvedAt int64                     `json:"resolved_at,omitempty"`
      Messages   []questionMessageResponse `json:"messages"`
  }
  func registerAgentQuestionRoutes(mux *http.ServeMux, d Deps)
  ```

- [ ] **Step 1: Write the failing test**

`internal/api/agent_questions_test.go`. Use the existing test harness in
`internal/api/agents_test.go` (same package) — reuse its server/setup helper and
its way of seeding a role and a session; the snippets below assume a helper
`newTestServer(t)` returning `(*httptest.Server, Deps)` and `do(t, srv, method,
path, body, sessionID)` returning `(*http.Response, []byte)`; if the existing
names differ, use those verbatim.

```go
func TestPostAgentQuestionFromHumanEnqueuesInboxEvent(t *testing.T) {
	srv, d := newTestServer(t)
	seedAgent(t, d, "sre")

	resp, body := do(t, srv, "POST", "/v1/agents/sre/questions",
		map[string]any{"body": "почему упал деплой?"}, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	var q agentQuestionResponse
	mustUnmarshal(t, body, &q)
	if q.RoleID != "sre" || q.AskedBy != "" || q.Ordinal != 1 || q.WhoseTurn != "role" {
		t.Fatalf("unexpected question: %+v", q)
	}

	events, err := d.Store.QueuedInboxEvents("sre")
	if err != nil {
		t.Fatalf("inbox: %v", err)
	}
	if len(events) != 1 || events[0].Kind != "question" {
		t.Fatalf("inbox events = %+v", events)
	}
	if !strings.Contains(events[0].Payload, "\"entry\":\"question\"") ||
		!strings.Contains(events[0].Payload, "почему упал деплой?") {
		t.Fatalf("payload = %s", events[0].Payload)
	}
}

func TestPostAgentQuestionFromRoleInstanceAwaitsUser(t *testing.T) {
	srv, d := newTestServer(t)
	seedAgent(t, d, "sre")
	seedAgentSession(t, d, "sre-run-1")

	resp, body := do(t, srv, "POST", "/v1/agents/sre/questions",
		map[string]any{"body": "нужно ваше решение", "context": "детали"}, "sre-run-1")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	var q agentQuestionResponse
	mustUnmarshal(t, body, &q)
	if q.AskedBy != "sre-run-1" || q.WhoseTurn != "user" || q.Context != "детали" {
		t.Fatalf("unexpected question: %+v", q)
	}

	events, _ := d.Store.QueuedInboxEvents("sre")
	if len(events) != 0 {
		t.Fatalf("role-opened question must not wake the role: %+v", events)
	}
}

func TestPostAgentQuestionForeignSessionForbidden(t *testing.T) {
	srv, d := newTestServer(t)
	seedAgent(t, d, "sre")
	seedAgentSession(t, d, "triage-run-1")

	resp, _ := do(t, srv, "POST", "/v1/agents/sre/questions",
		map[string]any{"body": "чужой"}, "triage-run-1")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestGetAgentQuestionsFiltersOpen(t *testing.T) {
	srv, d := newTestServer(t)
	seedAgent(t, d, "sre")

	first, _ := d.Store.AddAgentQuestion(store.AgentQuestion{RoleID: "sre", Body: "q1"})
	if _, err := d.Store.AddAgentQuestion(store.AgentQuestion{RoleID: "sre", Body: "q2"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := d.Store.ResolveAgentQuestion(first, "answered"); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	_, body := do(t, srv, "GET", "/v1/agents/sre/questions?status=open", nil, "")
	var out struct {
		Questions []agentQuestionResponse `json:"questions"`
	}
	mustUnmarshal(t, body, &out)
	if len(out.Questions) != 1 || out.Questions[0].Body != "q2" || out.Questions[0].Ordinal != 2 {
		t.Fatalf("questions = %+v", out.Questions)
	}
}

func TestAgentQuestionReplyAndAnswer(t *testing.T) {
	srv, d := newTestServer(t)
	seedAgent(t, d, "sre")
	seedAgentSession(t, d, "sre-run-1")

	_, body := do(t, srv, "POST", "/v1/agents/sre/questions", map[string]any{"body": "вопрос"}, "")
	var q agentQuestionResponse
	mustUnmarshal(t, body, &q)
	qid := strconv.FormatInt(q.ID, 10)

	// The role replies in-thread: no new inbox event, turn flips to the human.
	resp, body := do(t, srv, "POST", "/v1/agent-questions/"+qid+"/reply",
		map[string]any{"body": "разбираюсь"}, "sre-run-1")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("reply status = %d, body = %s", resp.StatusCode, body)
	}
	mustUnmarshal(t, body, &q)
	if len(q.Messages) != 1 || q.WhoseTurn != "user" {
		t.Fatalf("after role reply: %+v", q)
	}
	if events, _ := d.Store.QueuedInboxEvents("sre"); len(events) != 1 {
		t.Fatalf("role reply must not enqueue: %+v", events)
	}

	// The human replies: a second inbox event lands.
	if resp, body := do(t, srv, "POST", "/v1/agent-questions/"+qid+"/reply",
		map[string]any{"body": "жду"}, ""); resp.StatusCode != http.StatusCreated {
		t.Fatalf("human reply status = %d, body = %s", resp.StatusCode, body)
	}
	if events, _ := d.Store.QueuedInboxEvents("sre"); len(events) != 2 {
		t.Fatalf("human reply must enqueue: %+v", events)
	}

	// The human closes the thread.
	resp, body = do(t, srv, "POST", "/v1/agent-questions/"+qid+"/answer",
		map[string]any{"body": "делай вариант Б"}, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("answer status = %d, body = %s", resp.StatusCode, body)
	}
	mustUnmarshal(t, body, &q)
	if q.Status != "resolved" || q.Resolution != "answered" || q.WhoseTurn != "" {
		t.Fatalf("after answer: %+v", q)
	}

	// A second human answer is a conflict.
	if resp, _ := do(t, srv, "POST", "/v1/agent-questions/"+qid+"/answer",
		map[string]any{"dismiss": true}, ""); resp.StatusCode != http.StatusConflict {
		t.Fatalf("double answer status = %d, want 409", resp.StatusCode)
	}

	// The role may dispute a resolved thread: its reply reopens it.
	resp, body = do(t, srv, "POST", "/v1/agent-questions/"+qid+"/reply",
		map[string]any{"body": "вариант Б не сработает"}, "sre-run-1")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("reopen status = %d, body = %s", resp.StatusCode, body)
	}
	mustUnmarshal(t, body, &q)
	if q.Status != "open" || q.Resolution != "" {
		t.Fatalf("after reopen: %+v", q)
	}
}

func TestAgentQuestionAnswerRejectsAgents(t *testing.T) {
	srv, d := newTestServer(t)
	seedAgent(t, d, "sre")
	seedAgentSession(t, d, "sre-run-1")

	qid, _ := d.Store.AddAgentQuestion(store.AgentQuestion{RoleID: "sre", Body: "q"})
	resp, _ := do(t, srv, "POST", "/v1/agent-questions/"+strconv.FormatInt(qid, 10)+"/answer",
		map[string]any{"body": "сам себе"}, "sre-run-1")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestAgentQuestionNotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	if resp, _ := do(t, srv, "POST", "/v1/agent-questions/999/reply",
		map[string]any{"body": "x"}, ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run AgentQuestion -v`
Expected: compile failure / 404s — routes not registered.

- [ ] **Step 3: Implement the handlers**

`internal/api/agent_questions.go`:

- `whoseTurnAgent(q store.AgentQuestion, msgs []store.AgentQuestionMessage) string` —
  `""` when resolved; no messages → `"user"` if `q.AskedBy != ""` else `"role"`;
  otherwise last message author `""` → `"role"`, else `"user"`.
- `buildAgentQuestionResponse(d Deps, q store.AgentQuestion) (agentQuestionResponse, error)`
  — loads messages + ordinal, reuses `toQuestionMessageResponse` by converting
  each `store.AgentQuestionMessage` into the same JSON shape.
- `deliverHumanEntry(d Deps, roleID string, ordinal int, entry, text string) error`
  — marshals `{question_id, role_id, ordinal, entry, text}` into a `question`
  inbox event via `d.Store.EnqueueInboxEvent`, then, if a live instance exists,
  also queues `fmt.Sprintf("[role %s Q%d %s] %s", roleID, ordinal, entry, text)`
  into that session (mirror `deliverToOrchestrator`: `rewriteAttachmentLinks`,
  `d.Store.AddMessage`, `d.Bus.Publish("message.queued", ...)`, `d.Queue.Wake`).
- `liveRoleInstance(d Deps, roleID string) (string, error)` — first session from
  `d.Store.ListSessions(store.SessionFilter{Kind: "agent"})` (live-only by
  default) whose `roleFromSessionID` equals roleID; `""` when none.
- `callerIsRoleInstance(caller *store.Session, roleID string) bool` —
  `caller.Kind == "agent" && roleFromSessionID(caller.ID) == roleID`.
- `handlePostAgentQuestions`: `lookupAgent` → `callerSession` → 403 unless
  `caller == nil || callerIsRoleInstance(caller, a.ID)` → decode `{body, context}`
  → 400 `empty_body` on empty body → `AddAgentQuestion` with
  `AskedBy: callerAuthor(caller)` → when `caller == nil`, `deliverHumanEntry(d, a.ID, ordinal, "question", req.Body)`
  → `d.Bus.Publish("agent.question_asked", callerLabel(caller), map[string]any{"role_id": a.ID, "question_id": qid})`
  → 201 with the built response.
- `handleGetAgentQuestions`: `lookupAgent` → `ListAgentQuestions(a.ID, r.URL.Query().Get("status") == "open")`
  → `{"questions": [...]}`, 200.
- `parseAgentQuestionID` / `getAgentQuestionOr404` mirroring
  `parseQuestionID` / `getQuestionOr404` with code `agent_question_not_found`.
- `handlePostAgentQuestionReply`: load question → load role (`d.Store.GetAgent(q.RoleID)`;
  `store.ErrNotFound` → 404) → caller check as above → if `q.Status != "open"`:
  human caller gets 409 `question_resolved`, role instance reopens
  (`ReopenAgentQuestion`, 409 on failure, publish `agent.question_reopened`) →
  decode/validate body → `AddAgentQuestionMessage{Kind:"reply", Author: callerAuthor(caller)}`
  → when `caller == nil`, `deliverHumanEntry(d, q.RoleID, ordinal, "reply", req.Body)` →
  publish `agent.question_replied` → 201 with the rebuilt response.
- `handlePostAgentQuestionAnswer`: human only (`caller != nil` → 403) → 409 unless
  open → decode `{body, dismiss}` → dismiss: `ResolveAgentQuestion(id, "dismissed")`
  and nothing else; otherwise require non-empty body, `ResolveAgentQuestion(id, "answered")`
  first (409 on `store.ErrQuestionResolved`), then `AddAgentQuestionMessage{Kind:"answer"}`
  and `deliverHumanEntry(d, q.RoleID, ordinal, "answer", req.Body)` → publish
  `agent.question_resolved` with `resolution` → 200.
- `registerAgentQuestionRoutes(mux, d)` wiring:
  `GET /v1/agents/{id}/questions`, `POST /v1/agents/{id}/questions`,
  `POST /v1/agent-questions/{id}/reply`, `POST /v1/agent-questions/{id}/answer`.

In `internal/api/agents.go`, call `registerAgentQuestionRoutes(mux, d)` at the end
of `registerAgentRoutes`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/api/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api/agent_questions.go internal/api/agent_questions_test.go internal/api/agents.go
git commit -m "api: /v1/agents/{id}/questions and agent-question reply/answer"
```

---

### Task 3: Open-question counts in the agents list

**Files:**
- Modify: `internal/api/agents.go:32-45` (`agentResponse`), `:79-115`
  (`toAgentResponse`), `:119-135` (list handler)
- Test: `internal/api/agents_test.go`

**Interfaces:**
- Consumes: `store.OpenAgentQuestionCounts` from Task 1.
- Produces: `agentResponse` gains `OpenQuestions int \`json:"open_questions"\`` and
  `AwaitingUser int \`json:"awaiting_user"\``.

- [ ] **Step 1: Write the failing test**

Append to `internal/api/agents_test.go`:

```go
func TestListAgentsCarriesOpenQuestionCounts(t *testing.T) {
	srv, d := newTestServer(t)
	seedAgent(t, d, "sre")

	// Human-opened thread: open, but awaiting the role, not the user.
	if _, err := d.Store.AddAgentQuestion(store.AgentQuestion{RoleID: "sre", Body: "q1"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	// Role-opened thread: awaits the user.
	if _, err := d.Store.AddAgentQuestion(store.AgentQuestion{RoleID: "sre", AskedBy: "sre-run-1", Body: "q2"}); err != nil {
		t.Fatalf("add: %v", err)
	}

	_, body := do(t, srv, "GET", "/v1/agents", nil, "")
	var list []agentResponse
	mustUnmarshal(t, body, &list)
	if len(list) != 1 || list[0].OpenQuestions != 2 || list[0].AwaitingUser != 1 {
		t.Fatalf("list = %+v", list)
	}

	_, body = do(t, srv, "GET", "/v1/agents/sre", nil, "")
	var one agentResponse
	mustUnmarshal(t, body, &one)
	if one.OpenQuestions != 2 || one.AwaitingUser != 1 {
		t.Fatalf("get = %+v", one)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run ListAgentsCarriesOpenQuestionCounts -v`
Expected: FAIL — `OpenQuestions` undefined.

- [ ] **Step 3: Implement**

Add the two fields to `agentResponse`. Change `toAgentResponse` to take the
counts map so the list handler fetches it once:

```go
func toAgentResponse(d Deps, a store.Agent, withPrompt bool, counts map[string]QuestionCountsAlias) (agentResponse, error)
```

Concretely: signature becomes
`toAgentResponse(d Deps, a store.Agent, withPrompt bool, counts map[string]store.QuestionCounts)`;
inside, `c := counts[a.ID]; resp.OpenQuestions = c.Open; resp.AwaitingUser = c.AwaitingUser`.
The list handler calls `d.Store.OpenAgentQuestionCounts()` once before its loop
and passes the map. `writeAgent` calls `OpenAgentQuestionCounts()` itself and
passes the result (single-role responses are not hot paths).

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/api/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api/agents.go internal/api/agents_test.go
git commit -m "api: open_questions/awaiting_user counts on agent responses"
```

---

### Task 4: CLI — ask / reply / answer / questions

**Files:**
- Create: `internal/cli/agent_questions.go`
- Modify: `internal/cli/agent.go:19-27` (subcommand wiring)
- Test: `internal/cli/agent_questions_test.go`

**Interfaces:**
- Consumes: existing `connect`, `apiPath`, `printJSON`, `flags.JSON`,
  `usageError`, `roleFromSessionID` (already in `internal/cli/agent.go`).
- Produces:
  ```go
  type agentQuestionRow struct {
      ID         int64                 `json:"id"`
      RoleID     string                `json:"role_id"`
      Ordinal    int                   `json:"ordinal"`
      AskedBy    string                `json:"asked_by"`
      Body       string                `json:"body"`
      Context    string                `json:"context,omitempty"`
      Status     string                `json:"status"`
      WhoseTurn  string                `json:"whose_turn,omitempty"`
      Messages   []questionMessageRow  `json:"messages"`
  }
  func newAgentAskCmd() *cobra.Command
  func newAgentReplyCmd() *cobra.Command
  func newAgentAnswerCmd() *cobra.Command
  func newAgentQuestionsCmd() *cobra.Command
  func renderAgentQuestions(role string, qs []agentQuestionRow) string
  ```
  (`questionMessageRow` is the existing message row type used by
  `internal/cli/task.go`'s `questionRow`; reuse it rather than declaring a twin.)

- [ ] **Step 1: Write the failing test**

`internal/cli/agent_questions_test.go` — follow the CLI test style in
`internal/cli/agent_test.go` (fake daemon over `httptest` + `executeCommand`
helper; use whatever helper names that file already defines).

```go
func TestAgentAskSendsQuestion(t *testing.T) {
	var gotPath, gotBody string
	srv := fakeDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotPath, gotBody = r.URL.Path, string(b)
		writeJSONResponse(w, map[string]any{"id": 7, "ordinal": 1, "role_id": "sre", "status": "open"})
	})
	defer srv.Close()

	out, err := runCLI(t, srv, "agent", "ask", "sre", "почему упал деплой?")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if gotPath != "/v1/agents/sre/questions" {
		t.Fatalf("path = %s", gotPath)
	}
	if !strings.Contains(gotBody, "почему упал деплой?") {
		t.Fatalf("body = %s", gotBody)
	}
	if !strings.Contains(out, "Q1 (#7)") {
		t.Fatalf("out = %q", out)
	}
}

func TestAgentReplyPostsToQuestion(t *testing.T) {
	var gotPath string
	srv := fakeDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeJSONResponse(w, map[string]any{"id": 7, "ordinal": 1, "role_id": "sre", "status": "open"})
	})
	defer srv.Close()

	if _, err := runCLI(t, srv, "agent", "reply", "7", "разбираюсь"); err != nil {
		t.Fatalf("run: %v", err)
	}
	if gotPath != "/v1/agent-questions/7/reply" {
		t.Fatalf("path = %s", gotPath)
	}
}

func TestAgentAnswerRequiresBodyOrDismiss(t *testing.T) {
	srv := fakeDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSONResponse(w, map[string]any{"id": 7, "ordinal": 1, "status": "resolved"})
	})
	defer srv.Close()

	if _, err := runCLI(t, srv, "agent", "answer", "7"); err == nil {
		t.Fatal("answer with neither body nor --dismiss must fail")
	}
	if _, err := runCLI(t, srv, "agent", "answer", "7", "делай Б", "--dismiss"); err == nil {
		t.Fatal("answer with both body and --dismiss must fail")
	}
	if _, err := runCLI(t, srv, "agent", "answer", "7", "делай Б"); err != nil {
		t.Fatalf("valid answer failed: %v", err)
	}
}

func TestAgentQuestionsRendersThread(t *testing.T) {
	srv := fakeDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSONResponse(w, map[string]any{"questions": []map[string]any{{
			"id": 7, "ordinal": 1, "role_id": "sre", "status": "open",
			"whose_turn": "user", "body": "нужно решение",
			"messages": []map[string]any{{"author": "sre-run-1", "body": "детали"}},
		}}})
	})
	defer srv.Close()

	out, err := runCLI(t, srv, "agent", "questions", "sre")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"agent sre", "Q1 (#7) [open]", "ждёт ответа пользователя", "[sre-run-1] детали"} {
		if !strings.Contains(out, want) {
			t.Fatalf("out = %q, missing %q", out, want)
		}
	}
}

func TestAgentReplyResolvesRoleFromSession(t *testing.T) {
	// `rocket agent questions` with no role argument inside an instance
	// defaults to that instance's role.
	t.Setenv("ROCKET_SESSION_ID", "sre-run-2")
	var gotPath string
	srv := fakeDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeJSONResponse(w, map[string]any{"questions": []map[string]any{}})
	})
	defer srv.Close()

	if _, err := runCLI(t, srv, "agent", "questions"); err != nil {
		t.Fatalf("run: %v", err)
	}
	if gotPath != "/v1/agents/sre/questions" {
		t.Fatalf("path = %s", gotPath)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run Agent -v`
Expected: FAIL — `unknown command "ask" for "agent"`.

- [ ] **Step 3: Implement the commands**

`internal/cli/agent_questions.go`:

- `newAgentAskCmd`: `ask <role> "<вопрос>" [--context <md>]` → POST
  `apiPath("v1","agents",role,"questions")` with `{body, context?}`. Prints
  `question Q%d (#%d) opened`. Direction is decided by the daemon from the
  session header, so no client-side branching — the doc comment says so.
- `newAgentReplyCmd`: `reply <question-id> "<текст>"` → POST
  `apiPath("v1","agent-questions",id,"reply")`; validates the id parses as int64.
- `newAgentAnswerCmd`: `answer <question-id> ["<ответ>"] [--dismiss]` → POST
  `apiPath("v1","agent-questions",id,"answer")`; exactly one of body/`--dismiss`
  (same `hasBody == dismiss` guard as `newTaskAnswerCmd`).
- `newAgentQuestionsCmd`: `questions [<role>] [--open]` → role defaults to
  `resolveRole("")` (the instance's own role); GET
  `apiPath("v1","agents",role,"questions")` plus `?status=open`; JSON or
  `renderAgentQuestions`.
- `renderAgentQuestions(role string, qs []agentQuestionRow) string` — mirrors
  `renderQuestions` with header `agent <role>` and arrows
  `" → ждёт ответа пользователя"` (`whose_turn == "user"`) /
  `" → ждёт роль"` (`whose_turn == "role"`); thread lines `  [user] ...` /
  `  [<session>] ...`.

Wire all four into `newAgentCmd` in `internal/cli/agent.go`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/cli/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/agent_questions.go internal/cli/agent_questions_test.go internal/cli/agent.go
git commit -m "cli: rocket agent ask/reply/answer/questions"
```

---

### Task 5: Documentation

**Files:**
- Modify: `docs/10-agents.md` (Q&A subsection under «Роли»)
- Modify: `docs/03-daemon-api.md` (endpoints)
- Modify: `docs/04-cli.md` (commands)

- [ ] **Step 1: Document the threads in `docs/10-agents.md`**

Add after the core-layer bullet list:

```markdown
### Q&A-треды роли

Тот же механизм, что у задач ([12-tasks.md](12-tasks.md)), только адресат — роль:
таблицы `agent_questions` / `agent_question_messages`, оба направления.

- **Человек → роль**: `rocket agent ask <role> "..."` (или `POST /v1/agents/{id}/questions`
  без заголовка сессии). Вопрос кладёт в инбокс роли событие `question` —
  роль просыпается и отвечает `rocket agent reply <qid> "..."`.
- **Роль → человек**: тот же `rocket agent ask` изнутри инстанса роли
  (заголовок `X-Rocket-Session: <role>-run-<n>`): `asked_by` = id сессии, событие
  в инбокс не кладётся, на карточке роли загорается бейдж «ждёт ответа».
- Закрывает тред только человек: `rocket agent answer <qid> "<ответ>" | --dismiss`.
  Ответ доезжает до роли тем же событием `question`. Инстанс роли может
  оспорить закрытый тред — его reply переоткрывает вопрос (как у задач).
- Чья очередь (`whose_turn`: `user` | `role`) выводится из автора последней записи
  треда; счётчики `open_questions` / `awaiting_user` отдаются в `GET /v1/agents`.
```

- [ ] **Step 2: Document endpoints in `docs/03-daemon-api.md`**

In the agents section, add:

```markdown
- `GET /v1/agents/{id}/questions[?status=open]` — треды роли
- `POST /v1/agents/{id}/questions` `{body, context?}` — открыть тред
  (человек → роль кладёт в инбокс событие `question`; инстанс роли → человек — нет)
- `POST /v1/agent-questions/{qid}/reply` `{body}` — ответ в тред (человек или инстанс роли;
  reply инстанса переоткрывает закрытый тред)
- `POST /v1/agent-questions/{qid}/answer` `{body} | {dismiss:true}` — закрыть тред (только человек)
```

Also note that `GET /v1/agents` entries now carry `open_questions` and `awaiting_user`.

- [ ] **Step 3: Document commands in `docs/04-cli.md`**

```markdown
rocket agent ask <role> "<вопрос>" [--context <md>]   # открыть тред (направление — по вызывающему)
rocket agent questions [<role>] [--open]              # треды роли
rocket agent reply <qid> "<текст>"                    # ответ в тред
rocket agent answer <qid> "<ответ>" | --dismiss       # закрыть тред (человек)
```

- [ ] **Step 4: Commit**

```bash
git add docs/10-agents.md docs/03-daemon-api.md docs/04-cli.md
git commit -m "docs: role Q&A threads (API, CLI, agents doc)"
```

---

### Task 6: Verification and PR

- [ ] **Step 1: Full build, vet and test**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all PASS. If the repo has `make check` / `make test`, run that instead.

- [ ] **Step 2: End-to-end exercise against a real daemon**

Build the binary, start a daemon against a throwaway home, and walk both
directions:

```bash
go build -o /tmp/rocket ./cmd/rocket
# with the daemon running against a scratch ROCKET_HOME and a project seeded:
/tmp/rocket agent add sre --project <p> --prompt-file /tmp/role.md
/tmp/rocket agent ask sre "почему упал деплой?"
/tmp/rocket agent questions sre            # thread visible, awaits the role
/tmp/rocket agent show sre                 # queued inbox event of kind question
/tmp/rocket agent answer <qid> "делай Б"
/tmp/rocket agent questions sre            # resolved
/tmp/rocket agent ls                       # open_questions column reflects reality
```

Record the actual output in the PR description.

- [ ] **Step 3: Open the PR**

```bash
git push -u origin feature/task-639/qa-threads
gh pr create --title "qa-threads: role Q&A threads (#644)" --body "..."
```

Reference feature `task-639` and subtask #644 in the body; list the endpoints,
CLI commands, and the verification output from Step 2.
