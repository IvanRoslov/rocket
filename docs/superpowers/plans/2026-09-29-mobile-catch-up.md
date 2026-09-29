# Мобилка догоняет: чат и вопросы — план реализации

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Чат в мобилке показывает сообщения, доставленные через сокет Claude Code, чисто и в правильном порядке; появляется вкладка «Questions» со всеми открытыми вопросами и ответом одним нажатием с отменой.

**Architecture:** Демон при чтении транскрипта разворачивает конверт `<cross-session-message>` (новая `socketmsg.Unwrap`), форма API не меняется. Мобилка портирует из веба классификатор инжектов, дайджест вызовов инструментов и модель вопросов; вкладка «Questions» строится на `GET /v1/threads`, ответы уходят через отложенную очередь с окном отмены 5 с.

**Tech Stack:** Go (daemon), React + vitest (web), Expo 57 / React Native 0.86 / @tanstack/react-query / jest-expo + @testing-library/react-native (mobile).

**Spec:** `docs/superpowers/specs/2026-09-29-mobile-catch-up-design.md`

## Global Constraints

- Форма ответа `GET /v1/sessions/{id}/chat` не меняется: `{role, text, tool_name?, ts, quiz?}`.
- Язык UI мобилки — английский (весь существующий UI на английском): вкладка «Questions», секции «Waiting on you» / «Other open», плашка «Undo».
- Окно отмены ответа — **5000 мс**. Отложенный ответ отправляется при уходе с экрана и при уходе приложения в фон; теряется только по «Undo».
- `choose` — **1-based** индекс в `options`.
- Ответ на тред: `kind==='task'` → `POST /v1/questions/{id}/answer`; `kind==='role'` → `POST /v1/agent-questions/{id}/answer`. Тела: `{choose:N}` | `{body}` | `{dismiss:true}`.
- Тесты мобилки не кладутся в `mobile/app/` (expo-router подхватит их как роуты): чистые — рядом в `src/lib/*.test.ts`, экранные — в `mobile/__tests__/`.
- Документацию Expo сверять с v57 (`mobile/CLAUDE.md`).
- Команды: Go — `go test ./...` из корня; web — `cd web && npx vitest run <file>`; mobile — `cd mobile && npx jest <path>` и `npx tsc --noEmit`.
- Базовая линия: мобильный jest один раз показал 4 упавших набора, повторный прогон — 205/205 зелёные (флейки, не наши). Падение считается нашим, только если воспроизводится на двух прогонах.

## Review Focus

1. **Тело сообщения содержит текст про сам конверт** (например, агент цитирует `<cross-session-message>`) — `Unwrap` не должен обрезать тело по внутреннему тегу; экранированный `<\/cross-session-message` возвращается в исходный вид. → тест в Task 1.
2. **Конверт внутри длинного текста** (compaction-summary, где конверт встречается посреди) — не разворачивать, текст как есть. → тест в Task 1.
3. **Одинаковое сообщение, отправленное дважды подряд** — оба оптимистичных пузыря подтверждаются по одному разу, ни один не застревает. → тест в Task 5.
4. **Нажатие варианта ответа и сразу второго на другой карточке** — первый ответ отправляется (implicit commit), второй встаёт в окно отмены. → тест в Task 7.
5. **Ошибка сервера при отложенной отправке** (тред уже закрыт кем-то другим, 409) — карточка возвращается, показывается toast. → тест в Task 9.

---

## File Structure

| Файл | Ответственность |
|---|---|
| `internal/socketmsg/envelope.go` | + `Unwrap` — обратная к `Envelope` операция над текстом из транскрипта |
| `internal/agent/claudecode/chat.go` | вызывает `Unwrap` для user-записей |
| `docs/13-chat.md` | абзац «конверт сокета разворачивает демон» |
| `web/src/lib/classifyUserEntry.ts` | кадры тредов → system |
| `mobile/src/lib/chatDisplay.ts` | классификатор user-записей (новые форматы, kind `thread`) |
| `mobile/src/lib/toolDigest.ts` (new) | порт `web/src/lib/toolDigest.ts` (`summarizeToolEntry`) |
| `mobile/src/lib/chatRows.ts` | строки ленты: подтверждение отправок, `[large message]`, квиз-пары, группы инструментов |
| `mobile/app/chat/[id].tsx` | рендер новых строк, `res.body`, markdown у человека |
| `mobile/src/api/types.ts` | `ThreadInboxEntry`, `title?` у вопросов |
| `mobile/src/api/queries.ts` | `useThreads`, `useThreadAnswer` |
| `mobile/src/api/events.ts` | инвалидация `threads` |
| `mobile/src/lib/questions.ts` (new) | чистая модель вкладки: `waitingOnYou`, `otherOpen`, `threadSource`, `questionTitle` |
| `mobile/src/lib/deferred.ts` (new) | порт `web/src/screens/questions/deferred.ts` |
| `mobile/src/components/QuestionText.tsx` (new) | заголовок + markdown-тело вопроса |
| `mobile/src/components/ThreadCard.tsx` (new) | карточка треда во вкладке |
| `mobile/app/(tabs)/questions.tsx` (new) | экран вкладки |
| `mobile/app/(tabs)/_layout.tsx` | вкладка Questions вместо System, бейдж |
| `mobile/app/system.tsx` (moved from `(tabs)/system.tsx`) | экран System в стеке, с кнопкой «назад» |
| `mobile/app/(tabs)/settings.tsx` | ссылка на System |

---

### Task 1: `socketmsg.Unwrap`

**Files:**
- Modify: `internal/socketmsg/envelope.go`
- Test: `internal/socketmsg/envelope_test.go` (new)

**Interfaces:**
- Produces: `func Unwrap(s string) (body string, ok bool)` — `ok=false` ⇒ `s` не конверт, вызывающий оставляет текст как есть.

Реальная запись в транскрипте (проверено на 22 569 записях `~/.claude/projects`, все одного вида):

```
Another Claude session sent a message:\n
<cross-session-message from="uds:/tmp/cc-socks/rocketd-99908.sock" from-name="rocket">\n
<body>\n
</cross-session-message>\n
\n
This came from another Claude session — not typed by your user, … (хвост Claude Code)
```

- [ ] **Step 1: Write the failing test**

```go
package socketmsg

import "testing"

func TestUnwrap(t *testing.T) {
	const pre = "Another Claude session sent a message:\n"
	const trailer = "\n\nThis came from another Claude session — not typed by your user, but very likely working on their behalf."
	open := `<cross-session-message from="uds:/tmp/cc-socks/rocketd-1.sock" from-name="rocket">` + "\n"
	const closeTag = "\n</cross-session-message>"

	tests := []struct {
		name   string
		in     string
		want   string
		wantOK bool
	}{
		{"human via rocket", pre + open + "давай без миграции" + closeTag + trailer, "давай без миграции", true},
		{"agent with from prefix, multiline", pre + open + "[from w1] **done**\n\nPR #288" + closeTag + trailer, "[from w1] **done**\n\nPR #288", true},
		{"thread frame", pre + open + "[#4543/Q2 answer from human] go" + closeTag + trailer, "[#4543/Q2 answer from human] go", true},
		{"bare envelope without preamble and trailer", open + "hi" + closeTag, "hi", true},
		{"escaped close tag restored", pre + open + `quote: <\/cross-session-message> end` + closeTag + trailer, "quote: </cross-session-message> end", true},
		{"body mentions an opening tag", pre + open + "see <cross-session-message> docs" + closeTag + trailer, "see <cross-session-message> docs", true},
		{"unclosed", pre + open + "hi", "", false},
		{"foreign text before", "Summary:\n" + pre + open + "hi" + closeTag + trailer, "", false},
		{"foreign text after", pre + open + "hi" + closeTag + "\n\nsomething else", "", false},
		{"plain text", "hello there", "", false},
		{"empty", "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Unwrap(tc.in)
			if ok != tc.wantOK || got != tc.want {
				t.Errorf("Unwrap() = (%q, %v), ожидалось (%q, %v)", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestUnwrapRoundTrip(t *testing.T) {
	body := "line one\n</cross-session-message> inside\nline three"
	got, ok := Unwrap(Envelope("uds:/tmp/x.sock", "cto", body))
	if !ok || got != body {
		t.Fatalf("Unwrap(Envelope(body)) = (%q, %v), ожидалось (%q, true)", got, ok, body)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/socketmsg/ -run 'TestUnwrap' -v`
