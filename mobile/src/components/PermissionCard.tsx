import { useState } from 'react'
import { Pressable, ScrollView, StyleSheet, Text, View } from 'react-native'
import { useSessionEvent } from '../api/events'
import { useQuizAnswer } from '../api/queries'
import type { PendingQuiz, PermissionEcho } from '../api/types'
import {
  ESC_INDEX,
  permissionErrorText,
  resolvedPermissionLine,
  splitPermissionQuestion,
  UNCONFIRMED_TEXT,
} from '../lib/permission'
import { colors, mono, radius } from '../theme'
import { Badge, MonoText } from './ui'

/**
 * A Claude Code permission dialog waiting in the agent's terminal
 * (pending_quiz with source="permission", docs/13-chat.md). One tap answers:
 * the daemon presses that option's digit — or Escape for the trailing «Esc»
 * button — so there is no confirm step and no free-text row.
 *
 * After 202 the buttons stay locked until the daemon clears pending_quiz and
 * the parent unmounts the card; a changed dialog (409 prompt_changed) or an
 * unconfirmed answer unlocks them with an explanation.
 */
export function PendingPermissionCard({ sessionId, quiz }: { sessionId: string; quiz: PendingQuiz }) {
  const answer = useQuizAnswer()
  const [sent, setSent] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useSessionEvent(sessionId, 'session.quiz_answer_unconfirmed', () => {
    setSent(false)
    setError(UNCONFIRMED_TEXT)
  })

  const q = quiz.questions[0]
  const { title, context } = splitPermissionQuestion(q?.question ?? '')
  const options = q?.options ?? []
  const locked = sent || answer.isPending

  const press = (index: number) => {
    setError(null)
    answer.mutate(
      { sessionId, answers: [{ question_index: 0, option_indices: [index] }] },
      {
        onSuccess: () => setSent(true),
        onError: (e) => setError(permissionErrorText(e)),
      },
    )
  }

  const button = (label: string, index: number, secondary = false) => (
    <Pressable
      key={index}
      accessibilityRole="button"
      accessibilityLabel={label}
      disabled={locked}
      onPress={() => press(index)}
      style={({ pressed }) => [
        styles.option,
        secondary && styles.optionSecondary,
        pressed && styles.optionPressed,
        locked && styles.optionLocked,
      ]}
    >
      <Text style={[styles.optionLabel, secondary && styles.optionLabelSecondary]}>{label}</Text>
    </Pressable>
  )

  return (
    <View style={styles.card}>
      <View style={styles.head}>
        <Badge label="Разрешение" fg={colors.amberDeep} bg={colors.amberBg} />
        <Text style={styles.headText}>The agent is waiting in its terminal</Text>
      </View>

      {title ? <Text style={styles.title}>{title}</Text> : null}
      {context ? (
        <View style={styles.mono}>
          <MonoText style={styles.monoText}>{context}</MonoText>
        </View>
      ) : null}

      {options.length === 0 && quiz.raw ? (
        <ScrollView style={[styles.mono, styles.raw]} nestedScrollEnabled>
          <ScrollView horizontal>
            <MonoText style={styles.monoText}>{quiz.raw}</MonoText>
          </ScrollView>
        </ScrollView>
      ) : null}
      {/* Esc is always there: some options never make it into the list
          (ExitPlanMode's "tell Claude what to change" is a text field), and
          Esc is then the only way to say no — feedback follows as a chat
          message. */}
      <View style={{ gap: 8 }}>
        {options.map((o, i) => button(o.label, i))}
        {button('Esc', ESC_INDEX, options.length > 0)}
      </View>

      {error ? (
        <Text style={styles.error}>{error}</Text>
      ) : locked ? (
        <Text style={styles.note}>Отправляю ответ в терминал…</Text>
      ) : null}
    </View>
  )
}

/** A resolved permission dialog from the daemon's log (chat entry role="permission"). */
export function ResolvedPermissionCard({ permission, fallback }: { permission?: PermissionEcho; fallback: string }) {
  return (
    <View style={styles.resolved}>
      <Text style={styles.resolvedText}>
        {permission ? resolvedPermissionLine(permission) : `Разрешение: ${fallback}`}
      </Text>
    </View>
  )
}

const styles = StyleSheet.create({
  card: {
    borderWidth: 1.5,
    borderColor: colors.amberBorder,
    backgroundColor: colors.amberBgSoft,
    borderRadius: radius.xxl,
    padding: 14,
    marginVertical: 4,
  },
  head: { flexDirection: 'row', alignItems: 'center', gap: 8, marginBottom: 10 },
  headText: { fontSize: 12, color: colors.textDim, flex: 1 },
  title: { fontSize: 14.5, fontWeight: '600', lineHeight: 20, color: colors.text, marginBottom: 8 },
  mono: {
    borderWidth: 1,
    borderColor: colors.border,
    backgroundColor: colors.card,
    borderRadius: radius.md,
    padding: 9,
    marginBottom: 10,
  },
  raw: { maxHeight: 220 },
  monoText: { fontFamily: mono, fontSize: 11.5, lineHeight: 16, color: colors.textMid },
  option: {
    minHeight: 48,
    justifyContent: 'center',
    borderWidth: 1,
    borderColor: colors.border,
    backgroundColor: colors.card,
    borderRadius: radius.lg,
    paddingHorizontal: 14,
    paddingVertical: 10,
  },
  optionSecondary: { minHeight: 44, alignItems: 'center', backgroundColor: 'transparent' },
  optionLabelSecondary: { fontWeight: '500', color: colors.textDim },
  optionPressed: { backgroundColor: colors.amberBg, borderColor: colors.amberBorder },
  optionLocked: { opacity: 0.55 },
  optionLabel: { fontSize: 14, fontWeight: '600', color: colors.text },
  note: { fontSize: 11.5, color: colors.textFaint, textAlign: 'center', marginTop: 10 },
  error: { fontSize: 12.5, fontWeight: '600', color: colors.redFg, textAlign: 'center', marginTop: 10 },
  resolved: {
    alignSelf: 'flex-start',
    maxWidth: '94%',
    borderWidth: 1,
    borderColor: colors.amberBorder,
    backgroundColor: colors.amberBgSoft,
    borderRadius: radius.lg,
    paddingHorizontal: 11,
    paddingVertical: 7,
    marginVertical: 3,
  },
  resolvedText: { fontSize: 12.5, color: colors.amberDeep, fontWeight: '600' },
})
