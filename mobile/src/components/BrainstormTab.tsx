// The task screen's Brainstorm tab (task #4901, spec §3.1), top to bottom:
// the storm's counters, the Problem doc, the storm questions in order, and
// the exit block — the pending gate with Go / Needs changes and the history.
import { useState } from 'react'
import { Pressable, StyleSheet, Text, TextInput, View } from 'react-native'
import { ApiError } from '../api/client'
import { useBrainstormStats, useDecideGate, useTaskDocs } from '../api/queries'
import type { BrainstormStorm, Question, TaskDoc, TaskDocKind, TaskGate } from '../api/types'
import {
  docAt,
  exitState,
  gateHistoryLabel,
  gateState,
  isBrainstorm,
  latestDoc,
  participantLabel,
  stormWho,
} from '../lib/brainstorm'
import { ago } from '../lib/format'
import { colors, radius } from '../theme'
import { Markdown } from './Markdown'
import { QuestionCard } from './QuestionCard'
import { Card, PrimaryButton } from './ui'

const COUNTERS: { key: keyof BrainstormStorm; label: string }[] = [
  { key: 'questions', label: 'Questions' },
  { key: 'accepted', label: 'Accepted' },
  { key: 'accepted_with_comment', label: 'with comment' },
  { key: 'corrected', label: 'Corrected' },
  { key: 'wrong_turn', label: 'Wrong turn' },
  { key: 'spec_changes', label: 'Правок до Go' },
]

export function BrainstormTab({
  taskId,
  questions,
  gates,
  gatesError,
  gatesLoading,
}: {
  taskId: number
  questions: Question[]
  /** Newest first. */
  gates: TaskGate[]
  /** Set when the gates failed to load — never shown as "waiting". */
  gatesError?: string
  gatesLoading?: boolean
}) {
  const stats = useBrainstormStats(taskId, true)
  // The full history: a gate pins spec/plan versions a newer save may have replaced.
  const docs = useTaskDocs(taskId, true, true)
  const allDocs = docs.data ?? []
  const docsError = docs.isError ? `Could not load documents: ${(docs.error as Error).message}` : undefined
  const problem = latestDoc(allDocs, 'problem')
  const storm = questions.filter(isBrainstorm).sort((a, b) => a.ordinal - b.ordinal)

  return (
    <View style={{ gap: 14 }}>
      <Card style={styles.counters}>
        {COUNTERS.map((c) => (
          <View key={c.key} style={styles.counter}>
            <Text testID={`stat-${c.key}`} style={styles.counterValue}>
              {stats.data ? String(stats.data[c.key] ?? 0) : '—'}
            </Text>
            <Text style={styles.counterLabel}>{c.label}</Text>
          </View>
        ))}
        <View style={styles.who}>
          <Text testID="stat-who" style={styles.whoText}>
            {`Кто штормил: ${stormWho(stats.data?.answered_by ?? [])}`}
          </Text>
          <Text testID="stat-gate" style={styles.whoText}>
            {`Гейт: ${
              stats.data
                ? gateState({
                    go_at: stats.data.go_at,
                    spec_changes: stats.data.spec_changes,
                    has_gate: stats.data.has_gate ?? false,
                  })
                : '—'
            }`}
          </Text>
          {/* The totals above are the whole storm; split only when several took part. */}
          {(stats.data?.by_answerer ?? []).length > 1
            ? stats.data!.by_answerer.map((a) => (
                <Text key={a.answered_by} testID="by-answerer" style={styles.answerer}>
                  {`${participantLabel(a.answered_by)}: answered ${a.answered} · accepted ${a.accepted} (with comment ${a.accepted_with_comment}) · corrected ${a.corrected} · wrong turn ${a.wrong_turn}`}
                </Text>
              ))
            : null}
        </View>
      </Card>

      <View>
        <Text style={styles.label}>PROBLEM</Text>
        <Card>
          {docsError ? (
            <Text style={styles.error}>{docsError}</Text>
          ) : docs.isPending ? (
            <Text style={styles.empty}>Loading…</Text>
          ) : problem ? (
            <>
              <Text style={styles.meta}>
                v{problem.version} · {ago(problem.created_at)}
              </Text>
              <Markdown>{problem.body}</Markdown>
            </>
          ) : (
            <Text style={styles.empty}>No problem statement yet.</Text>
          )}
        </Card>
      </View>

      <View style={{ gap: 14 }}>
        <Text style={styles.label}>STORM QUESTIONS</Text>
        {storm.map((q) => (
          <QuestionCard key={q.id} q={q} />
        ))}
        {storm.length === 0 ? <Text style={styles.empty}>No storm questions yet.</Text> : null}
      </View>

      <View>
        <Text style={styles.label}>EXIT</Text>
        <ExitBlock
          gates={gates}
          gatesError={gatesError}
          gatesLoading={gatesLoading}
          docs={allDocs}
          docsLoading={docs.isPending}
          docsError={docsError}
        />
      </View>
    </View>
  )
}