Expected: FAIL — `undefined: Unwrap`

- [ ] **Step 3: Write minimal implementation** — дописать в `envelope.go`:

```go
// Преамбула и хвост, которыми Claude Code обрамляет конверт в транскрипте
// получателя. Кроме них вокруг конверта ничего быть не должно — иначе это
// не доставленное сообщение, а текст, который его цитирует.
const (
	transcriptPreamble = "Another Claude session sent a message:\n"
	transcriptTrailer  = "\n\nThis came from another Claude session"
)

var (
	envelopeOpen    = regexp.MustCompile(`\A<` + EnvelopeTag + `(?: [^>\n]*)?>\n`)
	escapedCloseTag = regexp.MustCompile(`(?i)<\\/(` + EnvelopeTag + `)`)
)

// Unwrap — обратная к Envelope операция над текстом user-записи транскрипта:
// снимает преамбулу Claude Code, конверт и хвост и возвращает тело в том виде,
// в каком его отправили (экранированные `<\/cross-session-message` —
// восстановлены). ok=false, если текст не является ровно одним доставленным
// конвертом; тогда вызывающий оставляет текст как есть.
func Unwrap(s string) (string, bool) {
	s = strings.TrimPrefix(s, transcriptPreamble)
	loc := envelopeOpen.FindStringIndex(s)
	if loc == nil {
		return "", false
	}
	rest := s[loc[1]:]
	closeTag := "\n</" + EnvelopeTag + ">"
	// Тело не может содержать неэкранированный закрывающий тег (Envelope его
	// экранирует), поэтому конец тела — первое вхождение.
	end := strings.Index(rest, closeTag)
	if end < 0 {
		return "", false
	}
	tail := rest[end+len(closeTag):]
	if tail != "" && !strings.HasPrefix(tail, transcriptTrailer) {
		return "", false
	}
	return escapedCloseTag.ReplaceAllString(rest[:end], "</$1"), true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/socketmsg/ -v -run 'TestUnwrap'`
Expected: PASS (все подтесты)

- [ ] **Step 5: Commit**

```bash
git add internal/socketmsg/envelope.go internal/socketmsg/envelope_test.go
git commit -m "socketmsg: Unwrap — достать тело из конверта в транскрипте"
```

---

### Task 2: Демон отдаёт в чат развёрнутый текст

**Files:**
- Modify: `internal/agent/claudecode/chat.go:263-272` (ветка `case "user"` в `parseChatLine`)
- Test: `internal/agent/claudecode/chat_test.go`
- Modify: `docs/13-chat.md`

**Interfaces:**
- Consumes: `socketmsg.Unwrap(s string) (string, bool)` (Task 1)

- [ ] **Step 1: Write the failing test** — в конец `chat_test.go` (хелперы `writeTranscript`, `New` уже есть в файле):

```go
func TestTranscriptTailUnwrapsSocketEnvelope(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", base)
	wt := "/tmp/some/worktree"

	content := "Another Claude session sent a message:\n" +
		"<cross-session-message from=\"uds:/tmp/cc-socks/rocketd-1.sock\" from-name=\"cto\">\n" +
		"[from cto] ship it\n" +
		"</cross-session-message>\n\nThis came from another Claude session — not typed by your user."
	raw, err := json.Marshal(content)
	if err != nil {
		t.Fatal(err)
	}
	writeTranscript(t, base, wt, "sess.jsonl", []string{
		`{"type":"user","timestamp":"2026-07-18T21:00:26.830Z","message":{"role":"user","content":` + string(raw) + `}}`,
		`{"type":"user","timestamp":"2026-07-18T21:00:27.830Z","message":{"role":"user","content":"<system-reminder>x</system-reminder>"}}`,
	}, time.Now())

	entries, _, err := New().TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, "")
	if err != nil {
		t.Fatalf("TranscriptTail() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if got := entries[0].Text; got != "[from cto] ship it" {
		t.Errorf("entries[0].Text = %q, want %q", got, "[from cto] ship it")
	}
	if got := entries[1].Text; got != "<system-reminder>x</system-reminder>" {
		t.Errorf("non-envelope text changed: %q", got)
	}
}
```

Если `encoding/json` ещё не импортирован в `chat_test.go` — добавить.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/claudecode/ -run TestTranscriptTailUnwrapsSocketEnvelope -v`
Expected: FAIL — `entries[0].Text = "Another Claude session sent a message:\n<cross-session-message …"`

- [ ] **Step 3: Write minimal implementation** — в `parseChatLine`, ветка `case "user"`:

```go
	case "user":
		text, ok := extractUserText(rec.Message.Content)
		if !ok {
			if e, isQuiz := quizAnswerEntry(rec, ts, quizIDs); isQuiz {
				return []agent.ChatEntry{e}, true
			}
			return nil, false
		}
		// Доставка через сокет Claude Code кладёт тело в конверт с преамбулой
		// (docs/design/cc-socket-protocol.md §5). Клиентам нужен текст ровно в
		// том виде, в каком его отправили: по нему они подтверждают свои
		// отправки и распознают инжекты.
		if body, isEnvelope := socketmsg.Unwrap(text); isEnvelope {
			text = body
		}
		return []agent.ChatEntry{{Role: "user", Text: text, TS: ts}}, true
```

Импорт: `"github.com/IvanRoslov/rocket/internal/socketmsg"`. Обновить комментарий `extractUserText` («nothing is stripped» → «конверт сокета снимает parseChatLine»).

- [ ] **Step 4: Run tests**

Run: `go build ./... && go test ./internal/agent/claudecode/ ./internal/socketmsg/ ./internal/api/`
Expected: PASS

- [ ] **Step 5: Docs** — в `docs/13-chat.md`, в разделе про `role:"user"` записи, добавить абзац:

```markdown
**Конверт сокета.** Сообщения, доставленные через сокет Claude Code
(`socket_delivery`), лежат в транскрипте в конверте
`<cross-session-message …>` с преамбулой и хвостом Claude Code. Демон
снимает их при чтении (`socketmsg.Unwrap`): в `text` приходит тело ровно в
том виде, в каком его отправили (`[from X] …`, `[rocket …]`, текст
человека). Клиентам конверт распознавать не нужно.
```

- [ ] **Step 6: Commit**

```bash
git add internal/agent/claudecode/chat.go internal/agent/claudecode/chat_test.go docs/13-chat.md
git commit -m "chat: разворачивать конверт сокета — чистый текст и работающее подтверждение отправок"
```

---

### Task 3: Веб — кадры тредов как system

**Files:**
- Modify: `web/src/lib/classifyUserEntry.ts:14-30`
- Test: `web/src/lib/classifyUserEntry.test.ts`

- [ ] **Step 1: Write the failing test** — в `describe('classifyUserEntry', …)`:

```ts
  it('classifies thread frames (task and role refs) as system', () => {
    expect(classifyUserEntry('[#1023/Q2 reply from cto] go')).toBe('system')
    expect(classifyUserEntry('[cto/Q1 answer from human] yes')).toBe('system')
  })

  it('keeps a human message that merely starts with a bracket as user', () => {
    expect(classifyUserEntry('[draft] my notes')).toBe('user')
  })
