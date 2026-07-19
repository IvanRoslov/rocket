import { useState } from 'react'
import { Pressable, StyleSheet, Text, TextInput, View } from 'react-native'
import { useQuizAnswer } from '../api/queries'
import type { ClosedQuizEcho, PendingQuiz } from '../api/types'
import { buildQuizAnswers, emptySelection, toggleOption, type QuizSelection } from '../lib/quiz'
import { colors, radius } from '../theme'
import { useToast } from './Toast'
import { Badge, PrimaryButton } from './ui'

/**
 * Interactive bubble for a live AskUserQuestion quiz (docs/13-chat.md).
 * Answers go to POST /v1/sessions/{id}/quiz/answer; after 202 the card
 * shows "typing the answer…" until the daemon clears pending_quiz and the
 * parent stops rendering it.
 */
export function PendingQuizCard({ sessionId, quiz }: { sessionId: string; quiz: PendingQuiz }) {
  const answer = useQuizAnswer()
  const toast = useToast()
  const [selections, setSelections] = useState<QuizSelection[]>(() => quiz.questions.map(emptySelection))
  const [answering, setAnswering] = useState(false)

  const setSel = (qi: number, next: QuizSelection) =>
    setSelections((prev) => prev.map((s, i) => (i === qi ? next : s)))

  const submit = () => {
    const built = buildQuizAnswers(quiz.questions, selections)
    if (!built.ok) {
      toast.show(built.error)
      return
    }
    answer.mutate(
      { sessionId, answers: built.answers },
      {
        onSuccess: () => setAnswering(true),
        onError: (e) => toast.show((e as Error).message),
      },
    )
  }

  return (
    <View style={styles.card}>
      <View style={styles.head}>
        <Badge label="quiz" fg={colors.indigoFg} bg={colors.indigoBg} />
        <Text style={styles.headText}>The agent is asking — pick your answers</Text>
      </View>

      {quiz.questions.map((q, qi) => {
        const sel = selections[qi] ?? emptySelection()
        return (
          <View key={qi} style={styles.question}>
            <View style={{ flexDirection: 'row', alignItems: 'center', gap: 7, marginBottom: 6 }}>
              {q.header ? <Badge label={q.header} fg={colors.slateFg} bg={colors.slateBg} /> : null}
              {q.multi_select ? (
                <Text style={{ fontSize: 10.5, color: colors.textFaint }}>multiple choice</Text>
              ) : null}
            </View>
            <Text style={styles.questionText}>{q.question}</Text>
            <View style={{ gap: 6 }}>
              {q.options.map((o, oi) => {
                const on = !sel.useText && sel.indices.includes(oi)
                return (
                  <Pressable
                    key={oi}
                    disabled={answering}
                    onPress={() => setSel(qi, toggleOption(sel, oi, q.multi_select))}
                    style={[styles.option, on && styles.optionOn]}
                  >
                    <Text style={[styles.optionMark, on && { color: colors.accent }]}>
                      {q.multi_select ? (on ? '☑' : '☐') : on ? '◉' : '○'}
                    </Text>
                    <View style={{ flex: 1 }}>
                      <Text style={[styles.optionLabel, on && { color: colors.indigoFg }]}>{o.label}</Text>
                      {o.description ? <Text style={styles.optionDesc}>{o.description}</Text> : null}
                    </View>
                  </Pressable>
                )
              })}
              <Pressable
                disabled={answering}
                onPress={() => setSel(qi, { indices: [], text: sel.text, useText: true })}
                style={[styles.option, sel.useText && styles.optionOn]}
              >
                <Text style={[styles.optionMark, sel.useText && { color: colors.accent }]}>
                  {sel.useText ? '◉' : '○'}
                </Text>
                {sel.useText ? (
                  <TextInput
                    style={styles.otherInput}
                    placeholder="Type your own answer…"
                    placeholderTextColor={colors.textFaint}
                    value={sel.text}
                    onChangeText={(t) => setSel(qi, { indices: [], text: t, useText: true })}
                    autoFocus
                    multiline
                  />
                ) : (
                  <Text style={styles.optionLabel}>Other — type your own</Text>
                )}
              </Pressable>
            </View>
          </View>
        )
      })}

      <PrimaryButton
        label={answering ? 'Typing the answer in the terminal…' : answer.isPending ? 'Sending…' : 'Answer'}
        disabled={answering || answer.isPending}
        onPress={submit}
        style={{ marginTop: 4 }}
      />
      <Text style={styles.note}>Regular messages are paused until the quiz is answered.</Text>
    </View>
  )
}

