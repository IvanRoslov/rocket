# Mobile: Multi-Participant Question Threads Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring the rocket mobile app to the multi-participant thread model — show a thread's participants and per-message addressees, drive the "awaiting you" indicator off `your_turn`, let the human pick addressees when replying, and recognise the human from both `""` and `"human"`.

**Architecture:** All the new rules are pure functions in a new `mobile/src/lib/threads.ts`, unit-tested without rendering — the same idiom the repo already uses for `src/lib/agents.ts` and `src/lib/quiz.ts`. The two thread screens (`app/task/[id].tsx` and `app/agent/[id].tsx`) hold near-identical Q&A cards; both consume the same helpers so the two screens cannot drift. Types gain the new wire fields; the three mutation hooks in `src/api/queries.ts` gain an optional `to`.

**Tech Stack:** TypeScript, React Native 0.86 / Expo SDK 57 (expo-router), @tanstack/react-query 5, Jest (`jest-expo` preset) + @testing-library/react-native.

## Global Constraints

- Expo SDK 57 — consult https://docs.expo.dev/versions/v57.0.0/ before writing any Expo API call (`mobile/AGENTS.md`).
- Code, identifiers, comments and commit messages in English. User-facing Russian only where the surrounding code is already Russian — it is not, so all copy stays English.
- The author wire is legacy and stays legacy: `messages[].author` and `asked_by` are `""` for the human today and `"human"` after subtask #736. **Both must render as the human.** Never assume one form.
- `to` decides who must RESPOND (`waiting_on`), never who gets NOTIFIED. Do not write copy claiming otherwise.
- No CI exists in this repo. Local `npm test` and `tsc --noEmit` in `mobile/` are the gate.
- Pre-existing failures elsewhere in the repo (`internal/cli`, `internal/queue`, `internal/session`) are not ours — do not touch them.
- One branch, one PR: `feature/reply-answer/mobile-participants`.

## Wire contract consumed (verified against `internal/api/questions.go:50-112`)

```json
{
  "id": 262, "task_id": 722,
  "asked_by": "",
  "participants": ["human", "reply-answer-orch", "cto"],
  "waiting_on": ["human", "cto"],
  "your_turn": true,
  "whose_turn": "user",
  "messages": [
    {"id": 1, "author": "cto", "kind": "reply",
     "addressed_to": ["reply-answer-orch"], "body": "...", "created_at": 0}
  ]
}
```

`participants` and `waiting_on` are always present (no `omitempty`); `addressed_to` and `your_turn` likewise. `whose_turn` is compat-only and **must not** drive the indicator.

## File Structure

| File | Responsibility |
|---|---|
| `mobile/src/lib/threads.ts` (create) | All participant/turn/addressee rules as pure functions. The single place that knows `""` and `"human"` are the same person. |
| `mobile/src/lib/threads.test.ts` (create) | Unit tests for the above, including the both-forms tolerance assertion. |
| `mobile/src/api/types.ts` (modify) | `QuestionMessage.addressed_to`; `participants`/`waiting_on`/`your_turn` on `Question` and `AgentQuestion`. |
| `mobile/src/api/queries.ts` (modify) | `useCreateQuestion`, `useQuestionReply`, `useQuestionAnswer` and the three agent equivalents take optional `to`. |
| `mobile/src/api/mutations.test.tsx` (modify) | Asserts `to` is sent when picked and the key is absent when not. |
| `mobile/app/task/[id].tsx` (modify) | Task thread card: participants row, per-message author + addressees, `your_turn` indicator, addressee picker. |
| `mobile/app/agent/[id].tsx` (modify) | Same for role threads. |
| `mobile/app/agent/agent-screens.test.tsx` (modify) | Renders the role thread against the new wire shape. |
| `mobile/app/task/task-thread.test.tsx` (create) | Renders the task thread; there is no task-screen test today. |
| `mobile/package.json` (modify) | Add the missing `typecheck` script. |

---

### Task 1: The pure thread rules

**Files:**
- Create: `mobile/src/lib/threads.ts`
- Test: `mobile/src/lib/threads.test.ts`

**Interfaces:**
- Consumes: `Question`, `AgentQuestion`, `QuestionMessage` from `../api/types` (extended in Task 2 — write Task 2 first if types do not yet compile).
- Produces:
  - `HUMAN: 'human'`
  - `isHuman(id: string | undefined): boolean`
  - `participantLabel(id: string | undefined): string`
  - `participantInitial(id: string | undefined): string`
  - `addresseeLabel(to: string[] | undefined): string`
  - `answerableBy(participants: string[]): string[]`
  - `toggleAddressee(sel: string[], id: string): string[]`
  - `addresseePayload(sel: string[]): { to?: string[] }`
  - `countYourTurn(threads: { status: string; your_turn?: boolean }[]): number`