```

- [ ] **Step 2: Run** `cd web && npx vitest run src/lib/classifyUserEntry.test.ts` — Expected: FAIL на первом тесте.

- [ ] **Step 3: Implement** — рядом с `FROM_PREFIX_RE`:

```ts
/** A Q&A thread frame from internal/api/threads.go threadPrefix: `[#1023/Q2 reply from cto]`, `[cto/Q1 answer from human]`. */
const THREAD_FRAME_RE = /^\[#?[\w.-]+\/Q\d+ [a-z-]+ from [^\]]+\]/
```

и в условие `system` добавить `|| THREAD_FRAME_RE.test(value)`.

- [ ] **Step 4: Run** тот же тест — Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/classifyUserEntry.ts web/src/lib/classifyUserEntry.test.ts
git commit -m "web: кадры Q&A-тредов в чате — системные строки, не сообщения человека"
```

---

### Task 4: Мобилка — классификатор инжектов

**Files:**
- Modify: `mobile/src/lib/chatDisplay.ts`
- Modify: `mobile/src/lib/chatRows.ts:18` (`isNoise`)
- Test: `mobile/src/lib/chatDisplay.test.ts`

**Interfaces:**
- Produces:
  ```ts
  export type UserDisplay =
    | { kind: 'human' }
    | { kind: 'agent'; from: string; body: string }
    | { kind: 'thread'; ref: string; label: string; body: string }
    | { kind: 'system'; label: string; body: string }
  export function classifyUserEntry(text: string): UserDisplay
  ```
  `thread` — видимая строка (не шум); `system` — шум.

- [ ] **Step 1: Write the failing tests** — дописать в `chatDisplay.test.ts`:

```ts
  it('rocket injects are system rows labeled by their tag', () => {
    expect(classifyUserEntry('[rocket heartbeat] worker idle 6m')).toEqual({
      kind: 'system',
      label: 'rocket heartbeat',
      body: 'worker idle 6m',
    })
    expect(classifyUserEntry('[rocket] delivery FAILED to w1').kind).toBe('system')
  })

  it('SYSTEM NOTIFICATION markers are system wherever they appear', () => {
    expect(classifyUserEntry('pre [SYSTEM NOTIFICATION - NOT USER INPUT] body').kind).toBe('system')
  })

  it('thread frames become visible thread rows', () => {
    expect(classifyUserEntry('[#4543/Q2 answer from human] go')).toEqual({
      kind: 'thread',
      ref: '#4543/Q2',
      label: 'answer from human',
      body: 'go',
    })
    expect(classifyUserEntry('[cto/Q1 reply from w1] **done**\nPR #3')).toEqual({
      kind: 'thread',
      ref: 'cto/Q1',
      label: 'reply from w1',
      body: '**done**\nPR #3',
    })
  })

  it('a human message starting with an unrelated bracket stays human', () => {
    expect(classifyUserEntry('[draft] my notes')).toEqual({ kind: 'human' })
  })
```

И в `mobile/src/lib/chatRows.test.ts`:

```ts
  it('thread frames are not noise, rocket injects are', () => {
    expect(isNoise({ role: 'user', text: '[#1/Q1 reply from cto] ok', ts: 1 })).toBe(false)
    expect(isNoise({ role: 'user', text: '[rocket heartbeat] idle', ts: 1 })).toBe(true)
  })
```

(импорт `isNoise` из `./chatRows`, если его ещё нет в файле).

- [ ] **Step 2: Run** `cd mobile && npx jest src/lib/chatDisplay.test.ts src/lib/chatRows.test.ts` — Expected: FAIL.

- [ ] **Step 3: Implement** — `chatDisplay.ts` целиком:

```ts
// The agent transcript stores every injected message with role="user":
// real human replies, inter-agent mail ("[from worker] …"), Q&A thread
// deliveries ("[#1023/Q2 reply from cto] …"), daemon notices ("[rocket …]")
// and harness noise (<task-notification>, <system-reminder>…). The daemon
// already unwraps the socket envelope (docs/13-chat.md), so the prefix is
// always at the start. This classifier decides how the chat renders each.

export type UserDisplay =
  | { kind: 'human' }
  /** Message injected from another session (worker/daemon) — shown as a left bubble. */
  | { kind: 'agent'; from: string; body: string }
  /** A Q&A thread entry delivered into the session — a visible compact row. */
  | { kind: 'thread'; ref: string; label: string; body: string }
  /** Harness/system injection — collapsed into a dim expandable row, hidden as noise. */
  | { kind: 'system'; label: string; body: string }

/** internal/api/threads.go threadPrefix: `[#1023/Q2 reply from cto]`, `[cto/Q1 answer from human]`. */
const THREAD_FRAME_RE = /^\[(#?[\w.-]+\/Q\d+) ([a-z-]+ from [^\]]+)\]\s*([\s\S]*)$/

export function classifyUserEntry(text: string): UserDisplay {
  const t = text.trimStart()

  const from = t.match(/^\[from ([^\]]+)\]\s*([\s\S]*)$/)
  if (from) return { kind: 'agent', from: from[1], body: from[2] }

  const frame = t.match(THREAD_FRAME_RE)
  if (frame) return { kind: 'thread', ref: frame[1], label: frame[2], body: frame[3] }

  const qm = t.match(/^\[task #(\d+) QM (reply|answer)\]\s*([\s\S]*)$/)
  if (qm) return { kind: 'system', label: `Q&A · task #${qm[1]} ${qm[2]}`, body: qm[3] }

  const rocket = t.match(/^\[(rocket[^\]]*)\]\s*([\s\S]*)$/)
  if (rocket) return { kind: 'system', label: rocket[1], body: rocket[2] }

  if (t.includes('[SYSTEM NOTIFICATION - NOT USER INPUT]')) {
    return { kind: 'system', label: 'system notification', body: t }
  }
  if (t.startsWith('[heartbeat')) return { kind: 'system', label: 'heartbeat', body: t }
  if (t.startsWith('[large message]')) return { kind: 'system', label: 'large message pointer', body: t }

  // XML-ish harness wrappers: <task-notification>, <system-reminder>,
  // <command-name>, <local-command-stdout>, …
  const tag = t.match(/^<([a-z][a-z0-9_-]*)[\s>]/i)
  if (tag) return { kind: 'system', label: tag[1], body: t }

  if (t.startsWith('Caveat:')) return { kind: 'system', label: 'caveat', body: t }

  return { kind: 'human' }
}
```

`isNoise` в `chatRows.ts` уже проверяет `kind === 'system'` — менять не нужно; тест подтверждает.

- [ ] **Step 4: Run** те же тесты — Expected: PASS (включая старые).

- [ ] **Step 5: Commit**

```bash
git add mobile/src/lib/chatDisplay.ts mobile/src/lib/chatDisplay.test.ts mobile/src/lib/chatRows.test.ts
git commit -m "mobile: классификатор чата знает [rocket …], кадры тредов и SYSTEM NOTIFICATION"
```

---

### Task 5: Мобилка — строки ленты (подтверждение, large message, квизы, группы инструментов)

**Files:**
- Create: `mobile/src/lib/toolDigest.ts`, `mobile/src/lib/toolDigest.test.ts`
- Modify: `mobile/src/lib/chatRows.ts`
- Test: `mobile/src/lib/chatRows.test.ts`

**Interfaces:**
- Produces:
  ```ts
  // toolDigest.ts
  export interface ToolDigest { command?: string; fileName?: string; filePath?: string; description?: string; raw?: string }
  export function summarizeToolEntry(toolName: string, text: string): ToolDigest
  export function toolLine(d: ToolDigest): string   // одна строка для рендера
  // chatRows.ts
  export type ChatRow =
    | { kind: 'entry'; key: string; entry: ChatEntry }
    | { kind: 'tools'; key: string; entries: ChatEntry[] }          // 2+ подряд
    | { kind: 'outgoing'; key: string; body: string; status: string; reason?: string }
  ```
  `buildChatRows` — прежняя сигнатура.

- [ ] **Step 1: Port toolDigest** — скопировать `web/src/lib/toolDigest.ts` в `mobile/src/lib/toolDigest.ts` **без** секции «Grouping consecutive tool entries» (группировка живёт в chatRows), и добавить:

```ts
/** One display line for a tool row: `git status — list changes`, `chat.go`, or the raw digest. */
export function toolLine(d: ToolDigest): string {
  const main = d.command ?? d.fileName ?? d.raw ?? ''
  return d.description && (d.command || d.fileName) ? `${main} — ${d.description}` : main
}
```

Тест `toolDigest.test.ts`:

```ts
import { summarizeToolEntry, toolLine } from './toolDigest'

describe('summarizeToolEntry', () => {
  it('pulls command and description out of full JSON', () => {
    const d = summarizeToolEntry('Bash', '{"command":"git status","description":"list changes"}')
    expect(toolLine(d)).toBe('git status — list changes')
  })
  it('survives a truncated digest', () => {
    const d = summarizeToolEntry('Bash', '{"command":"go test ./internal/…')
    expect(d.command).toBe('go test ./internal/…')
  })
  it('shows a file basename for file tools', () => {
    expect(toolLine(summarizeToolEntry('Edit', '{"file_path":"/a/b/chat.go","old_string":"x"}'))).toBe('chat.go')
  })
  it('falls back to raw text', () => {
    expect(toolLine(summarizeToolEntry('Grep', '{"pattern":"foo"}'))).toBe('{"pattern":"foo"}')
  })
})
```

- [ ] **Step 2: Write the failing chatRows tests** — дописать в `chatRows.test.ts`:

```ts
const u = (text: string, ts: number) => ({ role: 'user' as const, text, ts })
const tool = (name: string, ts: number) => ({ role: 'tool' as const, tool_name: name, text: '{"command":"ls"}', ts })

describe('buildChatRows v2', () => {
  it('confirms two identical sends against two transcript entries', () => {
    const rows = buildChatRows({
      entries: [u('ok', 100), u('ok', 101)],
      outgoing: [
        { msgId: 1, body: 'ok', sentAt: 100 },
        { msgId: 2, body: 'ok', sentAt: 101 },
      ],
      showNoise: false,
    })
    expect(rows.filter((r) => r.kind === 'outgoing')).toHaveLength(0)
    expect(rows).toHaveLength(2)
  })

  it('a [large message] pointer after the send claims it and renders the sent text in place', () => {
    const rows = buildChatRows({
      entries: [u('before', 90), u('[large message] Full text written to /x.md', 130), { role: 'assistant', text: 'got it', ts: 140 }],
      outgoing: [{ msgId: 7, body: 'very long text', sentAt: 120 }],
      showNoise: false,
    })
    expect(rows.map((r) => (r.kind === 'entry' ? r.entry.text : r.kind))).toEqual(['before', 'very long text', 'got it'])
  })

  it('drops the AskUserQuestion ask half when its answer follows', () => {
    const rows = buildChatRows({
      entries: [
        { role: 'tool', tool_name: 'AskUserQuestion', text: '{}', ts: 1, quiz: { questions: [] } },
        { role: 'quiz_answer', text: 'A', ts: 2 },
      ],
      outgoing: [],
      showNoise: true,
    })
    expect(rows.map((r) => r.kind === 'entry' && r.entry.role)).toEqual(['quiz_answer'])
  })

  it('groups 2+ adjacent tool entries when noise is shown, keeps a single one inline', () => {
    const rows = buildChatRows({
      entries: [tool('Bash', 1), tool('Read', 2), { role: 'assistant', text: 'x', ts: 3 }, tool('Bash', 4)],
      outgoing: [],
      showNoise: true,
    })
    expect(rows.map((r) => r.kind)).toEqual(['tools', 'entry', 'entry'])
    expect(rows[0].kind === 'tools' && rows[0].entries).toHaveLength(2)
  })
})
```

- [ ] **Step 3: Run** `cd mobile && npx jest src/lib/chatRows.test.ts src/lib/toolDigest.test.ts` — Expected: FAIL (large message, quiz, tools).

- [ ] **Step 4: Implement** — `buildChatRows` в `chatRows.ts`:

```ts
export type ChatRow =
  | { kind: 'entry'; key: string; entry: ChatEntry }
  /** Two or more adjacent tool calls, collapsed into one expandable row. */
  | { kind: 'tools'; key: string; entries: ChatEntry[] }
  | { kind: 'outgoing'; key: string; body: string; status: string; reason?: string }

const isQuizAsk = (e: ChatEntry) => e.role === 'tool' && e.tool_name === 'AskUserQuestion' && e.quiz !== undefined

export function buildChatRows(params: {
  entries: ChatEntry[]
  outgoing: OutgoingMsg[]
  queueMessages?: Message[]
  showNoise: boolean
}): ChatRow[] {
  const { entries, outgoing, queueMessages, showNoise } = params

  // Transcript timestamps come from the agent's log and can lag a second
  // behind our own clock, so allow a small window when matching.
  const SKEW = 5
  const claimed = new Set<number>()
  // A large send reaches the transcript as a `[large message]` pointer, never
  // as its text (docs/13-chat.md): the pointer's position is where the send
  // landed, so it is rendered there with the text we sent.
  const replaced = new Map<number, string>()
  const pendingOut: OutgoingMsg[] = []

  for (const o of outgoing) {
    const fresh = (e: ChatEntry, i: number) =>
      !claimed.has(i) && e.role === 'user' && (e.ts === 0 || e.ts >= o.sentAt - SKEW)
    let idx = entries.findIndex((e, i) => fresh(e, i) && e.text === o.body)
    if (idx === -1) {
      idx = entries.findIndex((e, i) => fresh(e, i) && e.text.startsWith('[large message]'))
      if (idx !== -1) replaced.set(idx, o.body)
    }
    if (idx !== -1) claimed.add(idx)
    else pendingOut.push(o)
  }

  const rows: ChatRow[] = []
  let toolRun: { i: number; e: ChatEntry }[] = []
  const flushTools = () => {
    if (toolRun.length === 1) rows.push({ kind: 'entry', key: `e${toolRun[0].i}`, entry: toolRun[0].e })
    else if (toolRun.length > 1) rows.push({ kind: 'tools', key: `t${toolRun[0].i}`, entries: toolRun.map((x) => x.e) })
    toolRun = []
  }

  entries.forEach((e, i) => {
    if (isQuizAsk(e) && entries[i + 1]?.role === 'quiz_answer') return
    const text = replaced.get(i)
    const entry = text !== undefined ? { ...e, text } : e
    if (text === undefined && !showNoise && isNoise(entry)) return
    if (entry.role === 'tool') {
      toolRun.push({ i, e: entry })
      return
    }
    flushTools()
    rows.push({ kind: 'entry', key: `e${i}`, entry })
  })
  flushTools()

  for (const o of pendingOut) {
    const m = queueMessages?.find((qm) => qm.id === o.msgId)
    rows.push({ kind: 'outgoing', key: `o${o.msgId}`, body: o.body, status: m?.status ?? 'queued', reason: m?.reason })
  }
  return rows
}
```

Обновить JSDoc над функцией: добавить абзацы про `[large message]`, квиз-пары и группы.

- [ ] **Step 5: Run** `cd mobile && npx jest src/lib/` — Expected: PASS (старые тесты `chatRows.test.ts` тоже).

- [ ] **Step 6: Commit**

```bash
git add mobile/src/lib/toolDigest.ts mobile/src/lib/toolDigest.test.ts mobile/src/lib/chatRows.ts mobile/src/lib/chatRows.test.ts
git commit -m "mobile: лента чата — large message на своём месте, квиз-пары, группы вызовов инструментов"
```

---

### Task 6: Мобилка — экран чата

**Files:**
- Modify: `mobile/app/chat/[id].tsx`
- Test: `mobile/__tests__/chat/chat-agent.test.tsx`

**Interfaces:**
- Consumes: `ChatRow` (`tools`), `classifyUserEntry` (`thread`), `summarizeToolEntry`, `toolLine`.

- [ ] **Step 1: Write the failing test** — открыть `__tests__/chat/chat-agent.test.tsx`, переиспользовать его `mockApi`/`renderWithProviders` и добавить тест: ответ `GET /v1/sessions/<id>/chat?limit=300` содержит

```ts
entries: [
  { role: 'user', text: '[#12/Q1 answer from human] go', ts: 1785622879 },
  { role: 'user', text: '**bold** from human', ts: 1785622880 },
  { role: 'tool', tool_name: 'Bash', text: '{"command":"git status","description":"list changes"}', ts: 1785622881 },
]
```

Ожидания: виден текст `go` и `#12/Q1`; текст `bold` виден, а строка `**bold** from human` — нет (markdown); после нажатия на `⚙ 1` виден `git status — list changes`, а `{"command"` не виден.

Ещё один тест: `POST /v1/messages` отвечает `{ id: 5, body: 'hello (1 attachment)', to: 'x', status: 'queued', attempts: 0, created_at: 1 }`; после отправки `hello` и следующего опроса с записью `{role:'user', text:'hello (1 attachment)', ts: now}` в ленте ровно одно вхождение `hello (1 attachment)` и нет `⏳ sending…`.

- [ ] **Step 2: Run** `cd mobile && npx jest __tests__/chat` — Expected: FAIL.

- [ ] **Step 3: Implement** в `app/chat/[id].tsx`:

1. Импорт `summarizeToolEntry, toolLine` из `../../src/lib/toolDigest`.
2. Tool-строка в `EntryBubble`:
   ```tsx
   {toolLine(summarizeToolEntry(entry.tool_name ?? '', entry.text))}
   ```
   вместо `{entry.text}`.
3. Новый компонент для группы:
   ```tsx
   function ToolGroupRow({ entries }: { entries: ChatEntry[] }) {
     const [open, setOpen] = useState(false)
     return (
       <Pressable style={styles.toolRow} onPress={() => setOpen((v) => !v)}>
         <Text style={[styles.toolText, { color: colors.textMid, fontWeight: '600' }]}>
           {open ? '▾' : '▸'} {entries.length} tool calls
         </Text>
         {open
           ? entries.map((e, i) => (
               <Text key={i} style={styles.toolText} numberOfLines={1}>
                 <Text style={{ color: colors.textMid, fontWeight: '600' }}>{e.tool_name ?? 'tool'} </Text>
                 {toolLine(summarizeToolEntry(e.tool_name ?? '', e.text))}
               </Text>
             ))
           : null}
       </Pressable>
     )
   }
   ```
   В рендере строк: `item.kind === 'tools' ? <ToolGroupRow key={item.key} entries={item.entries} /> : …`.
4. Строка треда в `EntryBubble` (после `system`):
   ```tsx
   if (d.kind === 'thread') {
     return (
       <View style={{ alignItems: 'flex-start', marginVertical: 3 }}>
         <View style={[styles.bubble, { backgroundColor: colors.cardAlt, borderColor: colors.border }]}>
           <MonoText style={{ fontSize: 11, fontWeight: '600', color: colors.textDim, marginBottom: 3 }}>
             Q&A {d.ref} · {d.label}
           </MonoText>
           <Markdown>{d.body}</Markdown>
           {entry.ts > 0 ? <Text style={styles.bubbleMeta}>{ago(entry.ts)}</Text> : null}
         </View>
       </View>
     )
   }
   ```
5. Пузырь человека: `<Markdown>{entry.text}</Markdown>` вместо `<Text style={styles.bubbleText}>{entry.text}</Text>`.
6. `submit`: `setOutgoing((prev) => [...prev, { msgId: m.id, body: m.body ?? body, sentAt: … }])` + комментарий: «`m.body` — сохранённое тело (ссылки на вложения переписаны); транскрипт содержит именно его».

- [ ] **Step 4: Run** `cd mobile && npx jest __tests__/chat src/lib && npx tsc --noEmit` — Expected: PASS, без ошибок типов.

- [ ] **Step 5: Commit**

```bash
git add mobile/app/chat/[id].tsx mobile/__tests__/chat/chat-agent.test.tsx
git commit -m "mobile: чат — строки Q&A, читаемые вызовы инструментов, markdown у человека, подтверждение по сохранённому телу"
```

---

### Task 7: Мобилка — данные и модель вопросов, отложенная очередь

**Files:**
- Modify: `mobile/src/api/types.ts`, `mobile/src/api/queries.ts`, `mobile/src/api/events.ts`
- Create: `mobile/src/lib/questions.ts`, `mobile/src/lib/questions.test.ts`, `mobile/src/lib/deferred.ts`, `mobile/src/lib/deferred.test.ts`
- Test: `mobile/src/api/events.test.ts`

**Interfaces:**
- Produces:
  ```ts
  // types.ts
  export interface ThreadInboxEntry { local_ref: string; kind: 'task' | 'role'; task_id?: number; role_id?: string;
    subject: string; id: number; ordinal: number; asked_by: string; title?: string; body: string;
    status: QuestionStatus; resolution?: 'answered' | 'dismissed' | 'fyi'; type: 'decision' | 'fyi';
    options?: string[]; participants: string[]; attention: string[]; waiting_on: string[]; your_turn: boolean;
    asked_at: number; updated_at: number; resolved_at?: number; stale?: boolean; project_id?: string; task_title?: string }
  // + `title?: string` в Question и AgentQuestion
  // queries.ts
  export function useThreads(): UseQueryResult<ThreadInboxEntry[]>          // key [baseUrl, 'threads']
  export type ThreadAnswer = { choose: number } | { body: string } | { dismiss: true }
  export function useThreadAnswer(): UseMutationResult<unknown, Error, { thread: Pick<ThreadInboxEntry, 'id' | 'kind'>; answer: ThreadAnswer }>
  // questions.ts
  export function waitingOnYou(threads: ThreadInboxEntry[]): ThreadInboxEntry[]
  export function otherOpen(threads: ThreadInboxEntry[]): ThreadInboxEntry[]
  export function threadSource(t: ThreadInboxEntry): { label: string; href: string }
  export function questionTitle(q: { title?: string; body: string }): string
  // deferred.ts
  export interface DeferredQueue { schedule(run: () => void): void; cancel(): boolean; flush(): void; isPending(): boolean; dispose(): void }
  export function createDeferredQueue(delayMs: number): DeferredQueue
  ```

- [ ] **Step 1: Write the failing tests**

`questions.test.ts`:

```ts
import type { ThreadInboxEntry } from '../api/types'
import { otherOpen, questionTitle, threadSource, waitingOnYou } from './questions'

const t = (p: Partial<ThreadInboxEntry>): ThreadInboxEntry => ({
  local_ref: '1/Q1', kind: 'task', task_id: 1, subject: 'task #1', id: 1, ordinal: 1, asked_by: 'orch',
  body: 'b', status: 'open', type: 'decision', participants: [], attention: [], waiting_on: [],
  your_turn: false, asked_at: 0, updated_at: 0, ...p,
})

describe('waitingOnYou', () => {
  it('keeps open threads on you, stale first, then oldest movement first', () => {
    const list = [
      t({ id: 1, your_turn: true, updated_at: 30 }),
      t({ id: 2, your_turn: true, updated_at: 10 }),
      t({ id: 3, your_turn: true, updated_at: 50, stale: true }),
      t({ id: 4, your_turn: false }),
      t({ id: 5, your_turn: true, status: 'resolved' }),
    ]
    expect(waitingOnYou(list).map((x) => x.id)).toEqual([3, 2, 1])
  })
})

describe('otherOpen', () => {
  it('is open threads not on you, fyi excluded, newest movement first', () => {
    const list = [t({ id: 1, updated_at: 5 }), t({ id: 2, updated_at: 9 }), t({ id: 3, your_turn: true }), t({ id: 4, type: 'fyi' })]
    expect(otherOpen(list).map((x) => x.id)).toEqual([2, 1])
  })
})

describe('threadSource', () => {
  it('labels a task thread with its number and title and links to the task', () => {
    expect(threadSource(t({ task_id: 1023, task_title: 'Ship it' }))).toEqual({ label: '#1023 · Ship it', href: '/task/1023' })
  })
  it('labels a role thread with the agent and links to the agent', () => {
    expect(threadSource(t({ kind: 'role', task_id: undefined, role_id: 'cto' }))).toEqual({ label: 'agent cto', href: '/agent/cto' })
  })
})

describe('questionTitle', () => {
  it('prefers the title, falls back to the first non-empty body line cut at 80 chars on a word', () => {
    expect(questionTitle({ title: ' Pick a DB ', body: 'x' })).toBe('Pick a DB')
    expect(questionTitle({ body: '\n\nfirst line\nsecond' })).toBe('first line')
    expect(questionTitle({ body: 'word '.repeat(30) }).endsWith('…')).toBe(true)
  })
})
```

`deferred.test.ts`:

```ts
import { createDeferredQueue } from './deferred'

beforeEach(() => jest.useFakeTimers())
afterEach(() => jest.useRealTimers())

it('runs after the delay', () => {
  const run = jest.fn()
  const q = createDeferredQueue(5000)
  q.schedule(run)
  jest.advanceTimersByTime(4999)
  expect(run).not.toHaveBeenCalled()
  jest.advanceTimersByTime(1)
  expect(run).toHaveBeenCalledTimes(1)
})

it('cancel drops the pending action', () => {
  const run = jest.fn()
  const q = createDeferredQueue(5000)
  q.schedule(run)
  expect(q.cancel()).toBe(true)
  jest.advanceTimersByTime(10000)
  expect(run).not.toHaveBeenCalled()
})

it('scheduling a second action commits the first immediately', () => {
  const first = jest.fn()
  const second = jest.fn()
  const q = createDeferredQueue(5000)
  q.schedule(first)
  q.schedule(second)
  expect(first).toHaveBeenCalledTimes(1)
  expect(second).not.toHaveBeenCalled()
  expect(q.isPending()).toBe(true)
})

it('dispose commits rather than drops', () => {
  const run = jest.fn()
  const q = createDeferredQueue(5000)
  q.schedule(run)
  q.dispose()
  expect(run).toHaveBeenCalledTimes(1)
})
```

В `events.test.ts` дописать:

```ts
it('question events refresh the threads inbox', () => {
  expect(parseEventType('task.question_asked')).toContain('threads')
  expect(parseEventType('agent.question_resolved')).toContain('threads')
})
```

- [ ] **Step 2: Run** `cd mobile && npx jest src/lib/questions.test.ts src/lib/deferred.test.ts src/api/events.test.ts` — Expected: FAIL.

- [ ] **Step 3: Implement**

- `types.ts`: добавить `ThreadInboxEntry` (поля — как в Interfaces, с комментариями из `web/src/lib/types.ts:450-486`) и `/** One-line heading; absent on a daemon older than task #1264. */ title?: string` в `Question` и `AgentQuestion`.
- `deferred.ts`: скопировать `web/src/screens/questions/deferred.ts` целиком (модуль чистый, без DOM); в шапочном комментарии заменить «pagehide, a hidden tab» на «the app going to background».
- `questions.ts`:

```ts
// Pure derivations behind the Questions tab — a function of GET /v1/threads,
// no React, so ordering can be tested directly. Same rules as the web
// dashboard's Decide queue (web/src/screens/questions/model.ts).
import type { ThreadInboxEntry } from '../api/types'

const TITLE_MAX = 80

/** Open decision threads waiting on you: stale first, then longest without movement first. */
export function waitingOnYou(threads: ThreadInboxEntry[]): ThreadInboxEntry[] {
  return threads
    .filter((t) => t.status === 'open' && t.your_turn)
    .sort((a, b) => Number(b.stale ?? false) - Number(a.stale ?? false) || a.updated_at - b.updated_at)
}

/** Other open decision threads — waiting on someone else; newest movement first. */
export function otherOpen(threads: ThreadInboxEntry[]): ThreadInboxEntry[] {
  return threads
    .filter((t) => t.status === 'open' && !t.your_turn && t.type !== 'fyi')
    .sort((a, b) => b.updated_at - a.updated_at)
}

/** Where a thread lives: the line above the card and the screen it opens. */
export function threadSource(t: ThreadInboxEntry): { label: string; href: string } {
  if (t.kind === 'role' && t.role_id) return { label: `agent ${t.role_id}`, href: `/agent/${t.role_id}` }
  const title = t.task_title ? ` · ${t.task_title}` : ''
  return { label: `#${t.task_id}${title}`, href: `/task/${t.task_id}` }
}

/** The heading: `title`, else the first non-empty body line cut on a word at 80 chars. */
export function questionTitle(q: { title?: string; body: string }): string {
  const title = q.title?.trim()
  if (title) return title
  const line = q.body.split('\n').find((l) => l.trim() !== '')?.trim() ?? ''
  if (line.length <= TITLE_MAX) return line
  const cut = line.slice(0, TITLE_MAX)
  const space = cut.lastIndexOf(' ')
  return `${(space > 0 ? cut.slice(0, space) : cut).trimEnd()}…`
}
```

- `queries.ts`:

```ts
export function useThreads() {
  const baseUrl = useBaseUrl()
  const refetchInterval = usePoll(10000)
  return useQuery({
    queryKey: [baseUrl, 'threads'],
    queryFn: async () => (await api.get<{ threads: ThreadInboxEntry[] }>(baseUrl, '/v1/threads')).threads ?? [],
    refetchInterval,
  })
}

export type ThreadAnswer = { choose: number } | { body: string } | { dismiss: true }

/** Answers any inbox thread; the endpoint depends on whether it is a task or an agent thread. */
export function useThreadAnswer() {
  const baseUrl = useBaseUrl()
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (p: { thread: Pick<ThreadInboxEntry, 'id' | 'kind'>; answer: ThreadAnswer }) =>
      api.post(
        baseUrl,
        p.thread.kind === 'role' ? `/v1/agent-questions/${p.thread.id}/answer` : `/v1/questions/${p.thread.id}/answer`,
        p.answer,
      ),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: [baseUrl, 'threads'] })
      qc.invalidateQueries({ queryKey: [baseUrl, 'task'] })
      qc.invalidateQueries({ queryKey: [baseUrl, 'agent'] })
      qc.invalidateQueries({ queryKey: [baseUrl, 'agents'] })
    },
  })
}
```

Сверить, как остальные хуки в `queries.ts` пользуются `usePoll` (сигнатура `usePoll(fast, slow?)`), и сделать так же. В существующих мутациях `useQuestionAnswer`, `useQuestionDismiss`, `useQuestionReply`, `useAgentQuestion*` добавить в `onSuccess` `qc.invalidateQueries({ queryKey: [baseUrl, 'threads'] })`.

- `events.ts`: в `case 'task'` и `case 'agent'` добавить `'threads'` в возвращаемый массив.

- [ ] **Step 4: Run** `cd mobile && npx jest src && npx tsc --noEmit` — Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add mobile/src/api mobile/src/lib/questions.ts mobile/src/lib/questions.test.ts mobile/src/lib/deferred.ts mobile/src/lib/deferred.test.ts
git commit -m "mobile: инбокс тредов (GET /v1/threads), модель вкладки вопросов, отложенная очередь ответов"
```

