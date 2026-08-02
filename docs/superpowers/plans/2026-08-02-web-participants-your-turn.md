# Web: participants and your-turn Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the rocket dashboard render a question thread as a multi-participant
conversation with an explicit "who must answer next", and make it tolerant of both
`""` and `"human"` as the human's participant id.

**Architecture:** A new `web/src/lib/participants.ts` owns the single source of truth
for "is this id the human" and for turning a participant id into a display label.
Everything that today compares an author or `asked_by` to `""` routes through it.
`QuestionThreadView` grows a participants row, per-message addressee chips and an
addressee picker in the composer; the picker's selection flows out through the
existing `onClarify`/`onAnswer` callbacks into the reply/answer mutations as an
optional `to`. The "awaiting you" signal switches from the legacy `whose_turn`
string to the caller-relative boolean `your_turn` in all three places that show it
(thread chip, task banner, nav counter), and the global Questions page gains an
off-by-default "waiting on me" filter.

**Tech Stack:** React 19, TypeScript, vitest + @testing-library/react, msw.

## Global Constraints

- Code, identifiers and comments in English. User-facing Russian only where the
  surrounding code already uses Russian (it does not, in these files).
- `messages[].author` and `asked_by` are still `""` for the human on the wire
  (`wireAuthor()` in `internal/api/questions.go:30-35`). Subtask #736 flips them to
  `"human"`. BOTH must render as the human — never assume one.
- `to` decides who must RESPOND (`waiting_on`), never who gets NOTIFIED.
- The "waiting on me" filter is OFF by default; when off it hides nothing.
- The badge is driven by `your_turn`, never by `whose_turn`.
- No CI in this repo: `cd web && npm test` plus `npx tsc -b` are the gate.

---

## File Structure

- Create `web/src/lib/participants.ts` — `HUMAN`, `isHuman()`, `participantLabel()`,
  `messageAuthorLabel()`. One responsibility: participant identity and naming.
- Create `web/src/lib/participants.test.ts`.
- Modify `web/src/lib/types.ts` — `participants`, `waiting_on`, `your_turn` on
  `Question`/`AgentQuestion`; `addressed_to` on `QuestionMessage`.
- Modify `web/src/lib/queries.ts` — optional `to` on the four reply/answer mutations.
- Modify `web/src/components/QuestionThreadView.tsx` (+ `questionthread.css`) —
  participants row, addressee chips, addressee picker.
- Modify `web/src/components/QuestionThread.tsx` /
  `web/src/components/AgentQuestionThread.tsx` — `your_turn` chip, tolerant
  `authorLabel`/`askerLabel`, pass `to` through.
- Modify `web/src/screens/task/QuestionBanner.tsx`,
  `web/src/components/AppShell.tsx`,
  `web/src/screens/questions/QuestionsScreen.tsx` (+ its css).
- Modify `web/src/mocks/fixtures.ts`, `web/src/mocks/handlers.ts`.

---

### Task 1: participant identity helper

**Files:**
- Create: `web/src/lib/participants.ts`
- Test: `web/src/lib/participants.test.ts`

**Interfaces:**
- Produces: `HUMAN = 'human'`; `isHuman(id?: string): boolean`;
  `participantLabel(id: string | undefined, agentName?: string): string`;
  `messageAuthorLabel(author: string | undefined, agentName: string | undefined,
  participants: string[] | undefined): string`.

- [ ] **Step 1: Write the failing test**