- [ ] **Step 1: Write the failing test**

```ts
// mobile/src/lib/threads.test.ts
import {
  addresseeLabel,
  addresseePayload,
  answerableBy,
  countYourTurn,
  isHuman,
  participantInitial,
  participantLabel,
  toggleAddressee,
} from './threads'

describe('isHuman', () => {
  // The wire sends "" today and "human" after subtask #736. Both are us.
  it('recognises the human in both wire forms', () => {
    expect(isHuman('')).toBe(true)
    expect(isHuman('human')).toBe(true)
    expect(isHuman(undefined)).toBe(true)
  })

  it('does not mistake an agent or a session for the human', () => {
    expect(isHuman('cto')).toBe(false)
    expect(isHuman('reply-answer-orch')).toBe(false)
  })
})

describe('participantLabel', () => {
  it('labels both human forms as "you"', () => {
    expect(participantLabel('')).toBe('you')
    expect(participantLabel('human')).toBe('you')
  })

  it('labels anyone else by their id', () => {
    expect(participantLabel('cto')).toBe('cto')
  })
})

describe('participantInitial', () => {
  it('gives the human a Y and everyone else their first letter', () => {
    expect(participantInitial('human')).toBe('Y')
    expect(participantInitial('')).toBe('Y')
    expect(participantInitial('cto')).toBe('C')
  })
})

describe('addresseeLabel', () => {
  it('is empty when nobody is addressed', () => {
    expect(addresseeLabel([])).toBe('')
    expect(addresseeLabel(undefined)).toBe('')
  })

  it('names the addressees, the human included', () => {
    expect(addresseeLabel(['cto'])).toBe('→ cto')
    expect(addresseeLabel(['human', 'cto'])).toBe('→ you, cto')
  })
})

describe('answerableBy', () => {
  it('offers every participant except the human', () => {
    expect(answerableBy(['human', 'reply-answer-orch', 'cto'])).toEqual(['reply-answer-orch', 'cto'])
  })

  it('tolerates the legacy empty human id', () => {
    expect(answerableBy(['', 'cto'])).toEqual(['cto'])
  })
})

describe('toggleAddressee', () => {
  it('adds then removes', () => {
    expect(toggleAddressee([], 'cto')).toEqual(['cto'])
    expect(toggleAddressee(['cto'], 'cto')).toEqual([])
  })

  it('keeps several addressees', () => {
    expect(toggleAddressee(['cto'], 'orch')).toEqual(['cto', 'orch'])
  })
})

describe('addresseePayload', () => {
  // "None picked" must send no `to` key at all — the daemon then falls back to
  // "everyone except the author", which is a different thing from `to: []`.
  it('omits the key when nobody is picked', () => {
    expect(addresseePayload([])).toEqual({})
    expect('to' in addresseePayload([])).toBe(false)
  })

  it('sends the picked addressees', () => {
    expect(addresseePayload(['cto'])).toEqual({ to: ['cto'] })
  })
})

describe('countYourTurn', () => {
  it('counts only open threads waiting on us', () => {
    const threads = [
      { status: 'open', your_turn: true },
      { status: 'open', your_turn: false },
      { status: 'resolved', your_turn: true },
      { status: 'open' },
    ]
    expect(countYourTurn(threads)).toBe(1)
  })

  it('is 0 for no threads', () => {
    expect(countYourTurn([])).toBe(0)
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd mobile && npx jest src/lib/threads.test.ts`
Expected: FAIL — "Cannot find module './threads'".

- [ ] **Step 3: Write minimal implementation**

