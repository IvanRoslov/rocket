# T4 heartbeat-stale: устаревание тредов и анти-stall nudge — план реализации

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:test-driven-development. Шаги помечены чекбоксами (`- [ ]`).

**Goal:** heartbeat начинает (а) напоминать участникам attention-множества об открытых decision-тредах без движения дольше `question_stale_after` и (б) выводить оркестратора из зависания на интерактивном промпте — nudge-сообщением в его очередь плюс записью `problem` в журнал корневой задачи.

**Architecture:** обе ветки — новые проходы существующего `heartbeat.Tick`. Устаревание тредов не привязано к оркестратору: это отдельный проход по `store.ListOpenThreads()` (все треды задач и ролей сразу), поэтому он живёт в новом файле `internal/heartbeat/stale.go` и вызывается из `Tick` один раз за тик, а не внутри `tickOne`. Доставка напоминания повторяет правила `internal/api/agent_delivery.go` (живой сессии — очередь, спящему постоянному агенту — inbox, человеку — только событие шины), но реализована локально: heartbeat зависит только от `store`. Анти-stall — расширение уже существующего `escalateInputStall` (#1004).

**Tech Stack:** Go 1.x, SQLite через `internal/store`, шина `internal/bus`, конфиг `internal/config` (YAML), стандартный `go test`.

## Global Constraints

- Спека: task doc #1023 «Спека v1», §«Устаревание тредов», §«Input stall: оркестратор не ждёт на промпте»; критерий приёмки 8.
- `question_stale_after` — новый ключ config.yaml, default `24h`.
- Анти-спам напоминаний: не чаще одного напоминания **на тред на получателя** в **фиксированные 24h** (не `question_stale_after`: `stale_after: 2h` не должен означать напоминание каждые 2 часа). Состояние — в памяти heartbeat; перезапуск демона повторит напоминание один раз, это допустимо (решение оркестратора, зафиксировать в описании PR).
- Анти-спам nudge: один раз на эпизод зависания — пока состояние не менялось, не повторять.
- Человеку сообщение не доставляется никогда: его канал — аддитивное поле `stale` в списковых ответах API тредов (плюс событие шины). `internal/api/threads.go` — только аддитивная правка, без рефакторинга (его владелец — параллельный T3).
- Текст напоминания использует целевую форму локальной ссылки (`1023/Q2`) и упоминает `rocket task answer` как запасную команду, пока T3 не смержен.
- Только `type = decision` и `status = open` треды; `fyi` не напоминает никогда.
- Ни один сбой доставки не должен ронять тик: логируем и идём дальше (правило `escalateInputStall`).
- Комментарии и доки — по-русски там, где вокруг русский (docs/), код и комментарии в коде — по-английски, как в `internal/heartbeat`.
- Верификация: `gofmt -l`, `go vet ./...`, `go test ./...`.

---

### Task 1: конфиг `question_stale_after`

**Files:**
- Modify: `internal/config/config.go` (константа рядом с `DefaultInputStallThreshold`, поле, default в `Load`)
- Test: `internal/config/config_test.go`
- Modify: `docs/05-state.md` (строка в примере config.yaml)

**Interfaces:**
- Produces: `config.DefaultQuestionStaleAfter = 24 * time.Hour`; `Config.QuestionStaleAfter time.Duration` (`yaml:"question_stale_after"`).

- [ ] **Step 1: Написать падающий тест**

```go
func TestLoadQuestionStaleAfterDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ROCKET_HOME", home)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.QuestionStaleAfter != DefaultQuestionStaleAfter {
		t.Errorf("QuestionStaleAfter = %s, want %s", cfg.QuestionStaleAfter, DefaultQuestionStaleAfter)
	}
}

func TestLoadQuestionStaleAfterFromYAML(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ROCKET_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("question_stale_after: 6h\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.QuestionStaleAfter != 6*time.Hour {
		t.Errorf("QuestionStaleAfter = %s, want 6h", cfg.QuestionStaleAfter)
	}
}
```

Перед написанием — свериться с уже существующими тестами `config_test.go` на `input_stall_threshold` и повторить их способ задания HOME (если там не `ROCKET_HOME`, использовать тот же приём).

- [ ] **Step 2: Убедиться, что тест падает**

Run: `go test ./internal/config/ -run QuestionStaleAfter -v`
Expected: FAIL, `cfg.QuestionStaleAfter undefined`.

- [ ] **Step 3: Реализовать**

```go
// DefaultQuestionStaleAfter is the built-in value of
// Config.QuestionStaleAfter — how long an open decision thread may sit
// without movement before the heartbeat reminds everyone whose turn it is.
const DefaultQuestionStaleAfter = 24 * time.Hour
```

Поле рядом с `QuestionReminderThreshold`:

```go
	// QuestionStaleAfter is how long an open decision thread may go without
	// movement (its last entry, or the question itself when it has none)
	// before the heartbeat sends one reminder to every participant in its
	// attention set. Zero (an absent key) means DefaultQuestionStaleAfter.
	QuestionStaleAfter time.Duration `yaml:"question_stale_after"`
```

Default в `Load`: `QuestionStaleAfter: DefaultQuestionStaleAfter,` (YAML-ключ, если задан, перезапишет его при `yaml.Unmarshal`, как у остальных полей).

- [ ] **Step 4: Тесты зелёные**

Run: `go test ./internal/config/ -v`
Expected: PASS.

- [ ] **Step 5: Строка в docs/05-state.md** рядом с `input_stall_threshold`:

```
question_stale_after: 24h   # сколько открытый decision-тред может висеть без движения до напоминания участникам attention
```

- [ ] **Step 6: Коммит**

```bash
git add internal/config docs/05-state.md
git commit -m "config: question_stale_after (default 24h)"
```

---

### Task 2: `OpenThread` отдаёт тип треда

**Files:**
- Modify: `internal/store/open_threads.go` (SELECT и скан)
- Test: `internal/store/open_threads_test.go` (если файла нет — создать)

**Interfaces:**
- Produces: `OpenThread.Question.Type` заполнен (`decision`|`fyi`) у каждого открытого треда.

Зачем: `ListOpenThreads` сейчас не выбирает `q.type`, а проход по stale-тредам обязан пропускать `fyi` (переоткрытый fyi-тред остаётся открытым и в норме получает тип `decision`, но полагаться на это без чтения колонки нельзя).

- [ ] **Step 1: Падающий тест**

```go
func TestListOpenThreadsCarriesType(t *testing.T) {
	s := openTestStore(t)   // helper, уже используемый в internal/store тестах
	taskID := seedTask(t, s) // см. соседние тесты пакета; если такого helper нет — создать задачу напрямую
	id, err := s.AddQuestion(Question{TaskID: taskID, Body: "?", Type: QuestionTypeDecision})
	if err != nil {
		t.Fatal(err)
	}
	threads, err := s.ListOpenThreads()
	if err != nil {
		t.Fatal(err)
	}
	var got string
	for _, th := range threads {
		if th.Question.ID == id {
			got = th.Question.Type
		}
	}
	if got != QuestionTypeDecision {
		t.Errorf("Question.Type = %q, want %q", got, QuestionTypeDecision)
	}
}
```

- [ ] **Step 2: Убедиться, что падает**

Run: `go test ./internal/store/ -run ListOpenThreadsCarriesType -v`
Expected: FAIL, `Question.Type = "" `.

- [ ] **Step 3: Реализовать** — добавить `q.type` в SELECT `ListOpenThreads` (сразу после `q.resolution`) и `&q.Type` в соответствующую позицию `rows.Scan`. Колонка `NOT NULL DEFAULT 'decision'`, поэтому `sql.NullString` не нужен.

- [ ] **Step 4: Тесты зелёные**

Run: `go test ./internal/store/ -v`
Expected: PASS.

- [ ] **Step 5: Коммит**

```bash
git add internal/store
git commit -m "store: ListOpenThreads отдаёт type треда"
```

---

### Task 3: детектор устаревшего треда (чистая функция)

**Files:**
- Create: `internal/heartbeat/stale.go`
- Create: `internal/heartbeat/stale_test.go`

**Interfaces:**
- Consumes: `store.OpenThread` (с `Question.Type` из Task 2).
- Produces:
  - `func StaleThread(th store.OpenThread, now time.Time, after time.Duration) (since time.Duration, ok bool)` — экспортируемая, чтобы её можно было переиспользовать в API/CLI-бейджах (T3/T5) без дублирования правила.

Правило: тред устарел, если `Question.Status == "open"`, `Question.Type != store.QuestionTypeFYI`, attention-множество непусто и от последнего движения прошло больше `after`. Точка отсчёта — `LastMessage.CreatedAt`, а при отсутствии сообщений — `Question.AskedAt`; непригодная (нулевая) метка означает «не устарел», а не «устарел с эпохи» — то же решение, что в `InputStalled`.

- [ ] **Step 1: Падающие тесты**

```go
package heartbeat

import (
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/store"
)

func TestStaleThread(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	old := now.Add(-30 * time.Hour).Unix()
	fresh := now.Add(-time.Hour).Unix()
	after := 24 * time.Hour

	base := func() store.OpenThread {
		return store.OpenThread{
			Question:  store.Question{ID: 1, Status: "open", Type: store.QuestionTypeDecision, AskedAt: old},
			Attention: []string{"cto"},
		}
	}

	t.Run("no messages, question older than after", func(t *testing.T) {
		if since, ok := StaleThread(base(), now, after); !ok || since != 30*time.Hour {
			t.Fatalf("StaleThread = (%s, %v), want (30h, true)", since, ok)
		}
	})

	t.Run("last message is the reference point", func(t *testing.T) {
		th := base()
		th.LastMessage = &store.QuestionMessage{CreatedAt: fresh}
		if _, ok := StaleThread(th, now, after); ok {
			t.Fatal("thread with a fresh reply must not be stale")
		}
	})

	t.Run("empty attention is nobody's turn", func(t *testing.T) {
		th := base()
		th.Attention = nil
		if _, ok := StaleThread(th, now, after); ok {
			t.Fatal("thread waiting on nobody must not be stale")
		}
	})

	t.Run("fyi never goes stale", func(t *testing.T) {
		th := base()
		th.Question.Type = store.QuestionTypeFYI
		if _, ok := StaleThread(th, now, after); ok {
			t.Fatal("fyi thread must not be stale")
		}
	})

	t.Run("resolved never goes stale", func(t *testing.T) {
		th := base()
		th.Question.Status = "resolved"
		if _, ok := StaleThread(th, now, after); ok {
			t.Fatal("resolved thread must not be stale")
		}
	})

	t.Run("no usable timestamp", func(t *testing.T) {
		th := base()
		th.Question.AskedAt = 0
		if _, ok := StaleThread(th, now, after); ok {
			t.Fatal("thread without a timestamp must not be stale")
		}
	})
}
```

- [ ] **Step 2: Убедиться, что падает**

Run: `go test ./internal/heartbeat/ -run StaleThread -v`
Expected: FAIL, `undefined: StaleThread`.

- [ ] **Step 3: Реализовать в `stale.go`**

```go
// StaleThread reports how long an open decision thread has gone without
// movement and whether that exceeds after. Movement is the thread's last
// entry — its last message, or the question itself when nobody has replied
// yet. A thread waiting on nobody (empty attention set), an fyi note, a
// resolved thread, or one without a usable timestamp is never stale.
func StaleThread(th store.OpenThread, now time.Time, after time.Duration) (since time.Duration, ok bool) {
	if th.Question.Status != "open" || th.Question.Type == store.QuestionTypeFYI {
		return 0, false
	}
	if len(th.Attention) == 0 {
		return 0, false
	}
	ref := th.Question.AskedAt
	if th.LastMessage != nil && th.LastMessage.CreatedAt > 0 {
		ref = th.LastMessage.CreatedAt
	}
	if ref <= 0 {
		return 0, false
	}
	since = now.Sub(time.Unix(ref, 0))
	return since, since > after
}
```

- [ ] **Step 4: Тесты зелёные**

Run: `go test ./internal/heartbeat/ -v`
Expected: PASS.

- [ ] **Step 5: Коммит**

```bash
git add internal/heartbeat/stale.go internal/heartbeat/stale_test.go
git commit -m "heartbeat: правило устаревания вопросного треда"
```

---

### Task 4: доставка напоминания участнику attention

**Files:**
- Modify: `internal/heartbeat/stale.go`
- Modify: `internal/heartbeat/stale_test.go`

**Interfaces:**
- Consumes: `store.Store` (`GetSession`, `GetAgent`, `AddMessage`, `AddInboxMessage`), `bus.Bus`, `h.wake`.
- Produces (методы `*Heartbeat`, неэкспортируемые):
  - `func (h *Heartbeat) remindParticipant(participant, body string) bool` — вернуть `true`, если напоминание куда-то ушло (человек считается «не доставлено»: ему адресован бейдж, а не сообщение).

Правила адресации (повторяют `internal/api/agent_delivery.go`, но без зависимости от пакета `api`):
- `store.IsHuman(participant)` → ничего не доставляем: человеку негде принять сообщение, его канал — бейдж в дашборде и `rocket questions` (T3/T5), которые читают то же событие/те же данные. Возвращаем `false`.
- Есть живая (`spawning|running`) сессия с таким id → `AddMessage{ToSession: id}`, `bus.Publish("message.queued", …)`, `h.wake(id)`.
- Иначе, если `GetAgent(participant)` находит постоянного агента → `AddInboxMessage{AgentID: participant, From: "", Body: body}`.
- Иначе — `slog.Warn` и `false` (эфемерная сессия умерла: инбокса у неё нет, доставлять некуда).

- [ ] **Step 1: Падающие тесты**

```go
func TestRemindParticipant_LiveSessionGetsQueuedMessage(t *testing.T) {
	st := openTestStore(t)
	b := bus.New(st)
	seedOrchAndTask(t, st, "orch1", "in_progress") // helper из heartbeat_test.go

	var woken []string
	hb := New(st, b, testConfig(), unknownActivity, func(to string) { woken = append(woken, to) })

	if !hb.remindParticipant("orch1", "reminder body") {
		t.Fatal("remindParticipant = false, want true for a live session")
	}
	msgs, err := st.ListMessages("orch1", "queued")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || !strings.Contains(msgs[0].Body, "reminder body") {
		t.Fatalf("queued messages = %+v, want one carrying the body", msgs)
	}
	if len(woken) != 1 || woken[0] != "orch1" {
		t.Errorf("woken = %v, want [orch1]", woken)
	}
}

func TestRemindParticipant_SleepingAgentGetsInbox(t *testing.T) {
	st := openTestStore(t)
	if err := st.AddAgent(store.Agent{ID: "cto", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	hb := New(st, bus.New(st), testConfig(), unknownActivity, func(string) {})

	if !hb.remindParticipant("cto", "reminder body") {
		t.Fatal("remindParticipant = false, want true for a registered agent")
	}
	msgs, err := st.ListInboxMessages("cto", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || !strings.Contains(msgs[0].Body, "reminder body") {
		t.Fatalf("inbox = %+v, want one message carrying the body", msgs)
	}
}

func TestRemindParticipant_HumanIsNotDelivered(t *testing.T) {
	st := openTestStore(t)
	hb := New(st, bus.New(st), testConfig(), unknownActivity, func(string) {})
	if hb.remindParticipant(store.ParticipantHuman, "reminder body") {
		t.Error("remindParticipant(human) = true, want false: the human is badged, not messaged")
	}
}
```

Точную сигнатуру `ListMessages`/`ListInboxMessages` сверить с `internal/store` перед написанием и подогнать вызов, не правило.

- [ ] **Step 2: Убедиться, что падает**

Run: `go test ./internal/heartbeat/ -run RemindParticipant -v`
Expected: FAIL, `hb.remindParticipant undefined`.

- [ ] **Step 3: Реализовать** по правилам выше; каждый сбой — `slog.Warn` и `false`, ошибки наружу не возвращаются.

- [ ] **Step 4: Тесты зелёные**

Run: `go test ./internal/heartbeat/ -v`
Expected: PASS.

- [ ] **Step 5: Коммит**

```bash
git add internal/heartbeat
git commit -m "heartbeat: адресация напоминания участнику attention"
```

---

### Task 5: проход по stale-тредам в `Tick`

**Files:**
- Modify: `internal/heartbeat/stale.go` (метод `sweepStaleThreads`, текст напоминания)
- Modify: `internal/heartbeat/heartbeat.go` (вызов из `Tick`; ключ анти-спама)
- Modify: `internal/heartbeat/stale_test.go`

**Interfaces:**
- Produces:
  - `func (h *Heartbeat) sweepStaleThreads() error`
  - `const staleKeyPrefix = "stale-thread:"` — пространство имён в `lastSent`, чтобы напоминание о треде не глушило сводку оркестратора и наоборот (тот же приём, что `escalationKeyPrefix`).
  - `func staleBody(th store.OpenThread, since time.Duration, ref string) string`

Анти-спам — один раз на тред **на получателя** за фиксированные 24h: ключ `staleKeyPrefix + questionID + ":" + participant`, окно — константа `staleReminderInterval = 24 * time.Hour` (намеренно не `cfg.QuestionStaleAfter`: `stale_after: 2h` иначе означал бы напоминание каждые 2 часа). Существующий `antiSpamOK` сравнивает с `cfg.HeartbeatInterval`, поэтому его надо обобщить: `antiSpamOK(key string, now time.Time, window time.Duration)`, а два существующих вызова передают `h.cfg.HeartbeatInterval` — поведение не меняется.

Текст напоминания (`ref` — локальная ссылка треда, `#<task>/Q<n>` для тредов задач, `<role>/Q<n>` для тредов ролей; ординал берётся из `store.QuestionOrdinal`):

```
[rocket stale thread] 1023/Q2 «<первые 60 символов вопроса>» ждёт вашего хода 30h.
Ответьте: rocket task reply 1023/Q2 "<текст>" — или закройте: rocket task close 1023/Q2 "<резолюция>" (пока close не смержен — rocket task answer 1023/Q2 "<резолюция>").
```

Для тредов ролей — `rocket agent reply|close|answer` и ссылка `<role>/Q<n>`. Событие шины на каждый устаревший тред: `question.stale` с `data{question_id, task_id, role_id, since_seconds, attention}` — из него дашборд (T5) и `rocket questions` (T3) рисуют бейдж «stale», в том числе для человека.

- [ ] **Step 1: Падающие тесты**

```go
func TestSweepStaleThreads_RemindsAttentionOnce(t *testing.T) {
	st := openTestStore(t)
	b := bus.New(st)
	cfg := testConfig()
	taskID := seedOrchAndTask(t, st, "orch1", "in_progress")
	if err := st.AddAgent(store.Agent{ID: "cto", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	qid, err := st.AddQuestion(store.Question{
		TaskID: taskID, Body: "Ship or hold?", Type: store.QuestionTypeDecision,
		AskedAt: time.Now().Add(-30 * time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetAttention(qid, []string{"cto"}); err != nil {
		t.Fatal(err)
	}

	hb := New(st, b, cfg, unknownActivity, func(string) {})
	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	inbox, err := st.ListInboxMessages("cto", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(inbox) != 1 {
		t.Fatalf("inbox = %d messages, want 1", len(inbox))
	}
	for _, want := range []string{"Ship or hold?", "30h", "close"} {
		if !strings.Contains(inbox[0].Body, want) {
			t.Errorf("body %q must contain %q", inbox[0].Body, want)
		}
	}
	if !hasEvent(eventTypes(t, st), "question.stale") {
		t.Error("want a question.stale event")
	}

	// Второй тик в пределах окна анти-спама не должен повторять напоминание.
	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if inbox, _ = st.ListInboxMessages("cto", "", 0); len(inbox) != 1 {
		t.Errorf("inbox = %d messages after the second tick, want 1 (anti-spam)", len(inbox))
	}
}

func TestSweepStaleThreads_FreshThreadIsQuiet(t *testing.T) {
	st := openTestStore(t)
	taskID := seedOrchAndTask(t, st, "orch1", "in_progress")
	if err := st.AddAgent(store.Agent{ID: "cto", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	qid, err := st.AddQuestion(store.Question{TaskID: taskID, Body: "Fresh?", AskedAt: time.Now().Unix()})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetAttention(qid, []string{"cto"}); err != nil {
		t.Fatal(err)
	}

	hb := New(st, bus.New(st), testConfig(), unknownActivity, func(string) {})
	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if inbox, _ := st.ListInboxMessages("cto", "", 0); len(inbox) != 0 {
		t.Errorf("inbox = %d messages, want 0 for a fresh thread", len(inbox))
	}
}
```

`testConfig()` дополнить полем `QuestionStaleAfter: 24 * time.Hour`.

- [ ] **Step 2: Убедиться, что падает**

Run: `go test ./internal/heartbeat/ -run SweepStale -v`
Expected: FAIL (напоминаний нет).

- [ ] **Step 3: Реализовать** `sweepStaleThreads` и вызвать его из `Tick` **до** прохода по оркестраторам; ошибка прохода логируется и не отменяет остальной тик (как `tickOne`). Треды, не привязанные ни к задаче, ни к роли, `ListOpenThreads` и так не возвращает.

- [ ] **Step 4: Тесты зелёные**

Run: `go test ./internal/heartbeat/ -v`
Expected: PASS.

- [ ] **Step 5: Коммит**

```bash
git add internal/heartbeat
git commit -m "heartbeat: напоминание участникам attention о stale-тредах"
```

---

### Task 5b: аддитивное поле `stale` в списковых ответах тредов

**Files:**
- Modify: `internal/api/threads.go` (только добавление поля и его вычисление; никакого рефакторинга — файл параллельно правит T3)
- Test: `internal/api/threads_test.go`

Человеку сообщение не доставляется — его канал — бейдж. Чтобы `rocket questions` (T3) и дашборд (T5) не пересчитывали правило заново, ответ треда получает `stale bool \`json:"stale,omitempty"\`` — то же `heartbeat.StaleThread` (Task 3), посчитанное на чтении, как уже считается `waiting_terminal` в `internal/api/waiting.go`. Порог берётся из `cfg.QuestionStaleAfter`; при нуле — `config.DefaultQuestionStaleAfter`.

- [ ] **Step 1: Падающий тест** — открытый decision-тред, `asked_at` 30 часов назад, attention `["human"]`: `GET` списка тредов задачи отдаёт `stale: true`; свежий тред поля не имеет. Форму запроса и helper'ы взять из соседних тестов `internal/api/threads_test.go`.
- [ ] **Step 2: Убедиться, что падает** — `go test ./internal/api/ -run Stale -v`, FAIL.
- [ ] **Step 3: Реализовать** — заполнять поле там же, где ответ уже собирается из `store.OpenThread` (если списковый обработчик читает треды иначе, собрать `OpenThread` из уже прочитанных данных, а не менять запросы).
- [ ] **Step 4: Тесты зелёные** — `go test ./internal/api/ -v`.
- [ ] **Step 5: Коммит**

```bash
git add internal/api
git commit -m "api: аддитивное поле stale у вопросного треда"
```

---

### Task 6: анти-stall nudge оркестратору + запись problem в журнал

**Files:**
- Modify: `internal/heartbeat/heartbeat.go` (`escalateInputStall`)
- Modify: `internal/heartbeat/heartbeat_test.go`

**Interfaces:**
- Produces: `func nudgeBody(orch store.Session) string`.

Что добавляется к существующей эскалации в inbox `cto`:

1. **Nudge только при `kind == "prompt"`** (то есть `PendingQuiz == ""`). При открытом квизе доставка сообщений сессии приостановлена (`docs/06-messaging.md`), поэтому nudge просто ляжет в очередь и ничего не разблокирует — там работает только эскалация человеку/`cto`. Текст:

```
[rocket] You are stalled on an interactive prompt nobody watches. Ask through the task instead: rocket task ask <task-id> "<question>" (add --to <who> to name whose turn it is).
```

Доставка — `AddMessage{ToSession: orch.ID}` + `bus.Publish("message.queued", …)` + `h.wake(orch.ID)`, тем же паттерном, что и обычная сводка.

1a. **Анти-спам — один раз на эпизод зависания**, а не раз в интервал: heartbeat запоминает точку отсчёта эпизода (`asked_at` квиза или `activity_ts` — ту самую, что вернула `InputStalled`) в `lastStallRef map[string]int64` и повторяет nudge/запись только когда точка сменилась, то есть началось новое зависание. Уже существующая эскалация в inbox `cto` свою логику анти-спама не меняет.

2. **Запись в журнал корневой задачи** — один раз на эпизод, тем же условием:

```go
_, err := h.st.AddTaskLog(store.TaskLogEntry{
	TaskID: task.ID,
	Kind:   "problem",
	Body:   fmt.Sprintf("Оркестратор %s висит на интерактивном вводе %dm (%s). Ожидание невыразимо в системе: спрашивать надо тредом (rocket task ask), а не промптом в терминале.", orch.ID, int(since.Minutes()), kind),
	Author: orch.ID,
})
```

Сбой записи логируется, тик продолжается.

- [ ] **Step 1: Падающие тесты**

```go
func TestTick_OrchestratorPromptStall_NudgesAndLogsProblem(t *testing.T) {
	st := openTestStore(t)
	cfg := testConfig()
	addCTOAgent(t, st)
	taskID := seedOrchAndTask(t, st, "orch1", "in_progress")
	setOrchInputState(t, st, "orch1", "waiting_input", time.Now().Add(-20*time.Minute).Unix(), "")

	hb := New(st, bus.New(st), cfg, unknownActivity, func(string) {})
	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	msgs, err := st.ListMessages("orch1", "queued")
	if err != nil {
		t.Fatal(err)
	}
	var nudged bool
	for _, m := range msgs {
		if strings.Contains(m.Body, "interactive prompt nobody watches") {
			nudged = true
		}
	}
	if !nudged {
		t.Errorf("queued messages = %+v, want a nudge", msgs)
	}

	log, err := st.ListTaskLog(taskID, "problem")
	if err != nil {
		t.Fatal(err)
	}
	if len(log) != 1 || !strings.Contains(log[0].Body, "orch1") {
		t.Fatalf("problem log = %+v, want one entry naming orch1", log)
	}
}

func TestTick_OrchestratorQuizStall_DoesNotNudge(t *testing.T) {
	st := openTestStore(t)
	addCTOAgent(t, st)
	askedAt := time.Now().Add(-20 * time.Minute).Unix()
	seedOrchAndTask(t, st, "orch1", "in_progress")
	setOrchInputState(t, st, "orch1", "active", time.Now().Unix(),
		`{"questions":[{"question":"Ship or hold?"}],"asked_at":`+strconv.FormatInt(askedAt, 10)+`}`)

	hb := New(st, bus.New(st), testConfig(), unknownActivity, func(string) {})
	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	msgs, _ := st.ListMessages("orch1", "queued")
	for _, m := range msgs {
		if strings.Contains(m.Body, "interactive prompt nobody watches") {
			t.Fatal("a pending quiz pauses delivery: nudge must not be queued")
		}
	}
}
```

- [ ] **Step 2: Убедиться, что падает**

Run: `go test ./internal/heartbeat/ -run Stall -v`
Expected: FAIL (нет nudge, нет записи problem).

- [ ] **Step 3: Реализовать** в `escalateInputStall`, после успешной проверки анти-спама и до/после записи в inbox — порядок не важен, важно, что все три действия происходят под одним ключом анти-спама.

- [ ] **Step 4: Тесты зелёные (весь пакет — старые тесты эскалации должны остаться зелёными)**

Run: `go test ./internal/heartbeat/ -v`
Expected: PASS.

- [ ] **Step 5: Коммит**

```bash
git add internal/heartbeat
git commit -m "heartbeat: nudge застрявшему на промпте оркестратору + problem в журнал"
```

---

### Task 7: `rocket ls` показывает ⏳ у зависшей сессии

**Files:**
- Modify: `internal/cli/sessions.go` (`renderSessions`)
- Modify: `internal/cli/waiting_test.go`

`rocket status` и `rocket task ls` уже подписывают состояние (`withWaitingGlyph`, `waitingTerminalMark`), а `rocket ls` получает `waiting_terminal` из API и молча его игнорирует — план T4 требует подписи и там. Кроме того бриф просит уточнить формулировку **для оркестраторов**: «⏳ ждёт ввода в терминале — вероятно, застрял» вместо нейтрального «⏳ ждёт ответа в терминале». Поэтому `waitingTerminalMark` разделяется на два: прежний текст остаётся для воркеров и прочих сессий, а для строки, у которой известен kind `orchestrator`, используется новый (`waitingTerminalMarkOrch`). В `rocket task ls` kind сессии в строке задачи неизвестен, но задача с сессией-оркестратором — это корневая задача фичи; чтобы не тянуть в CLI лишних запросов, там текст меняется на оркестраторский только если строка задачи корневая (`ParentID == 0`), иначе остаётся прежний.

- [ ] **Step 1: Падающий тест**

```go
// TestRenderSessionsWaitingTerminal: `rocket ls` marks the ACTIVITY cell of a
// session stalled on interactive input, the same way `rocket status` does.
func TestRenderSessionsWaitingTerminal(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	sessions := []sessionRow{
		{ID: "orch-1", Kind: "orchestrator", State: "running", Activity: "waiting_input",
			WaitingTerminal: true, CreatedAt: now.Add(-time.Hour).Unix()},
		{ID: "wk-1", Kind: "worker", State: "running", Activity: "editing foo.go",
			CreatedAt: now.Add(-time.Hour).Unix()},
	}

	var buf bytes.Buffer
	renderSessions(sessions, &buf, now)
	for _, line := range strings.Split(buf.String(), "\n") {
		switch {
		case strings.HasPrefix(line, "orch-1"):
			if !strings.Contains(line, "waiting_input "+waitingTerminalGlyph) {
				t.Errorf("line %q, want the activity cell tagged with %q", line, waitingTerminalGlyph)
			}
		case strings.HasPrefix(line, "wk-1"):
			if strings.Contains(line, waitingTerminalGlyph) {
				t.Errorf("line %q, want no marker", line)
			}
		}
	}
}
```

- [ ] **Step 2: Убедиться, что падает**

Run: `go test ./internal/cli/ -run RenderSessionsWaitingTerminal -v`
Expected: FAIL (глифа нет).

- [ ] **Step 3: Реализовать** — в `renderSessions` заменить `activity` на `withWaitingGlyph(activity, s.WaitingTerminal)` (функция уже есть в `internal/cli/status.go`); прочерк для пустой активности сохранить.

- [ ] **Step 4: Тесты зелёные**

Run: `go test ./internal/cli/ -v`
Expected: PASS.

- [ ] **Step 5: Коммит**

```bash
git add internal/cli
git commit -m "cli: rocket ls подписывает зависшую на вводе сессию"
```

---

### Task 8: доки и финальная верификация

**Files:**
- Modify: `docs/08-orchestrators.md` (§«Застревание на интерактивном вводе» + новый подраздел про устаревание тредов)
- Modify: `docs/03-daemon-api.md` (список типов событий: `question.stale`)
- Modify: `docs/12-tasks.md` (одна фраза про устаревание тредов со ссылкой на 08)

Большой доковый проход — T6; здесь правится только то, что этот PR фактически меняет.

- [ ] **Step 1:** В `docs/08-orchestrators.md` поправить строку «Сообщение самому застрявшему оркестратору намеренно не отправляется» — теперь при `waiting_input` без квиза nudge отправляется (при открытом квизе — по-прежнему нет, доставка приостановлена), и добавляется запись `problem` в журнал корневой задачи.
- [ ] **Step 2:** Туда же — подраздел «Устаревание тредов»: порог `question_stale_after` (24h), точка отсчёта, кому уходит напоминание (attention: живой сессии — в очередь, спящему агенту — в inbox, человеку — бейдж), анти-спам 1/сутки на тред, событие `question.stale`.
- [ ] **Step 3:** В `docs/03-daemon-api.md` добавить `question.stale` в перечень типов событий с формой `data`.
- [ ] **Step 4:** Полная верификация:

```bash
gofmt -l . | grep -v node_modules
go vet ./...
go test ./...
```

Expected: пусто, без ошибок, все пакеты PASS.

- [ ] **Step 5:** Ручная проверка на живом демоне не требуется (heartbeat завязан на время суток); вместо неё — интеграционный тик из Task 5, который проходит полный путь store→heartbeat→inbox/очередь/шина.

- [ ] **Step 6: Коммит и PR**

```bash
git add docs
git commit -m "docs: устаревание тредов и анти-stall nudge в heartbeat"
git push -u origin feature/task-1023/heartbeat-stale
gh pr create --title "heartbeat: напоминания о stale-тредах и анти-stall nudge (#1027)" --body "..."
```

---

## Self-review

- Спека §«Устаревание тредов» → Tasks 1–5. §«Input stall» (nudge + problem) → Task 6; подпись состояния в `ls`/`task ls` → Task 7 (в `task ls`/`status` уже есть с #1004). Критерий приёмки 8 покрыт тестами Tasks 5 и 6.
- Кнопка «закрыть с резолюцией» у stale-тредов в дашборде — T5, не здесь; событие `question.stale` (Task 5) — то, из чего T5 её рисует.
- Команда закрытия в тексте напоминания — целевая форма `rocket task close <ref>` из T3 (одна волна, мержится рядом). Если T3 не смержится до T4, форму заменить на `rocket task answer <id>` одной правкой в `staleBody`.