```ts
import { HUMAN, isHuman, messageAuthorLabel, participantLabel } from './participants'

describe('isHuman', () => {
  it('accepts both the legacy empty author and the canonical "human"', () => {
    expect(isHuman('')).toBe(true)
    expect(isHuman(undefined)).toBe(true)
    expect(isHuman(HUMAN)).toBe(true)
    expect(isHuman('cto')).toBe(false)
  })
})

describe('participantLabel', () => {
  it('labels the human "you" and falls back to the raw id otherwise', () => {
    expect(participantLabel('')).toBe('you')
    expect(participantLabel(HUMAN)).toBe('you')
    expect(participantLabel('cto')).toBe('cto')
    expect(participantLabel('s-orch', 'billing-orch')).toBe('billing-orch')
  })
})

describe('messageAuthorLabel', () => {
  it('uses the agent display name only when there is a single non-human participant', () => {
    expect(messageAuthorLabel('s-orch', 'billing-orch', ['human', 's-orch'])).toBe('billing-orch')
    expect(messageAuthorLabel('s-orch', 'billing-orch', undefined)).toBe('billing-orch')
    expect(messageAuthorLabel('s-orch', 'billing-orch', ['human', 's-orch', 'cto'])).toBe('s-orch')
    expect(messageAuthorLabel('', 'billing-orch', ['human', 's-orch', 'cto'])).toBe('you')
  })
})
```

- [ ] **Step 2: Run it and watch it fail**

Run: `cd web && npx vitest run src/lib/participants.test.ts`
Expected: FAIL — cannot resolve `./participants`.

- [ ] **Step 3: Implement**

```ts
// Participant identity for question threads. The wire is mid-migration: the
// human is `""` today (wireAuthor() in internal/api/questions.go) and becomes
// `"human"` in subtask #736, so every comparison goes through isHuman().

export const HUMAN = 'human'

export function isHuman(id?: string): boolean {
  return !id || id === HUMAN
}

export function participantLabel(id: string | undefined, agentName?: string): string {
  if (isHuman(id)) return 'you'
  return agentName ?? id!
}

/**
 * `agentName` (the orchestrator name / role id) is a display name for ONE
 * counterpart. In a thread with several agent participants it would
 * misattribute messages, so it only applies while there is a single non-human
 * participant — otherwise every agent shows under its own participant id.
 */
export function messageAuthorLabel(
  author: string | undefined,
  agentName: string | undefined,
  participants: string[] | undefined,
): string {
  if (isHuman(author)) return 'you'
  if (!participants) return agentName ?? author!
  const agents = participants.filter((p) => !isHuman(p))
  return agents.length <= 1 ? (agentName ?? author!) : author!
}
```

- [ ] **Step 4: Run the test and watch it pass**

Run: `cd web && npx vitest run src/lib/participants.test.ts` — expect PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/participants.ts web/src/lib/participants.test.ts
git commit -m "web: participant identity helper tolerant of \"\" and \"human\""
```

---

### Task 2: types gain participants, waiting_on, your_turn, addressed_to

**Files:**
- Modify: `web/src/lib/types.ts:344-395`, `web/src/lib/types.ts:474-489`

Types are compile-time only; the gate is `npx tsc -b` plus the tests of later
tasks, which cannot be written without these fields.

- [ ] **Step 1: Add the fields**

On `Question` and `AgentQuestion`, after `resolution`:

```ts
  /** Everyone taking part in the thread: "human", agent ids, session ids. */
  participants?: string[]
  /** The subset of `participants` expected to speak next. */
  waiting_on?: string[]
  /** Caller-relative: the dashboard user is in `waiting_on`. */
  your_turn?: boolean
```

On `QuestionMessage`, after `kind`:

```ts
  /** Who must respond to this message. Empty/absent = everyone but the author. */
  addressed_to?: string[]
```

- [ ] **Step 2: Typecheck**

Run: `cd web && npx tsc -b` — expect no errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/lib/types.ts
git commit -m "web: types for thread participants, waiting_on and addressees"
```

---

### Task 3: reply/answer mutations carry an optional `to`

**Files:**
- Modify: `web/src/lib/queries.ts:450-510`, `web/src/lib/queries.ts:800-840`
- Test: `web/src/lib/queries.test.tsx`

**Interfaces:**
- Produces: `useReplyQuestion()` / `useAnswerQuestion()` variables gain
  `to?: string[]`; same for `useReplyAgentQuestion()` / `useAnswerAgentQuestion()`.