```ts
// mobile/src/lib/threads.ts
// Presentation rules for multi-participant Q&A threads (docs/12-tasks.md).
// Kept pure so both thread screens share one set of rules and they are
// unit-tested without rendering.

/** The canonical participant id of the person using the app. */
export const HUMAN = 'human'

/**
 * True for the human. The author wire is deliberately legacy: `asked_by` and
 * `messages[].author` are "" today and become "human" with subtask #736,
 * while `participants`/`waiting_on`/`addressed_to` already say "human". Both
 * forms mean us, so never compare against just one.
 */
export function isHuman(id: string | undefined): boolean {
  return !id || id === HUMAN
}

/** Display name of a participant: ourselves as "you", anyone else by id. */
export function participantLabel(id: string | undefined): string {
  return isHuman(id) ? 'you' : id!
}

/** Single-letter avatar glyph matching `participantLabel`. */
export function participantInitial(id: string | undefined): string {
  return isHuman(id) ? 'Y' : id!.slice(0, 1).toUpperCase()
}

/** "→ cto, you" for a message's addressees; empty when it addressed everyone. */
export function addresseeLabel(to: string[] | undefined): string {
  if (!to || to.length === 0) return ''
  return `→ ${to.map(participantLabel).join(', ')}`
}

/** The addressees we may pick: every participant but ourselves. */
export function answerableBy(participants: string[]): string[] {
  return participants.filter((p) => !isHuman(p))
}

/** Flip one addressee in the picker's selection, preserving order. */
export function toggleAddressee(sel: string[], id: string): string[] {
  return sel.includes(id) ? sel.filter((x) => x !== id) : [...sel, id]
}

/**
 * Body fragment carrying the picked addressees. Picking nobody must omit the
 * key entirely rather than send `to: []`: an absent `to` means "everyone
 * except the author", which is the daemon's default and not the same thing.
 */
export function addresseePayload(sel: string[]): { to?: string[] } {
  return sel.length > 0 ? { to: sel } : {}
}

/**
 * Open threads whose next word is ours. Driven by `your_turn`, the
 * participant-aware field — never by the compat `whose_turn`.
 */
export function countYourTurn(threads: { status: string; your_turn?: boolean }[]): number {
  return threads.filter((t) => t.status === 'open' && t.your_turn === true).length
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd mobile && npx jest src/lib/threads.test.ts`
Expected: PASS, 15 tests.

- [ ] **Step 5: Commit**

```bash
git add mobile/src/lib/threads.ts mobile/src/lib/threads.test.ts
git commit -m "mobile: pure rules for multi-participant threads"
```

---

### Task 2: Types learn the participant fields

**Files:**
- Modify: `mobile/src/api/types.ts:161-182` (`QuestionMessage`, `Question`), `mobile/src/api/types.ts:219-233` (`AgentQuestion`)
- Modify: `mobile/package.json` (add the `typecheck` script)

**Interfaces:**
- Produces: `QuestionMessage.addressed_to?: string[]`; `participants: string[]`, `waiting_on: string[]`, `your_turn: boolean` on both `Question` and `AgentQuestion`.

- [ ] **Step 1: Add the `typecheck` script**

`mobile/package.json` has no typecheck script today; the brief assumes one. Add it inside `"scripts"`:

```json
    "test": "jest",
    "typecheck": "tsc --noEmit"
```

- [ ] **Step 2: Extend the types**

In `mobile/src/api/types.ts`, replace the `QuestionMessage` interface with:

```ts
export interface QuestionMessage {
  id: number
  author?: string
  kind: 'reply' | 'answer'
  body: string
  /**
   * Who owes a response to this message. Empty or absent means "everyone in
   * the thread except its author" — the daemon's default fan-out.
   */
  addressed_to?: string[]
  created_at: number
}
```

Add these three fields to **both** `Question` and `AgentQuestion`, right above their `asked_at`:

```ts
  /** Everyone taking part in the thread; ids are "human", an agent or a session. */
  participants: string[]
  /** The subset expected to speak next. */
  waiting_on: string[]
  /** Whether the caller — us — is one of them. Drives the indicator. */
  your_turn: boolean
```

Leave the existing `whose_turn` fields in place: they are the daemon's compat field until subtask #736 and removing them is not ours to do.

- [ ] **Step 3: Verify the typecheck reports the call sites that must change**

Run: `cd mobile && npm run typecheck`
Expected: FAIL — errors in `app/agent/agent-screens.test.tsx` where the `QUESTIONS` fixture now lacks `participants`, `waiting_on` and `your_turn`. That failure list is the work of Tasks 3-6.

- [ ] **Step 4: Commit**

```bash
git add mobile/src/api/types.ts mobile/package.json
git commit -m "mobile: thread types carry participants, waiting_on and your_turn"
```

---

### Task 3: Mutations carry the picked addressees

**Files:**
- Modify: `mobile/src/api/queries.ts:257-290` (`useCreateQuestion`, `useQuestionReply`, `useQuestionAnswer`) and `mobile/src/api/queries.ts:508-536` (`useCreateAgentQuestion`, `useAgentQuestionReply`, `useAgentQuestionAnswer`)
- Test: `mobile/src/api/mutations.test.tsx`

Note: the brief names `mobile/src/api/mutations.ts`; no such file exists — the mutation hooks live in `queries.ts` and only the *test* is called `mutations.test.tsx`. Follow the code.

**Interfaces:**
- Consumes: `addresseePayload` from `../lib/threads` (Task 1).
- Produces: every one of the six hooks accepts an optional `to?: string[]` in its mutate variables.

- [ ] **Step 1: Write the failing test**

Append to the `describe('mutation hooks')` block in `mobile/src/api/mutations.test.tsx`, and add `useQuestionAnswer`, `useQuestionReply` and `useCreateQuestion` to the import list from `./queries`:

```tsx
  it('reply sends the picked addressees', async () => {
    const r = await setup(useQuestionReply)
    await act(async () => {
      await r.current.hook.mutateAsync({ id: 7, body: 'here you go', to: ['cto'] })
    })
    const { url, init } = lastCall()
    expect(url).toBe(`${BASE}/v1/questions/7/reply`)
    expect(JSON.parse(init.body as string)).toEqual({ body: 'here you go', to: ['cto'] })
  })

  it('reply omits the to key when nobody is picked', async () => {
    const r = await setup(useQuestionReply)
    await act(async () => {
      await r.current.hook.mutateAsync({ id: 7, body: 'here you go' })
    })
    expect(JSON.parse(lastCall().init.body as string)).toEqual({ body: 'here you go' })
  })

  it('answer sends the picked addressees', async () => {
    const r = await setup(useQuestionAnswer)
    await act(async () => {
      await r.current.hook.mutateAsync({ id: 7, body: 'final', to: ['cto', 'reply-answer-orch'] })
    })
    const { url, init } = lastCall()
    expect(url).toBe(`${BASE}/v1/questions/7/answer`)
    expect(JSON.parse(init.body as string)).toEqual({
      body: 'final',
      to: ['cto', 'reply-answer-orch'],
    })
  })

  it('creating a thread sends the picked addressees', async () => {
    const r = await setup(useCreateQuestion)
    await act(async () => {
      await r.current.hook.mutateAsync({ taskId: 12, body: 'who owns this?', to: ['cto'] })
    })
    const { url, init } = lastCall()
    expect(url).toBe(`${BASE}/v1/tasks/12/questions`)
    expect(JSON.parse(init.body as string)).toEqual({ body: 'who owns this?', to: ['cto'] })
  })
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd mobile && npx jest src/api/mutations.test.tsx`
Expected: FAIL — the bodies come back without `to`, e.g. received `{"body": "here you go"}` where `{"body": "here you go", "to": ["cto"]}` was expected.

- [ ] **Step 3: Write minimal implementation**

In `mobile/src/api/queries.ts`, import the helper at the top with the other local imports:

```ts
import { addresseePayload } from '../lib/threads'
```

Then thread `to` through each hook. `useCreateQuestion` becomes:

```ts
export function useCreateQuestion() {
  // ...existing surrounding lines unchanged...
    mutationFn: (p: { taskId: number; body: string; context?: string; to?: string[] }) =>
      api.post<Question>(baseUrl, `/v1/tasks/${p.taskId}/questions`, {
        body: p.body,
        context: p.context,
        ...addresseePayload(p.to ?? []),
      }),
```

`useQuestionReply`:

```ts
    mutationFn: (p: { id: number; body: string; to?: string[] }) =>
      api.post(baseUrl, `/v1/questions/${p.id}/reply`, {
        body: p.body,
        ...addresseePayload(p.to ?? []),
      }),
```

`useQuestionAnswer`:

```ts
    mutationFn: (p: { id: number; body: string; to?: string[] }) =>
      api.post(baseUrl, `/v1/questions/${p.id}/answer`, {
        body: p.body,
        ...addresseePayload(p.to ?? []),
      }),
```

Apply the same three edits to `useCreateAgentQuestion` (`/v1/agents/${p.agentId}/questions`, variable `agentId`), `useAgentQuestionReply` (`/v1/agent-questions/${p.id}/reply`) and `useAgentQuestionAnswer` (`/v1/agent-questions/${p.id}/answer`).