---

### Task 8: Мобилка — заголовок и markdown у вопросов в задаче и агенте

**Files:**
- Create: `mobile/src/components/QuestionText.tsx`
- Modify: `mobile/app/task/[id].tsx:143`, `mobile/app/agent/[id].tsx:135`
- Test: `mobile/__tests__/task/task-thread.test.tsx`

**Interfaces:**
- Consumes: `questionTitle` (Task 7), `Markdown`.
- Produces: `export function QuestionText({ q }: { q: { title?: string; body: string } })`.

- [ ] **Step 1: Write the failing test** — в `task-thread.test.tsx` добавить тест с `mockApi({ '/v1/tasks/12/questions': { questions: [{ ...THREAD, title: 'Which repo?', body: 'Pick **one**:\n- a\n- b' }] } })`: ожидается `getByText('Which repo?')`, `getByText('one')`, и `queryByText(/\*\*one\*\*/)` → `null`.

- [ ] **Step 2: Run** `cd mobile && npx jest __tests__/task` — Expected: FAIL.

- [ ] **Step 3: Implement**

```tsx
import { StyleSheet, Text, View } from 'react-native'
import { questionTitle } from '../lib/questions'
import { Markdown } from './Markdown'

/**
 * A question's heading and markdown body (task #1264: the body is markdown
 * with the context folded in). The body repeats the heading when the daemon
 * derived no title, so it is shown only when it adds something.
 */
export function QuestionText({ q }: { q: { title?: string; body: string } }) {
  const title = questionTitle(q)
  const bodyAddsSomething = q.body.trim() !== title
  return (
    <View style={{ marginBottom: 14 }}>
      <Text style={styles.title}>{title}</Text>
      {bodyAddsSomething ? <Markdown>{q.body}</Markdown> : null}
    </View>
  )
}

const styles = StyleSheet.create({
  title: { fontSize: 17, lineHeight: 24, fontWeight: '700', letterSpacing: -0.2, marginBottom: 6 },
})
```