/** Closed quiz round rendered from a quiz_answer transcript entry. */
export function ClosedQuizCard({ echo, fallback }: { echo?: ClosedQuizEcho; fallback: string }) {
  const questions = echo?.questions ?? []
  const answers = echo?.answers ?? {}
  return (
    <View style={[styles.card, { borderColor: colors.border, backgroundColor: colors.card }]}>
      <View style={styles.head}>
        <Badge label="quiz" fg={colors.slateFg} bg={colors.slateBg} />
        <Text style={styles.headText}>answered</Text>
      </View>
      {questions.length > 0 ? (
        questions.map((q, i) => {
          const chosen = answers[q.question]
          return (
            <View key={i} style={{ marginBottom: 10 }}>
              <Text style={[styles.questionText, { marginBottom: 5 }]}>{q.question}</Text>
              <View style={{ gap: 4 }}>
                {(q.options ?? []).map((o, oi) => {
                  const on = chosen !== undefined && (chosen === o.label || chosen.includes(o.label))
                  return (
                    <View key={oi} style={[styles.option, on && styles.optionOn, { paddingVertical: 6 }]}>
                      <Text style={[styles.optionMark, on && { color: colors.accent }]}>{on ? '◉' : '○'}</Text>
                      <Text style={[styles.optionLabel, on && { color: colors.indigoFg }]}>{o.label}</Text>
                    </View>
                  )
                })}
                {chosen !== undefined && !(q.options ?? []).some((o) => chosen.includes(o.label)) ? (
                  <View style={[styles.option, styles.optionOn, { paddingVertical: 6 }]}>
                    <Text style={[styles.optionMark, { color: colors.accent }]}>◉</Text>
                    <Text style={[styles.optionLabel, { color: colors.indigoFg }]}>{chosen}</Text>
                  </View>
                ) : null}
              </View>
            </View>
          )
        })
      ) : (
        <Text style={{ fontSize: 13, lineHeight: 19, color: colors.textMid }}>{fallback}</Text>
      )}
    </View>
  )
}

const styles = StyleSheet.create({
  card: {
    borderWidth: 1.5,
    borderColor: colors.indigoBorder,
    backgroundColor: '#fbfbff',
    borderRadius: radius.xxl,
    padding: 14,
    marginVertical: 4,
  },
  head: { flexDirection: 'row', alignItems: 'center', gap: 8, marginBottom: 10 },
  headText: { fontSize: 12, color: colors.textDim, flex: 1 },
  question: { marginBottom: 14 },
  questionText: { fontSize: 14.5, fontWeight: '600', lineHeight: 20, color: colors.text, marginBottom: 8 },
  option: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 9,
    borderWidth: 1,
    borderColor: colors.border,
    backgroundColor: colors.card,
    borderRadius: radius.md + 1,
    padding: 10,
  },
  optionOn: { borderColor: colors.indigoBorder, backgroundColor: colors.indigoBg },
  optionMark: { fontSize: 15, color: colors.textFaint, width: 18, textAlign: 'center' },
  optionLabel: { fontSize: 13.5, fontWeight: '600', color: colors.text },
  optionDesc: { fontSize: 12, color: colors.textDim, marginTop: 2 },
  otherInput: { flex: 1, fontSize: 13.5, color: colors.text, padding: 0, minHeight: 20 },
  note: { fontSize: 11.5, color: colors.textFaint, textAlign: 'center', marginTop: 8 },
})