Leave `useQuestionDismiss` and `useAgentQuestionDismiss` alone: dismissing closes the thread, so there is nobody left to address.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd mobile && npx jest src/api/mutations.test.tsx`
Expected: PASS, 11 tests.

- [ ] **Step 5: Commit**

```bash
git add mobile/src/api/queries.ts mobile/src/api/mutations.test.tsx
git commit -m "mobile: question mutations carry the picked addressees"
```

---

### Task 4: The role thread screen shows participants, addressees and your-turn

**Files:**
- Modify: `mobile/app/agent/[id].tsx:52-180` (`AgentQuestionCard`), `mobile/app/agent/[id].tsx:273`
- Test: `mobile/app/agent/agent-screens.test.tsx`

**Interfaces:**
- Consumes: `isHuman`, `participantLabel`, `participantInitial`, `addresseeLabel`, `answerableBy`, `toggleAddressee`, `countYourTurn` from `../../src/lib/threads`; the `to`-aware hooks from Task 3.

- [ ] **Step 1: Write the failing test**

In `mobile/app/agent/agent-screens.test.tsx`, replace the `QUESTIONS` fixture with a participant-shaped thread whose messages exercise both human wire forms:

```tsx
const QUESTIONS = {
  questions: [
    {
      id: 1,
      role_id: 'sre',
      ordinal: 1,
      asked_by: 'sre',
      body: 'status?',
      context: 'from mobile',
      status: 'open',
      participants: ['human', 'sre', 'cto'],
      waiting_on: ['human'],
      your_turn: true,
      whose_turn: 'user',
      asked_at: 1785622879,
      messages: [
        // The legacy empty form and the canonical one must render identically.
        { id: 11, author: '', kind: 'reply', body: 'looking', addressed_to: [], created_at: 1785622880 },
        { id: 12, author: 'human', kind: 'reply', body: 'still looking', addressed_to: ['cto'], created_at: 1785622881 },
        { id: 13, author: 'cto', kind: 'reply', body: 'over to you', addressed_to: ['human'], created_at: 1785622882 },
      ],
    },
  ],
}
```

Then add to `describe('AgentScreen')`:

```tsx
  it('lists the thread participants', async () => {
    mockApi()
    renderWithProviders(<AgentScreen />)
    await waitFor(() => expect(screen.getByText(/status\?/)).toBeTruthy())
    expect(screen.getByText('you, sre, cto')).toBeTruthy()
  })

  it('flags our turn from your_turn, not whose_turn', async () => {
    mockApi()
    renderWithProviders(<AgentScreen />)
    await waitFor(() => expect(screen.getByText('awaiting you')).toBeTruthy())
  })

  it('does not flag our turn when your_turn is false', async () => {
    // whose_turn stays "user" so a regression back onto the compat field fails here.
    const q = QUESTIONS.questions[0]
    mockApi({
      '/v1/agents/sre/questions': {
        questions: [{ ...q, your_turn: false, waiting_on: ['cto'], whose_turn: 'user' }],
      },
    })
    renderWithProviders(<AgentScreen />)
    await waitFor(() => expect(screen.getByText(/status\?/)).toBeTruthy())
    expect(screen.queryByText('awaiting you')).toBeNull()
  })

  it('renders a human message as us for both the empty and canonical author', async () => {
    mockApi()
    renderWithProviders(<AgentScreen />)
    await waitFor(() => expect(screen.getByText('looking')).toBeTruthy())
    // Two human-authored messages: author "" and author "human".
    expect(screen.getAllByText('you').length).toBe(2)
    expect(screen.getByText('cto')).toBeTruthy()
  })

  it('shows who a message was addressed to', async () => {
    mockApi()
    renderWithProviders(<AgentScreen />)
    await waitFor(() => expect(screen.getByText('still looking')).toBeTruthy())
    expect(screen.getByText('→ cto')).toBeTruthy()
    expect(screen.getByText('→ you')).toBeTruthy()
  })

  it('sends the picked addressee with the reply', async () => {
    mockApi()
    renderWithProviders(<AgentScreen />)
    await waitFor(() => expect(screen.getByText(/status\?/)).toBeTruthy())
    fireEvent.changeText(screen.getByPlaceholderText(/Write a reply/), 'ack')
    fireEvent.press(screen.getByText('cto', { exact: true }))
    fireEvent.press(screen.getByText('Clarify'))
    await waitFor(() => {
      const call = (fetch as jest.Mock).mock.calls.find(([u]: [string]) =>
        String(u).includes('/v1/agent-questions/1/reply'),
      )
      expect(call).toBeTruthy()
      expect(JSON.parse(call[1].body)).toEqual({ body: 'ack', to: ['cto'] })
    })
  })
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd mobile && npx jest app/agent/agent-screens.test.tsx`
Expected: FAIL — "Unable to find an element with text: you, sre, cto"; the addressee and picker tests fail likewise.

- [ ] **Step 3: Write minimal implementation**

In `mobile/app/agent/[id].tsx`, import the helpers:

```ts
import {
  addresseeLabel,
  answerableBy,
  countYourTurn,
  isHuman,
  participantInitial,
  participantLabel,
  toggleAddressee,
} from '../../src/lib/threads'
```

Inside `AgentQuestionCard`, add the picker state next to the existing `useState`s and make `mine` tolerant:

```ts
  const [to, setTo] = useState<string[]>([])
  // A thread we opened flips the roles; the human is "" today and "human"
  // after subtask #736, so recognise both.
  const mine = isHuman(q.asked_by)
  const others = answerableBy(q.participants ?? [])
```

Replace the turn badge (currently `q.whose_turn === 'user' ? ...`) with:

```tsx
        <Badge
          label={q.your_turn ? 'awaiting you' : `waiting for ${(q.waiting_on ?? []).map(participantLabel).join(', ') || agentId}`}
          fg={colors.amberDeep}
          bg={colors.amberBg}
        />