В обоих экранах заменить `<Text style={styles.qText}>{q.body}</Text>` на `<QuestionText q={q} />`; удалить стиль `qText`, если он больше нигде не используется (строки 535/646 — проверить через grep).

- [ ] **Step 4: Run** `cd mobile && npx jest __tests__/task __tests__/agent && npx tsc --noEmit` — Expected: PASS (старый тест `getByText('which repo?')` тоже проходит: заголовок = первая строка тела).

- [ ] **Step 5: Commit**

```bash
git add mobile/src/components/QuestionText.tsx mobile/app/task/[id].tsx mobile/app/agent/[id].tsx mobile/__tests__/task/task-thread.test.tsx
git commit -m "mobile: вопросы в задаче и агенте — заголовок и markdown вместо сырого текста"
```

---

### Task 9: Мобилка — вкладка «Questions»

**Files:**
- Create: `mobile/src/components/ThreadCard.tsx`, `mobile/app/(tabs)/questions.tsx`
- Move: `mobile/app/(tabs)/system.tsx` → `mobile/app/system.tsx`
- Modify: `mobile/app/(tabs)/_layout.tsx`, `mobile/app/(tabs)/settings.tsx`
- Test: `mobile/__tests__/questions/questions.test.tsx` (new)

**Interfaces:**
- Consumes: `useThreads`, `useThreadAnswer`, `ThreadAnswer`, `waitingOnYou`, `otherOpen`, `threadSource`, `createDeferredQueue`, `QuestionText`, `useToast`.
- Produces: `ThreadCard` props — `{ thread: ThreadInboxEntry; onAnswer: (thread: ThreadInboxEntry, answer: ThreadAnswer, label: string) => void }`.

