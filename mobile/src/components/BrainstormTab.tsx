// The task screen's Brainstorm tab (task #4901, spec §3.1), top to bottom:
// the storm's counters, the Problem doc, the storm questions in order, and
// the exit block — the pending gate with Go / Needs changes and the history.
import { useState } from 'react'
import { Pressable, StyleSheet, Text, TextInput, View } from 'react-native'
import { ApiError } from '../api/client'
import { useBrainstormStats, useDecideGate, useTaskDocs } from '../api/queries'
import type { BrainstormStorm, Question, TaskDoc, TaskDocKind, TaskGate } from '../api/types'
import { docAt, exitState, gateHistoryLabel, isBrainstorm, latestDoc } from '../lib/brainstorm'
import { ago } from '../lib/format'
import { colors, radius } from '../theme'
import { Markdown } from './Markdown'
import { QuestionCard } from './QuestionCard'
import { useToast } from './Toast'
import { Card, PrimaryButton } from './ui'

const COUNTERS: { key: keyof BrainstormStorm; label: string }[] = [
  { key: 'questions', label: 'Questions' },
  { key: 'accepted', label: 'Accepted' },
  { key: 'accepted_with_comment', label: 'with comment' },
  { key: 'corrected', label: 'Corrected' },
  { key: 'wrong_turn', label: 'Wrong turn' },
  { key: 'spec_changes', label: 'Spec changes' },
]

export function BrainstormTab({
  taskId,
  questions,
  gates,
  gatesError,
}: {
  taskId: number
  questions: Question[]
  /** Newest first. */
  gates: TaskGate[]
  /** Set when the gates failed to load — never shown as "waiting". */
  gatesError?: string
}) {
  const stats = useBrainstormStats(taskId, true)
  // The full history: a gate pins spec/plan versions a newer save may have replaced.
  const docs = useTaskDocs(taskId, true, true)
  const allDocs = docs.data ?? []
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
      </Card>

      <View>
        <Text style={styles.label}>PROBLEM</Text>
        <Card>
          {problem ? (
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
        <ExitBlock gates={gates} gatesError={gatesError} docs={allDocs} />
      </View>
    </View>
  )
}

function ExitBlock({ gates, gatesError, docs }: { gates: TaskGate[]; gatesError?: string; docs: TaskDoc[] }) {
  const history = gates.filter((g) => g.status !== 'pending')
  if (gatesError) {
    return (
      <Card>
        <Text style={{ fontSize: 13.5, color: colors.redFg }}>Could not load gates: {gatesError}</Text>
      </Card>
    )
  }
  const state = exitState(gates, docs)
  return (
    <Card style={{ gap: 12 }}>
      {state.kind === 'waiting_spec' ? <Text style={styles.state}>Waiting for spec</Text> : null}
      {state.kind === 'waiting_request' ? <Text style={styles.state}>Waiting for gate request</Text> : null}
      {state.kind === 'go' ? (
        <Text style={[styles.state, { color: colors.greenFg }]}>Go given on spec v{state.gate.spec_version}</Text>
      ) : null}
      {/* Keyed by gate: a new request starts with a clean comment box. */}
      {state.kind === 'pending' ? <PendingGate key={state.gate.id} gate={state.gate} docs={docs} /> : null}
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

function PendingGate({ gate, docs }: { gate: TaskGate; docs: TaskDoc[] }) {
  const decide = useDecideGate()
  const toast = useToast()
  const [comment, setComment] = useState('')
  const [open, setOpen] = useState<TaskDocKind | null>(null)
  const shown = open === 'spec' ? docAt(docs, 'spec', gate.spec_version) : open === 'plan' ? docAt(docs, 'plan', gate.plan_version) : undefined

  const send = (decision: 'go' | 'changes') =>
    decide.mutate(
      { id: gate.id, decision, comment: decision === 'changes' ? comment.trim() : '' },
      {
        onError: (e) => {
          if (e instanceof ApiError && e.status === 409) {
            // The spec moved on or someone already decided: the refetch shows the new state.
            setComment('')
            toast.show('Gate is no longer current')
          } else {
            toast.show((e as Error).message)
          }
        },
      },
    )

  const docLink = (kind: TaskDocKind, label: string) => (
    <Pressable onPress={() => setOpen((o) => (o === kind ? null : kind))} hitSlop={6}>
      <Text style={styles.link}>{label}</Text>
    </Pressable>
  )

  return (
    <View style={{ gap: 12 }}>
      <View style={styles.versions}>
        {docLink('spec', `Spec v${gate.spec_version}`)}
        {gate.plan_version != null ? (
          <>
            <Text style={styles.meta}>·</Text>
            {docLink('plan', `Plan v${gate.plan_version}`)}
          </>
        ) : null}
        <View style={{ flex: 1 }} />
        <Text style={styles.meta}>requested {ago(gate.requested_at)}</Text>
      </View>
      {open ? (
        <View style={styles.docBox}>
          {shown ? <Markdown>{shown.body}</Markdown> : <Text style={styles.empty}>This version is not available.</Text>}
        </View>
      ) : null}
      <TextInput
        value={comment}
        onChangeText={setComment}
        placeholder="What needs to change?"
        placeholderTextColor={colors.textFaint}
        multiline
        style={styles.input}
      />
      <View style={{ flexDirection: 'row', gap: 9 }}>
        <Pressable
          disabled={decide.isPending || !comment.trim()}
          onPress={() => send('changes')}
          style={[styles.changesBtn, (decide.isPending || !comment.trim()) && { opacity: 0.4 }]}
        >
          <Text style={{ color: colors.redFg, fontSize: 13, fontWeight: '600' }}>Needs changes</Text>
        </Pressable>
        <PrimaryButton label="Go" disabled={decide.isPending} onPress={() => send('go')} style={{ flex: 1 }} />
      </View>
    </View>
  )
}

const styles = StyleSheet.create({
  counters: { flexDirection: 'row', flexWrap: 'wrap', rowGap: 12, padding: 14 },
  counter: { width: '33.33%', alignItems: 'center' },
  counterValue: { fontSize: 20, fontWeight: '700', color: colors.text },
  counterLabel: { fontSize: 11, color: colors.textDim, marginTop: 2 },
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