```

Replace the `{mine ? 'you asked' : agentId}` mono text with `{participantLabel(q.asked_by)} asked`.

Add a participants row directly under the `qHead` view, before the body `Text`:

```tsx
        <Text style={styles.discussLabel}>PARTICIPANTS</Text>
        <Text style={{ fontSize: 12.5, color: colors.textDim, marginBottom: 12 }}>
          {(q.participants ?? []).map(participantLabel).join(', ')}
        </Text>
```

Replace the message-row author block. `const isUser = !m.author` becomes `const isUser = isHuman(m.author)`, the avatar glyph becomes `participantInitial(m.author)`, the name becomes `participantLabel(m.author)`, and an addressee line joins the header row:

```tsx
                <Text style={{ fontSize: 12.5, fontWeight: '600', color: isUser ? colors.indigoFg : colors.amberDeep }}>
                  {participantLabel(m.author)}
                </Text>
                {addresseeLabel(m.addressed_to) ? (
                  <Text style={{ fontSize: 11, color: colors.textDim }}>{addresseeLabel(m.addressed_to)}</Text>
                ) : null}
                <Text style={{ fontSize: 11, color: colors.textFaint }}>{ago(m.created_at)}</Text>
```

Add the picker inside `styles.replyBox`, above the `TextInput`:

```tsx
          {others.length > 0 ? (
            <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: 7, marginBottom: 10 }}>
              <Text style={{ fontSize: 11, color: colors.textFaint, alignSelf: 'center' }}>TO</Text>
              {others.map((p) => {
                const on = to.includes(p)
                return (
                  <Pressable
                    key={p}
                    onPress={() => setTo((sel) => toggleAddressee(sel, p))}
                    style={{
                      paddingHorizontal: 10,
                      paddingVertical: 5,
                      borderRadius: radius.pill,
                      borderWidth: 1,
                      borderColor: on ? colors.amberDeep : colors.border,
                      backgroundColor: on ? colors.amberBg : 'transparent',
                    }}
                  >
                    <Text style={{ fontSize: 12, color: on ? colors.amberDeep : colors.textDim }}>{p}</Text>
                  </Pressable>
                )
              })}
            </View>
          ) : null}
```

Send the selection and clear it on success:

```ts
  const send = (final: boolean) => {
    const body = text.trim()
    if (!body) return
    const m = final ? answer : reply
    m.mutate(
      { id: q.id, body, to },
      {
        onSuccess: () => {
          setText('')
          setTo([])
        },
        onError: (e) => toast.show((e as Error).message),
      },
    )
  }
```

Finally, at `mobile/app/agent/[id].tsx:273`, drive the screen's awaiting list off `your_turn`:

```ts
  const awaiting = open.filter((q) => q.your_turn)
```

`radius` is already imported on this screen; `countYourTurn` is used in Task 6.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd mobile && npx jest app/agent/agent-screens.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add mobile/app/agent/[id].tsx mobile/app/agent/agent-screens.test.tsx
git commit -m "mobile: role threads show participants, addressees and your-turn"
```

---

### Task 5: The task thread screen gets the same treatment

**Files:**
- Modify: `mobile/app/task/[id].tsx:58-198` (`QuestionCard`), `mobile/app/task/[id].tsx:200-263` (`AskQuestionSheet`), `mobile/app/task/[id].tsx:436`
- Test: `mobile/app/task/task-thread.test.tsx` (create — no task-screen test exists)

**Interfaces:**
- Consumes: the same helpers as Task 4; `useCreateQuestion` now takes `to`.

- [ ] **Step 1: Write the failing test**