- [ ] **Step 1: Write the failing screen test** — `__tests__/questions/questions.test.tsx`, провайдеры и `mockApi` по образцу `__tests__/task/task-thread.test.tsx`, плюс `jest.useFakeTimers()`. `mockApi` отвечает на `GET /v1/threads`:

```ts
const THREADS = { threads: [
  { local_ref: '1023/Q2', kind: 'task', task_id: 1023, task_title: 'Ship it', subject: 'task #1023', id: 7, ordinal: 2,
    asked_by: 'orch', title: 'Which DB?', body: 'Pick **one**', status: 'open', type: 'decision', options: ['Postgres', 'SQLite'],
    participants: ['human', 'orch'], attention: ['human'], waiting_on: ['human'], your_turn: true, asked_at: 1, updated_at: 1 },
  { local_ref: 'cto/Q1', kind: 'role', role_id: 'cto', subject: 'role cto', id: 9, ordinal: 1, asked_by: 'cto',
    title: 'Other', body: 'x', status: 'open', type: 'decision', participants: ['cto', 'w1'], attention: ['w1'],
    waiting_on: ['w1'], your_turn: false, asked_at: 1, updated_at: 2 },
] }
```

и записывает POST-запросы (метод, путь, тело) в массив `posts`. Тесты:

1. Виден `Which DB?`, `#1023 · Ship it`, кнопки `Postgres`/`SQLite`; `Other` скрыт до нажатия `Other open (1)`.
2. Нажать `Postgres` → карточка заменяется плашкой `Answered: Postgres` с `Undo`; `posts` пуст; `jest.advanceTimersByTime(5000)` → `posts` = `[{ path: '/v1/questions/7/answer', body: { choose: 1 } }]`.
3. Нажать `Postgres`, затем `Undo` → через 5000 мс `posts` пуст, карточка снова видна.
4. Нажать `Not relevant` → через 5000 мс POST `{ dismiss: true }`.
5. Ввести текст `use sqlite` и нажать `Send` → через 5000 мс POST `{ body: 'use sqlite' }`.
6. POST отвечает `409` с `{ error: { code: 'question_resolved', message: 'already resolved' } }` → после 5000 мс карточка `Which DB?` снова видна и показан toast с `already resolved`.
7. Размонтирование экрана при висящей плашке (`unmount()`) → POST уходит сразу.
8. Для треда `kind: 'role'` с `your_turn: true` — POST уходит на `/v1/agent-questions/<id>/answer`.