- [ ] **Step 1: Write the failing test** (append to `queries.test.tsx`, following its
  existing render-hook + msw idiom)

```ts
test('reply sends `to` when addressees are picked and omits the key when none are', async () => {
  const bodies: Record<string, unknown>[] = []
  server.use(
    http.post('/v1/questions/:id/reply', async ({ request }) => {
      bodies.push((await request.json()) as Record<string, unknown>)
      return HttpResponse.json({ id: 1 }, { status: 201 })
    }),
  )
  const { result } = renderHook(() => useReplyQuestion(), { wrapper })

  await act(async () => {
    result.current.mutate({ id: 1, body: 'hi', taskId: 12, to: ['cto'] })
  })
  await waitFor(() => expect(bodies).toHaveLength(1))
  expect(bodies[0]).toEqual({ body: 'hi', to: ['cto'] })

  await act(async () => {
    result.current.mutate({ id: 1, body: 'hi', taskId: 12, to: [] })
  })
  await waitFor(() => expect(bodies).toHaveLength(2))
  expect(bodies[1]).toEqual({ body: 'hi' })
  expect(bodies[1]).not.toHaveProperty('to')
})
```

- [ ] **Step 2: Run it and watch it fail**

Run: `cd web && npx vitest run src/lib/queries.test.tsx -t 'reply sends'`
Expected: FAIL — `to` is not part of the variables type / not sent.

- [ ] **Step 3: Implement**

Add a shared helper near the question mutations:

```ts
/** `to` decides who must RESPOND. An empty pick must not put the key on the
 * wire at all — the API reads an absent `to` as "everyone but the author". */
function withTo<T extends object>(payload: T, to?: string[]): T & { to?: string[] } {
  return to && to.length > 0 ? { ...payload, to } : payload
}
```

Widen the four mutations' variable types with `to?: string[]` and build their
payloads through `withTo`, e.g.

```ts
    mutationFn: ({ id, body, to }) => api.post<Question>(`/v1/questions/${id}/reply`, withTo({ body }, to)),
```

and for answer:

```ts
    mutationFn: ({ id, body, dismiss, to }) =>
      api.post<Question>(`/v1/questions/${id}/answer`, dismiss ? { dismiss: true } : withTo({ body }, to)),
```

- [ ] **Step 4: Run the test and watch it pass**

Run: `cd web && npx vitest run src/lib/queries.test.tsx` — expect PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/queries.ts web/src/lib/queries.test.tsx
git commit -m "web: reply and answer mutations carry an optional addressee list"
```

---

### Task 4: thread view renders participants and addressees

**Files:**
- Modify: `web/src/components/QuestionThreadView.tsx`, `web/src/components/questionthread.css`
- Test: `web/src/components/QuestionThreadView.test.tsx`

**Interfaces:**
- Produces: `QuestionThreadViewProps` gains `participants?: string[]`;
  `ThreadEntry` gains `addressed_to?: string[]`.

- [ ] **Step 1: Write the failing tests**

```tsx
it('lists the thread participants', () => {
  render(<QuestionThreadView {...base} participants={['human', 's-orch', 'cto']} />)
  const row = screen.getByLabelText('Participants')
  expect(within(row).getByText('you')).toBeInTheDocument()
  expect(within(row).getByText('s-orch')).toBeInTheDocument()
  expect(within(row).getByText('cto')).toBeInTheDocument()
})

it('shows a message’s addressees when it has them', () => {
  render(
    <QuestionThreadView
      {...base}
      participants={['human', 's-orch', 'cto']}
      messages={[{ id: 1, author: 'cto', body: 'over to you', created_at: 0, addressed_to: ['s-orch'] }]}
    />,
  )
  expect(screen.getByText('→ s-orch')).toBeInTheDocument()
})