/** Why a decision was refused, in words a human can act on (same as the web tab). */
function decideError(err: unknown): string {
  if (err instanceof ApiError && err.status === 409) {
    return 'This gate is no longer current: the spec changed after it was requested, or it was already decided. Refreshed.'
  }
  return err instanceof Error ? `Failed: ${err.message}` : 'Failed'
}

function ExitBlock({
  gates,
  gatesError,
  gatesLoading,
  docs,
  docsLoading,
  docsError,
}: {
  gates: TaskGate[]
  gatesError?: string
  gatesLoading?: boolean
  docs: TaskDoc[]
  docsLoading: boolean
  docsError?: string
}) {
  const decide = useDecideGate()
  const [changesOpen, setChangesOpen] = useState(false)
  const [comment, setComment] = useState('')
  const [shown, setShown] = useState<TaskDocKind | null>(null)
  const state = exitState(gates, latestDoc(docs, 'spec')?.version)
  const pending = state.kind === 'pending' ? state.gate : undefined
  // Oldest first, like the web tab.
  const history = gates.filter((g) => g.status !== 'pending').reverse()

  // A new pending gate is a new question: drop the old answer-in-progress and
  // the old refusal. Until then a refused comment stays, with its error.
  const [seenPendingId, setSeenPendingId] = useState(pending?.id)
  if (seenPendingId !== pending?.id) {
    setSeenPendingId(pending?.id)
    setShown(null)
    setChangesOpen(false)
    setComment('')
    decide.reset()
  }

  const send = (decision: 'go' | 'changes') => {
    if (!pending) return
    decide.mutate(
      { id: pending.id, decision, comment: decision === 'changes' ? comment.trim() : '' },
      {
        onSuccess: () => {
          setChangesOpen(false)
          setComment('')
        },
      },
    )
  }

  let body: React.ReactNode
  if (gatesError) {
    body = <Text style={styles.error}>Could not load gates: {gatesError}</Text>
  } else if (gatesLoading) {
    body = <Text style={styles.empty}>Loading…</Text>
  } else if (pending) {
    const shownDoc =
      shown === 'spec'
        ? docAt(docs, 'spec', pending.spec_version, pending.requested_at)
        : shown === 'plan'
          ? docAt(docs, 'plan', pending.plan_version, pending.requested_at)
          : undefined
    const docLink = (kind: TaskDocKind, label: string) => (
      <Pressable onPress={() => setShown((o) => (o === kind ? null : kind))} hitSlop={6}>
        <Text style={styles.link}>{label}</Text>
      </Pressable>
    )
    body = (
      <View style={{ gap: 12 }}>
        <View style={styles.versions}>
          {docLink('spec', `Spec v${pending.spec_version}`)}
          {pending.plan_version != null ? (
            <>
              <Text style={styles.meta}>·</Text>
              {docLink('plan', `Plan v${pending.plan_version}`)}
            </>
          ) : null}
          <View style={{ flex: 1 }} />
          <Text style={styles.meta}>requested {ago(pending.requested_at)}</Text>
        </View>
        {shown ? (
          <View style={styles.docBox}>
            {shownDoc ? (
              <Markdown>{shownDoc.body}</Markdown>
            ) : docsError ? (
              <Text style={styles.error}>{docsError}</Text>
            ) : docsLoading ? (
              <Text style={styles.empty}>Loading…</Text>
            ) : (
              <Text style={styles.empty}>This document version is not available.</Text>
            )}
          </View>
        ) : null}
        <View style={{ flexDirection: 'row', gap: 9 }}>
          <Pressable
            disabled={decide.isPending}
            onPress={() => setChangesOpen((o) => !o)}
            style={[styles.changesBtn, decide.isPending && { opacity: 0.4 }]}
          >
            <Text style={styles.changesText}>Needs changes</Text>
          </Pressable>
          <PrimaryButton label="Go" disabled={decide.isPending} onPress={() => send('go')} style={{ flex: 1 }} />
        </View>
        {changesOpen ? (
          <View style={{ gap: 9 }}>
            <TextInput
              value={comment}
              onChangeText={setComment}
              placeholder="What should change in the spec?"
              placeholderTextColor={colors.textFaint}
              multiline
              autoFocus
              style={styles.input}
            />
            <Pressable
              disabled={decide.isPending || !comment.trim()}
              onPress={() => send('changes')}
              style={[styles.changesBtn, styles.sendBtn, (decide.isPending || !comment.trim()) && { opacity: 0.4 }]}
            >
              <Text style={styles.changesText}>Send changes</Text>
            </Pressable>
          </View>
        ) : null}
      </View>
    )
  } else if (state.kind === 'go') {
    const g = state.gate
    body = (
      <Text style={[styles.state, { color: colors.greenFg }]}>
        Go given on spec v{g.spec_version}
        {g.plan_version != null ? ` · plan v${g.plan_version}` : ''}
      </Text>
    )
  } else if (docsError) {
    body = <Text style={styles.error}>{docsError}</Text>
  } else if (docsLoading) {
    body = <Text style={styles.empty}>Loading…</Text>
  } else {
    body = (
      <Text style={styles.state}>{state.kind === 'waiting_request' ? 'Waiting for gate request' : 'Waiting for spec'}</Text>
    )
  }

  return (
    <Card style={{ gap: 12 }}>
      {body}
      {decide.isError ? <Text style={styles.error}>{decideError(decide.error)}</Text> : null}
      {history.length > 0 ? (
        <View style={{ gap: 6 }}>
          <Text style={styles.label}>HISTORY</Text>
          {history.map((g) => (
            <View key={g.id} style={styles.historyRow}>
              <Text style={styles.historyText}>{gateHistoryLabel(g)}</Text>
              <Text style={styles.meta}>{ago(g.decided_at ?? g.requested_at)}</Text>
            </View>
          ))}
        </View>
      ) : null}
    </Card>
  )
}