- [ ] **Step 2: Run** `cd mobile && npx jest __tests__/questions` — Expected: FAIL (экрана нет).

- [ ] **Step 3: Implement `ThreadCard.tsx`**

```tsx
import { router } from 'expo-router'
import { useState } from 'react'
import { Pressable, StyleSheet, Text, TextInput, View } from 'react-native'
import type { ThreadAnswer } from '../api/queries'
import type { ThreadInboxEntry } from '../api/types'
import { threadSource } from '../lib/questions'
import { participantLabel } from '../lib/threads'
import { colors, radius } from '../theme'
import { QuestionText } from './QuestionText'
import { Badge, Card, MonoText } from './ui'

/**
 * One inbox thread: where it lives, the question, one-tap options, a free
 * answer and "Not relevant". Answers are not sent here — `onAnswer` hands
 * them to the screen, which holds them in the undo window.
 */
export function ThreadCard({
  thread,
  onAnswer,
}: {
  thread: ThreadInboxEntry
  onAnswer: (thread: ThreadInboxEntry, answer: ThreadAnswer, label: string) => void
}) {
  const [text, setText] = useState('')
  const source = threadSource(thread)
  const options = thread.options ?? []
  const actionable = thread.your_turn

  return (
    <Card style={{ padding: 14, marginBottom: 12 }}>
      <Pressable onPress={() => router.navigate(source.href as never)} style={styles.sourceRow}>
        <MonoText style={styles.source} numberOfLines={1}>{source.label}</MonoText>
        <MonoText style={styles.ref}>{thread.local_ref}</MonoText>
      </Pressable>
      <View style={styles.metaRow}>
        <Text style={styles.meta}>{participantLabel(thread.asked_by)} asked</Text>
        {thread.stale ? <Badge label="stale" fg={colors.amberDeep} bg={colors.amberBg} /> : null}
        {!actionable ? (
          <Text style={styles.meta}>· waiting for {thread.waiting_on.map(participantLabel).join(', ')}</Text>
        ) : null}
      </View>
      <QuestionText q={thread} />
      {actionable ? (
        <>
          {options.length > 0 ? (
            <View style={styles.optionRow}>
              {options.map((label, i) => (
                <Pressable key={label} style={styles.optionBtn} onPress={() => onAnswer(thread, { choose: i + 1 }, label)}>
                  <Text style={styles.optionText}>{label}</Text>
                </Pressable>
              ))}
            </View>
          ) : null}
          <TextInput
            value={text}
            onChangeText={setText}
            placeholder="Your answer…"
            placeholderTextColor={colors.textFaint}
            multiline
            style={styles.input}
          />
          <View style={styles.actions}>
            <Pressable onPress={() => onAnswer(thread, { dismiss: true }, 'not relevant')}>
              <Text style={styles.dismiss}>Not relevant</Text>
            </Pressable>
            <View style={{ flex: 1 }} />
            <Pressable
              disabled={!text.trim()}
              style={[styles.sendBtn, !text.trim() && { opacity: 0.4 }]}
              onPress={() => onAnswer(thread, { body: text.trim() }, text.trim())}
            >
              <Text style={styles.sendText}>Send</Text>
            </Pressable>
          </View>
        </>
      ) : null}
    </Card>
  )
}
```

Стили: `optionRow`/`optionBtn`/`optionText` скопировать из `app/task/[id].tsx` (grep `optionBtn:`), `input` — из стиля поля ответа там же; `sourceRow` — `flexDirection:'row', gap:8, marginBottom:6`; `source` — `fontSize:12, color: colors.accent, flex:1`; `ref`, `meta` — `fontSize:11.5, color: colors.textFaint`; `metaRow` — `flexDirection:'row', alignItems:'center', gap:6, marginBottom:8, flexWrap:'wrap'`; `actions` — `flexDirection:'row', alignItems:'center', marginTop:10`; `dismiss` — `fontSize:13, color: colors.textDim`; `sendBtn` — `backgroundColor: colors.ink, borderRadius: radius.md, paddingHorizontal:14, paddingVertical:8`; `sendText` — `color:'#fff', fontWeight:'600'`. Если какого-то токена (`radius.md`, `colors.ink`) нет в `src/theme.ts` — взять ближайший существующий.

- [ ] **Step 4: Implement `app/(tabs)/questions.tsx`**