it('renders a human-authored message as "you" for both "" and "human"', () => {
  render(
    <QuestionThreadView
      {...base}
      messages={[
        { id: 1, author: '', body: 'legacy wire', created_at: 0 },
        { id: 2, author: 'human', body: 'post-#736 wire', created_at: 0 },
      ]}
    />,
  )
  expect(screen.getAllByText('you')).toHaveLength(2)
})
```

- [ ] **Step 2: Run and watch fail**

Run: `cd web && npx vitest run src/components/QuestionThreadView.test.tsx`
Expected: FAIL — no `Participants` region; `human` renders as an agent.

- [ ] **Step 3: Implement**

Import `isHuman`, `messageAuthorLabel`, `participantLabel`. Replace
`const fromAgent = !!m.author` with `const fromAgent = !isHuman(m.author)`, the
author cell with
`{messageAuthorLabel(m.author, agentName, participants)}`, and add after the
message head:

```tsx
{m.addressed_to && m.addressed_to.length > 0 && (
  <span className="question-thread__addressees">
    {'→ '}
    {m.addressed_to.map((id) => participantLabel(id, agentName)).join(', ')}
  </span>
)}
```

Add the participants row under the header:

```tsx
{participants && participants.length > 0 && (
  <div className="question-thread__participants" aria-label="Participants">
    {participants.map((id) => (
      <span key={id} className="question-thread__participant">
        {participantLabel(id, agentName)}
      </span>
    ))}
  </div>
)}
```

CSS follows the existing chip idiom (`--surface-2` background, 6px radius,
`var(--font-mono)` 11.5px, `--text-3`).

- [ ] **Step 4: Run and watch pass**

Run: `cd web && npx vitest run src/components/QuestionThreadView.test.tsx` — PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/QuestionThreadView.tsx web/src/components/questionthread.css web/src/components/QuestionThreadView.test.tsx
git commit -m "web: thread view shows participants and per-message addressees"
```

---

### Task 5: addressee picker in the composer

**Files:**
- Modify: `web/src/components/QuestionThreadView.tsx`, `web/src/components/questionthread.css`
- Test: `web/src/components/QuestionThreadView.test.tsx`

**Interfaces:**
- Produces: `onClarify(body: string, to: string[])` and
  `onAnswer(body: string, to: string[])` — `to` is `[]` when nothing is picked.

- [ ] **Step 1: Write the failing tests**

```tsx
it('passes the picked addressees to onAnswer and none when nothing is picked', async () => {
  const onAnswer = vi.fn()
  render(<QuestionThreadView {...base} participants={['human', 's-orch', 'cto']} onAnswer={onAnswer} />)

  await userEvent.type(screen.getByLabelText('Reply to Q1'), 'ok')
  await userEvent.click(screen.getByRole('button', { name: /Answer/ }))
  expect(onAnswer).toHaveBeenCalledWith('ok', [])

  await userEvent.type(screen.getByLabelText('Reply to Q1'), 'again')
  await userEvent.click(screen.getByRole('checkbox', { name: 'cto' }))
  await userEvent.click(screen.getByRole('button', { name: /Answer/ }))
  expect(onAnswer).toHaveBeenLastCalledWith('again', ['cto'])
})

it('never offers the human as an addressee', () => {
  render(<QuestionThreadView {...base} participants={['human', 's-orch']} />)
  expect(screen.queryByRole('checkbox', { name: 'you' })).not.toBeInTheDocument()
  expect(screen.getByRole('checkbox', { name: 's-orch' })).toBeInTheDocument()
})
```

- [ ] **Step 2: Run and watch fail**

Run: `cd web && npx vitest run src/components/QuestionThreadView.test.tsx -t addressees`
Expected: FAIL — no checkboxes.

- [ ] **Step 3: Implement**

```tsx
const [to, setTo] = useState<string[]>([])
const candidates = (participants ?? []).filter((p) => !isHuman(p))

function toggle(id: string) {
  setTo((prev) => (prev.includes(id) ? prev.filter((p) => p !== id) : [...prev, id]))
}

function submit(handler: (body: string, to: string[]) => void) {
  if (!body.trim()) return
  handler(body, to)
  setBody('')
}
```