```tsx
// mobile/app/task/task-thread.test.tsx
/**
 * Renders the task screen's Q&A tab against the daemon's participant-shaped
 * JSON, so a contract drift or a crashing branch fails here, not in the app.
 */
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native'
import { SafeAreaProvider } from 'react-native-safe-area-context'
import { ToastProvider } from '../../src/components/Toast'
import { ServerProvider } from '../../src/servers/ServerContext'
import TaskScreen from './[id]'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

jest.mock('expo-router', () => ({
  router: { navigate: jest.fn(), push: jest.fn(), back: jest.fn() },
  useLocalSearchParams: () => ({ id: '12' }),
}))

jest.mock('expo-clipboard', () => ({ setStringAsync: jest.fn() }))

const TASK = {
  id: 12,
  title: 'ship the thing',
  project_id: 'platform',
  status: 'in_progress',
  created_by: 'user',
  created_at: 1785622872,
  updated_at: 1785622879,
  subtasks: [],
  open_questions: 1,
}

const THREAD = {
  id: 1,
  task_id: 12,
  ordinal: 1,
  asked_by: 'reply-answer-orch',
  body: 'which repo?',
  status: 'open',
  participants: ['human', 'reply-answer-orch', 'cto'],
  waiting_on: ['human'],
  your_turn: true,
  whose_turn: 'user',
  asked_at: 1785622879,
  messages: [
    { id: 11, author: '', kind: 'reply', body: 'checking', addressed_to: [], created_at: 1785622880 },
    { id: 12, author: 'human', kind: 'reply', body: 'still checking', addressed_to: ['cto'], created_at: 1785622881 },
    { id: 13, author: 'cto', kind: 'reply', body: 'your call', addressed_to: ['human'], created_at: 1785622882 },
  ],
}

function mockApi(overrides: Record<string, unknown> = {}) {
  const bodies: Record<string, unknown> = {
    '/v1/tasks/12': TASK,
    '/v1/tasks/12/questions': { questions: [THREAD] },
    '/v1/sessions': [],
    ...overrides,
  }
  globalThis.fetch = jest.fn(async (url: string) => {
    const path = String(url).replace(/^https?:\/\/[^/]+/, '')
    if (!(path in bodies)) {
      return { ok: false, status: 404, json: async () => ({ error: { code: 'not_found' } }) }
    }
    return { ok: true, status: 200, json: async () => bodies[path] }
  }) as unknown as typeof fetch
}

function renderWithProviders(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } })
  const metrics = {
    frame: { x: 0, y: 0, width: 390, height: 844 },
    insets: { top: 47, left: 0, right: 0, bottom: 34 },
  }
  return render(
    <SafeAreaProvider initialMetrics={metrics}>
      <QueryClientProvider client={qc}>
        <ServerProvider>
          <ToastProvider>{ui}</ToastProvider>
        </ServerProvider>
      </QueryClientProvider>
    </SafeAreaProvider>,
  )
}

/** Opens the Questions tab, which is not the screen's default. */
async function openQuestions() {
  await waitFor(() => expect(screen.getByText('Questions')).toBeTruthy())
  fireEvent.press(screen.getByText('Questions'))
  await waitFor(() => expect(screen.getByText('which repo?')).toBeTruthy())
}

describe('task Q&A thread', () => {
  afterEach(() => jest.restoreAllMocks())

  it('lists the thread participants', async () => {
    mockApi()
    renderWithProviders(<TaskScreen />)
    await openQuestions()
    expect(screen.getByText('you, reply-answer-orch, cto')).toBeTruthy()
  })

  it('flags our turn from your_turn, not whose_turn', async () => {
    mockApi()
    renderWithProviders(<TaskScreen />)
    await openQuestions()
    expect(screen.getByText('awaiting you')).toBeTruthy()
  })

  it('does not flag our turn when your_turn is false', async () => {
    mockApi({
      '/v1/tasks/12/questions': {
        questions: [{ ...THREAD, your_turn: false, waiting_on: ['cto'], whose_turn: 'user' }],
      },
    })
    renderWithProviders(<TaskScreen />)
    await openQuestions()
    expect(screen.queryByText('awaiting you')).toBeNull()
  })

  it('renders a human message as us for both the empty and canonical author', async () => {
    mockApi()
    renderWithProviders(<TaskScreen />)
    await openQuestions()
    expect(screen.getAllByText('you').length).toBe(2)
  })

  it('shows who a message was addressed to', async () => {
    mockApi()
    renderWithProviders(<TaskScreen />)
    await openQuestions()
    expect(screen.getByText('→ cto')).toBeTruthy()
    expect(screen.getByText('→ you')).toBeTruthy()
  })

  it('sends the picked addressee with the reply', async () => {
    mockApi()
    renderWithProviders(<TaskScreen />)
    await openQuestions()
    fireEvent.changeText(screen.getByPlaceholderText(/Write a reply/), 'the monorepo')
    fireEvent.press(screen.getByText('cto', { exact: true }))
    fireEvent.press(screen.getByText('Clarify'))
    await waitFor(() => {
      const call = (fetch as jest.Mock).mock.calls.find(([u]: [string]) =>
        String(u).includes('/v1/questions/1/reply'),
      )
      expect(call).toBeTruthy()
      expect(JSON.parse(call[1].body)).toEqual({ body: 'the monorepo', to: ['cto'] })
    })
  })

  it('omits the to key when no addressee is picked', async () => {
    mockApi()
    renderWithProviders(<TaskScreen />)
    await openQuestions()
    fireEvent.changeText(screen.getByPlaceholderText(/Write a reply/), 'the monorepo')
    fireEvent.press(screen.getByText('Clarify'))
    await waitFor(() => {
      const call = (fetch as jest.Mock).mock.calls.find(([u]: [string]) =>
        String(u).includes('/v1/questions/1/reply'),
      )
      expect(call).toBeTruthy()
      expect(JSON.parse(call[1].body)).toEqual({ body: 'the monorepo' })
    })
  })

  it('keeps the final answer available to us', async () => {
    mockApi()
    renderWithProviders(<TaskScreen />)
    await openQuestions()
    expect(screen.getByText('Answer & close')).toBeTruthy()
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd mobile && npx jest app/task/task-thread.test.tsx`
Expected: FAIL — "Unable to find an element with text: you, reply-answer-orch, cto".