```tsx
import { useEffect, useMemo, useRef, useState } from 'react'
import { AppState, Pressable, RefreshControl, ScrollView, StyleSheet, Text, View } from 'react-native'
import { SafeAreaView } from 'react-native-safe-area-context'
import { useThreadAnswer, useThreads, type ThreadAnswer } from '../../src/api/queries'
import type { ThreadInboxEntry } from '../../src/api/types'
import { ConnectionBanner } from '../../src/components/ConnectionBanner'
import { ThreadCard } from '../../src/components/ThreadCard'
import { useToast } from '../../src/components/Toast'
import { EmptyState, SectionTitle } from '../../src/components/ui'
import { createDeferredQueue } from '../../src/lib/deferred'
import { otherOpen, waitingOnYou } from '../../src/lib/questions'
import { colors } from '../../src/theme'

/** How long a tap can be taken back before the answer goes to the daemon (it has no undo). */
const UNDO_MS = 5000

export default function QuestionsScreen() {
  const threads = useThreads()
  const answer = useThreadAnswer()
  const toast = useToast()
  const [showOthers, setShowOthers] = useState(false)
  // Threads answered locally and not yet confirmed by a refetch: hidden from
  // the lists; the latest one is shown as the undo bar.
  const [hidden, setHidden] = useState<Set<number>>(new Set())
  const [undo, setUndo] = useState<{ id: number; label: string } | null>(null)
  const queue = useRef(createDeferredQueue(UNDO_MS)).current

  // Leaving is not an Undo: unmount and backgrounding commit the pending answer.
  useEffect(() => {
    const sub = AppState.addEventListener('change', (s) => {
      if (s !== 'active') queue.flush()
    })
    return () => {
      sub.remove()
      queue.dispose()
    }
  }, [queue])

  const onAnswer = (thread: ThreadInboxEntry, a: ThreadAnswer, label: string) => {
    setHidden((prev) => new Set(prev).add(thread.id))
    setUndo({ id: thread.id, label })
    queue.schedule(() => {
      setUndo((u) => (u?.id === thread.id ? null : u))
      answer.mutate(
        { thread, answer: a },
        {
          onError: (e) => {
            setHidden((prev) => {
              const next = new Set(prev)
              next.delete(thread.id)
              return next
            })
            toast.show((e as Error).message)
          },
        },
      )
    })
  }

  const onUndo = () => {
    if (!undo) return
    queue.cancel()
    setHidden((prev) => {
      const next = new Set(prev)
      next.delete(undo.id)
      return next
    })
    setUndo(null)
  }

  const all = threads.data ?? []
  const mine = useMemo(() => waitingOnYou(all).filter((t) => !hidden.has(t.id)), [all, hidden])
  const others = useMemo(() => otherOpen(all), [all])

  return (
    <SafeAreaView style={{ flex: 1, backgroundColor: colors.page }} edges={['top']}>
      <ConnectionBanner />
      <ScrollView
        contentContainerStyle={{ padding: 14, paddingBottom: 90 }}
        refreshControl={<RefreshControl refreshing={threads.isRefetching} onRefresh={() => threads.refetch()} />}
      >
        <Text style={styles.h1}>Questions</Text>
        <SectionTitle>{`Waiting on you (${mine.length})`}</SectionTitle>
        {mine.map((t) => (
          <ThreadCard key={t.id} thread={t} onAnswer={onAnswer} />
        ))}
        {threads.isSuccess && mine.length === 0 ? <EmptyState text="No questions waiting on you." /> : null}
        {others.length > 0 ? (
          <Pressable onPress={() => setShowOthers((v) => !v)} style={{ paddingVertical: 10 }}>
            <Text style={styles.othersToggle}>
              {showOthers ? '▾' : '▸'} Other open ({others.length})
            </Text>
          </Pressable>
        ) : null}
        {showOthers ? others.map((t) => <ThreadCard key={t.id} thread={t} onAnswer={onAnswer} />) : null}
      </ScrollView>
      {undo ? (
        <View style={styles.undoBar}>
          <Text style={styles.undoText} numberOfLines={1}>Answered: {undo.label}</Text>
          <Pressable onPress={onUndo} hitSlop={8}>
            <Text style={styles.undoBtn}>Undo</Text>
          </Pressable>
        </View>
      ) : null}
    </SafeAreaView>
  )
}

const styles = StyleSheet.create({
  h1: { fontSize: 22, fontWeight: '700', letterSpacing: -0.3, marginBottom: 12 },
  othersToggle: { fontSize: 13, fontWeight: '600', color: colors.textDim },
  undoBar: {
    position: 'absolute', left: 14, right: 14, bottom: 14, flexDirection: 'row', alignItems: 'center', gap: 12,
    backgroundColor: colors.text, borderRadius: 12, paddingHorizontal: 16, paddingVertical: 12,
  },
  undoText: { flex: 1, color: '#fff', fontSize: 13.5 },
  undoBtn: { color: '#9ec5ff', fontWeight: '700', fontSize: 13.5 },
})
```

Замечание по тесту 1: `Answered: Postgres` — плашка; тест 6 проверяет, что после ошибки `hidden` сбрасывается. Спрятанный тред снимается из `hidden` не нужно — после успешной мутации refetch уберёт его из `waitingOnYou`.

- [ ] **Step 5: Tabs, System, Settings**

1. `git mv mobile/app/(tabs)/system.tsx mobile/app/system.tsx`; поправить импорты `../../src/…` → `../src/…`; в шапке перед логотипом добавить `<BackButton onPress={() => router.back()} />` (импорт `BackButton` из `../src/components/ui`, `router` из `expo-router`); `edges={['top', 'bottom']}`.
2. `_layout.tsx`: удалить `SystemIcon` и `Tabs.Screen name="system"`; добавить

```tsx
/** A question: a speech bubble with a tail. */
function QuestionsIcon({ color }: { color: import('react-native').ColorValue }) {
  return (
    <View style={{ width: 20, height: 20, alignItems: 'center', justifyContent: 'center' }}>
      <View style={{ width: 17, height: 13, borderRadius: 4, borderWidth: 1.7, borderColor: color }} />
      <View style={{ width: 5, height: 5, marginTop: -2, marginLeft: -8, borderLeftWidth: 1.7, borderBottomWidth: 1.7, borderColor: color, transform: [{ skewY: '-30deg' }] }} />
    </View>
  )
}
```

и в `TabsLayout`:

```tsx
  const threads = useThreads()
  const yourTurn = countYourTurn(threads.data ?? [])
  …
      <Tabs.Screen
        name="questions"
        options={{
          title: 'Questions',
          tabBarIcon: ({ color }) => <QuestionsIcon color={color} />,
          ...(yourTurn > 0 ? { tabBarBadge: yourTurn } : {}),
        }}
      />
```

между `agents` и `settings` (`countYourTurn` — из `src/lib/threads`, уже считает `status==='open' && your_turn`; `useThreads` — из `src/api/queries`).
3. `settings.tsx`: в начале контента добавить карточку-ссылку

```tsx
<Pressable onPress={() => router.navigate('/system')} style={styles.linkRow}>
  <Text style={{ fontSize: 14.5, fontWeight: '600', color: colors.text, flex: 1 }}>System</Text>
  <Text style={{ fontSize: 13, color: colors.textFaint }}>sessions, worktrees, cleanup ›</Text>
</Pressable>
```

со стилем `linkRow: { flexDirection: 'row', alignItems: 'center', backgroundColor: colors.card, borderWidth: 1, borderColor: colors.border, borderRadius: radius.lg, padding: 14, marginBottom: 18 }` (токен радиуса — как у `Card` в `ui.tsx`).
4. `grep -rn "'/system'\|\"system\"\|(tabs)/system" mobile/app mobile/src mobile/__tests__` — поправить ссылки на старый путь вкладки, если есть.

- [ ] **Step 6: Run** `cd mobile && npx jest && npx tsc --noEmit` — Expected: PASS (флейки из базовой линии — перезапустить один раз).

- [ ] **Step 7: Commit**

```bash
git add -A mobile/app mobile/src/components/ThreadCard.tsx mobile/__tests__/questions
git commit -m "mobile: вкладка Questions — все открытые вопросы, ответ одним нажатием с отменой 5 с; System — в настройках"
```

---

### Task 10: Документация и ручная проверка

**Files:**
- Modify: `docs/11-dashboard.md` (раздел «Мобильный клиент», ~строка 232)
- Modify: `mobile/README.md` (список экранов, если он там есть)

- [ ] **Step 1: Docs** — в «Мобильный клиент» описать вкладки (Projects, Kanban, Milestones, Agents, Questions, Settings → System), вкладку Questions (`GET /v1/threads`, ответ `choose`/`body`/`dismiss`, окно отмены 5 с) и убрать утверждение про интерактивный терминал по WebSocket (в мобилке его нет).

- [ ] **Step 2: Full verification**

Run: `go test ./... && (cd web && npx vitest run) && (cd mobile && npx jest && npx tsc --noEmit)`
Expected: всё зелёное.

- [ ] **Step 3: Manual check** (навык `run` / симулятор Expo): открыть чат постоянного агента, отправить сообщение — пузырь подтверждается и встаёт на своё место, ответ агента ниже; нет тегов `cross-session-message`. Вкладка Questions: ответить кнопкой, нажать Undo, ответить снова и дождаться закрытия треда.

- [ ] **Step 4: Commit**

```bash
git add docs/11-dashboard.md mobile/README.md
git commit -m "docs: мобильный клиент — вкладка Questions, System в настройках"
```