const styles = StyleSheet.create({
  counters: { flexDirection: 'row', flexWrap: 'wrap', rowGap: 12, padding: 14 },
  counter: { width: '33.33%', alignItems: 'center' },
  counterValue: { fontSize: 20, fontWeight: '700', color: colors.text },
  counterLabel: { fontSize: 11, color: colors.textDim, marginTop: 2 },
  who: { width: '100%', gap: 4, paddingTop: 10, borderTopWidth: 1, borderTopColor: colors.border },
  whoText: { fontSize: 13, fontWeight: '600', color: colors.textMid },
  answerer: { fontSize: 12, color: colors.textDim },
  label: { fontSize: 10.5, fontWeight: '600', color: colors.textFaint, letterSpacing: 0.5, marginBottom: 8 },
  meta: { fontSize: 11, color: colors.textFaint },
  empty: { fontSize: 13, color: colors.textFaint },
  state: { fontSize: 14, fontWeight: '600', color: colors.textMid },
  versions: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  link: { fontSize: 14, fontWeight: '600', color: colors.accent },
  docBox: {
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: radius.lg,
    padding: 12,
    backgroundColor: colors.cardAlt,
  },
  input: {
    minHeight: 60,
    padding: 12,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: radius.lg,
    fontSize: 14,
    color: colors.text,
    backgroundColor: colors.card,
    textAlignVertical: 'top',
  },
  error: { fontSize: 13, lineHeight: 19, color: colors.redFg },
  changesText: { color: colors.redFg, fontSize: 13, fontWeight: '600' },
  sendBtn: { flex: 0, height: 42 },
  changesBtn: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    borderRadius: radius.lg,
    borderWidth: 1,
    borderColor: colors.redBorder,
    backgroundColor: colors.redBgSoft,
  },
  historyRow: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  historyText: { flex: 1, fontSize: 13, color: colors.textBody },
})