- [ ] **Step 3: Write minimal implementation**

Apply to `mobile/app/task/[id].tsx` exactly the edits Task 4 applied to the agent screen, with three differences:

1. The import path is `../../src/lib/threads` and `radius` must be added to the existing `import { colors, mono, radius } from '../../src/theme'` line if absent.
2. The "waiting for" fallback names the orchestrator, not an agent:

```tsx
        <Badge
          label={q.your_turn ? 'awaiting you' : `waiting for ${(q.waiting_on ?? []).map(participantLabel).join(', ') || 'orch'}`}
          fg={colors.amberDeep}
          bg={colors.amberBg}
        />
```

3. `AskQuestionSheet` gains the same picker so a new thread can address someone specific. It has no thread yet, so its options come from the task's sessions — out of scope here; instead pass no `to` and leave the sheet unchanged. (The picker on an existing thread is what acceptance criterion 4 asks for.)

Then at `mobile/app/task/[id].tsx:436`:

```ts
  const awaiting = open.filter((q) => q.your_turn)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd mobile && npx jest app/task/task-thread.test.tsx`
Expected: PASS, 8 tests.

- [ ] **Step 5: Commit**

```bash
git add mobile/app/task/[id].tsx mobile/app/task/task-thread.test.tsx
git commit -m "mobile: task threads show participants, addressees and your-turn"
```

---

### Task 6: Tab badge — resolved, no code change

**Files:** none.

**The conflict and its resolution.** Acceptance criterion 3 says "Tab badge counts `your_turn` threads". The only tab badge is Agents (`mobile/app/(tabs)/_layout.tsx:110`), fed by `awaitingUser()` (`mobile/src/lib/agents.ts:14`) summing `Agent.awaiting_user` from `GET /v1/agents`. That field is a server-side last-author heuristic (`internal/store/questions.go:246-260`), not participant-aware, and the agents-list payload carries neither `your_turn` nor `waiting_on`. Counting `your_turn` client-side would need an N+1 fetch of every agent's threads.

`reply-answer-orch` confirmed the blocker and chose the server-side repair: `awaiting_user` stops being a separate heuristic and comes to mean exactly "the human is in `waiting_on`" — a correctness fix to an existing field, not a new field. Subtask **#738** carries it.

Therefore: **the badge keeps reading `awaiting_user` and no mobile code changes.** The shape is already right; its participant-awareness lands under us when #738 merges. `countYourTurn` from Task 1 still earns its place — the two thread screens use it for their in-screen awaiting lists.

- [ ] **Step 1: Confirm nothing to do**

Run: `git diff --stat main -- "mobile/app/(tabs)/_layout.tsx"`
Expected: empty.

- [ ] **Step 2: Record the decision**

```bash
rocket task log 734 --kind decision "Tab badge stays on Agent.awaiting_user: participant-awareness is a server-side fix landing in subtask #738, not a client change. Confirmed with reply-answer-orch."
```

---

### Task 7: Full verification and the PR

**Files:** none — this task only runs and reports.

- [ ] **Step 1: Run the whole mobile suite**

Run: `cd mobile && npm test`
Expected: all suites pass. Capture the real output for the PR body — do not paraphrase it.

- [ ] **Step 2: Typecheck**

Run: `cd mobile && npm run typecheck`
Expected: no output, exit 0.

- [ ] **Step 3: Confirm the Go side is untouched**

Run: `git diff --stat main -- internal/ cmd/`
Expected: empty. This task changes no Go code, so the repo's known-flaky Go tests are irrelevant to it.

- [ ] **Step 4: Open the PR**

```bash
git push -u origin feature/reply-answer/mobile-participants
gh pr create --title "mobile: multi-participant question threads (#734)" --body "..."
```

The body must reference feature `reply-answer` and subtask #734, list the exact verification commands with their real output, and state the Task 6 outcome.

- [ ] **Step 5: Report to the orchestrator**

```bash
rocket send reply-answer-orch "#734 PR <url> — npm test green, typecheck clean. Tab badge: <outcome>."
```