and, above the actions row:

```tsx
{candidates.length > 0 && (
  <div className="question-thread__to">
    <span className="question-thread__to-label">Must respond</span>
    {candidates.map((id) => (
      <label key={id} className="question-thread__to-option">
        <input type="checkbox" checked={to.includes(id)} onChange={() => toggle(id)} />
        {participantLabel(id, agentName)}
      </label>
    ))}
    <span className="question-thread__to-hint">
      Everyone is notified either way; this picks who must answer.
    </span>
  </div>
)}
```

Note the checkbox label must be the raw participant id for non-human ids —
`participantLabel` already returns that (or `agentName` for a single agent).

- [ ] **Step 4: Run and watch pass**

Run: `cd web && npx vitest run src/components/QuestionThreadView.test.tsx` — PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/QuestionThreadView.tsx web/src/components/questionthread.css web/src/components/QuestionThreadView.test.tsx
git commit -m "web: composer lets the human pick who must respond"
```

---

### Task 6: your_turn badge and tolerant asker/author labels

**Files:**
- Modify: `web/src/components/QuestionThread.tsx`,
  `web/src/components/AgentQuestionThread.tsx`,
  `web/src/screens/task/QuestionBanner.tsx`, `web/src/components/AppShell.tsx`
- Test: `web/src/screens/task/Task.test.tsx`, `web/src/components/AppShell.test.tsx`

- [ ] **Step 1: Write the failing tests**

In `Task.test.tsx` (fixtures updated in Task 8 carry `your_turn`):

```tsx
it('renders a human-authored thread message as "you" for both "" and "human"', async () => {
  // Q3 on task #12 gets one message per wire spelling of the human.
  ...
  expect(screen.getAllByText('you').length).toBeGreaterThanOrEqual(2)
})
```

and an `authorLabel` unit assertion:

```tsx
expect(authorLabel('')).toBe('you')
expect(authorLabel('human')).toBe('you')
```

- [ ] **Step 2: Run and watch fail**

Run: `cd web && npx vitest run src/screens/task/Task.test.tsx`
Expected: FAIL — `authorLabel('human')` returns `'human'`.

- [ ] **Step 3: Implement**

`QuestionThread.tsx`:

```ts
export function authorLabel(author: string | undefined, orchestratorName?: string): string {
  return participantLabel(author, orchestratorName)
}

function whoseTurnLabel(question: Question): string {
  if (question.your_turn) return 'awaiting you'
  const waiting = (question.waiting_on ?? []).filter((p) => !isHuman(p))
  if (waiting.length > 0) return `awaiting ${waiting.join(', ')}`
  if (question.whose_turn === 'orchestrator') return 'awaiting orchestrator'
  return ''
}

function askerLabel(question: Question, orchestratorName?: string): string {
  if (isHuman(question.asked_by)) return 'you asked the orchestrator'
  return `${orchestratorName ?? question.asked_by} asked`
}
```

`turnWarn={!!question.your_turn}`, and pass `participants={question.participants}`
plus `onClarify={(body, to) => reply.mutate({ id: question.id, body, to, taskId })}`
(same for `onAnswer`). `AgentQuestionThread.tsx` mirrors this with `roleId`.

`QuestionBanner.tsx`: `const awaitingUser = question.your_turn === true`.

`AppShell.tsx`: `const awaitingCount = (questions ?? []).filter((q) => q.your_turn).length`.

- [ ] **Step 4: Run and watch pass**

Run: `cd web && npx vitest run src/screens/task src/components` — PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/components web/src/screens/task
git commit -m "web: drive the awaiting-you signal from your_turn"
```

---

### Task 7: "waiting on me" filter on the Questions page

**Files:**
- Modify: `web/src/screens/questions/QuestionsScreen.tsx`,
  `web/src/screens/questions/QuestionsScreen.css`
- Test: `web/src/screens/questions/Questions.test.tsx`

- [ ] **Step 1: Write the failing tests**

```tsx
test('the "waiting on me" filter is off by default and hides nothing', async () => {
  renderQuestions()
  expect(await screen.findByText('Awaiting you')).toBeInTheDocument()
  const filter = screen.getByRole('checkbox', { name: /waiting on me/i })
  expect(filter).not.toBeChecked()
  expect(screen.getByText('Awaiting others')).toBeInTheDocument()
})

test('checking it narrows the list to your_turn threads', async () => {
  renderQuestions()
  await screen.findByText('Awaiting you')
  await userEvent.click(screen.getByRole('checkbox', { name: /waiting on me/i }))
  expect(screen.queryByText('Awaiting others')).not.toBeInTheDocument()
  expect(screen.getByText('Awaiting you')).toBeInTheDocument()
})
```

- [ ] **Step 2: Run and watch fail**

Run: `cd web && npx vitest run src/screens/questions/Questions.test.tsx`
Expected: FAIL — no checkbox.

- [ ] **Step 3: Implement**

```tsx
const [onlyMine, setOnlyMine] = useState(false)
const all = questions ?? []
const awaitingYou = all.filter((q) => q.your_turn)
const awaitingOthers = onlyMine ? [] : all.filter((q) => !q.your_turn)
```

Render the checkbox next to the title:

```tsx
<label className="questions-screen__filter">
  <input type="checkbox" checked={onlyMine} onChange={(e) => setOnlyMine(e.target.checked)} />
  Waiting on me
</label>
```

Rename the second group label to `Awaiting others` (it is no longer always the
orchestrator).

- [ ] **Step 4: Run and watch pass**

Run: `cd web && npx vitest run src/screens/questions/Questions.test.tsx` — PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/screens/questions
git commit -m "web: off-by-default \"waiting on me\" filter on the questions page"
```

---

### Task 8: mocks speak the participant contract

**Files:**
- Modify: `web/src/mocks/fixtures.ts:418-528`, `web/src/mocks/handlers.ts:599-675`

- [ ] **Step 1: Update fixtures**

Every fixture question gains `participants`, `waiting_on` and `your_turn`
consistent with its `whose_turn`, e.g. for Q3 (open, awaiting the user):

```ts
    participants: ['human', 's-billing-v2-orch'],
    waiting_on: ['human'],
    your_turn: true,
```

Give Q5 a third participant (`'cto'`) and an `addressed_to` on its reply so the
multi-participant path has fixture coverage.

- [ ] **Step 2: Update handlers**

`POST /v1/tasks/:id/questions`, `/v1/questions/:id/reply` and
`/v1/questions/:id/answer` read the optional `to`, store it as the message's
`addressed_to`, and recompute `waiting_on`/`your_turn`:

```ts
const to = body.to && body.to.length > 0 ? body.to : undefined
question.messages.push({ ..., addressed_to: to })
question.waiting_on = to ?? (question.participants ?? []).filter((p) => p !== 'human')
question.your_turn = (question.waiting_on ?? []).includes('human')
question.whose_turn = question.your_turn ? 'user' : 'orchestrator'
```

Mirror the same three changes in the agent-question handlers.

- [ ] **Step 3: Run the whole suite**

Run: `cd web && npm test` — expect PASS.

- [ ] **Step 4: Commit**

```bash
git add web/src/mocks
git commit -m "web: mocks carry participants, waiting_on and addressees"
```

---

### Task 9: verification

- [ ] **Step 1:** `cd web && npm test` — record the real output.
- [ ] **Step 2:** `cd web && npx tsc -b` — record the real output.
- [ ] **Step 3:** `go build ./... && go test ./internal/api/...` — untouched, confirm
  nothing regressed outside `web/`.
- [ ] **Step 4:** Open the PR with those outputs pasted into the body.
