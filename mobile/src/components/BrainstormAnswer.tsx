// The brainstorm-only parts of a thread card (task #4901, spec §2.2/§3.1):
// options with the recommendation starred plus an optional comment, and the
// answer summary with its outcome, which the human may correct.
import { useState } from 'react'
import { Pressable, StyleSheet, Text, TextInput, View } from 'react-native'
import { useSetQuestionOutcome } from '../api/queries'
import type { BrainstormFields, BrainstormOutcome } from '../api/types'
import { OUTCOME_LABEL, OUTCOMES, chosenLabel, isRecommended } from '../lib/brainstorm'
import { colors, radius } from '../theme'
import { useToast } from './Toast'
import { Badge } from './ui'

/**
 * One tap answers with an option; whatever is typed in the comment field
 * goes along with it. `onChoose` gets the 1-based option and the trimmed
 * comment (`''` when none).
 */
export function StormOptions({
  q,
  disabled,
  onChoose,
  onInputFocus,
  onInputBlur,
}: {
  q: { options?: string[] } & Pick<BrainstormFields, 'recommended_option'>
  disabled?: boolean
  onChoose: (choose: number, comment: string, label: string) => void
  onInputFocus?: () => void
  onInputBlur?: () => void
}) {
  const [comment, setComment] = useState('')
  return (
    <View style={{ marginBottom: 12 }}>
      <View style={styles.optionRow}>
        {(q.options ?? []).map((label, i) => {
          const rec = isRecommended(q, i)
          return (
            <Pressable
              key={i}
              testID={`option-${i + 1}`}
              disabled={disabled}
              style={[styles.optionBtn, rec ? styles.optionRec : null]}
              onPress={() => {
                onChoose(i + 1, comment.trim(), label)
                setComment('')
              }}
            >
              {rec ? <Text style={styles.recLabel}>★ Recommended</Text> : null}
              <Text style={styles.optionText}>{label}</Text>
            </Pressable>
          )
        })}
      </View>
      <TextInput
        value={comment}
        onChangeText={setComment}
        onFocus={onInputFocus}
        onBlur={onInputBlur}
        placeholder="Comment (optional)"
        placeholderTextColor={colors.textFaint}
        multiline
        style={styles.comment}
      />
    </View>
  )
}

const OUTCOME_COLORS: Record<BrainstormOutcome, { fg: string; bg: string }> = {
  accepted: { fg: colors.greenFg, bg: colors.greenBg },
  corrected: { fg: colors.amberDeep, bg: colors.amberBg },
  wrong_turn: { fg: colors.redFg, bg: colors.redBg },
}

/** Badge text of an outcome; an accepted answer with a comment says so. */
export function outcomeText(q: Pick<BrainstormFields, 'outcome' | 'answer_comment'>): string {
  if (!q.outcome) return ''
  if (q.outcome === 'accepted' && q.answer_comment?.trim()) return 'Accepted with comment'
  return OUTCOME_LABEL[q.outcome]
}

/** What the human answered on a closed brainstorm thread, and how it scored. */
export function StormAnswerSummary({
  q,
}: {
  q: { id: number; options?: string[] } & BrainstormFields
}) {
  const setOutcome = useSetQuestionOutcome()
  const toast = useToast()
  const [menu, setMenu] = useState(false)
  if (!q.outcome) return null
  const chosen = chosenLabel(q)
  const tone = OUTCOME_COLORS[q.outcome]

  return (
    <View style={styles.summary}>
      <Text style={styles.chose}>{chosen ? `Chose: ${chosen}` : 'Own answer'}</Text>
      {q.answer_comment?.trim() ? <Text style={styles.commentText}>{q.answer_comment}</Text> : null}
      <View style={styles.badges}>
        <Pressable testID="outcome-change" onPress={() => setMenu((m) => !m)} hitSlop={6}>
          <Badge label={outcomeText(q)} fg={tone.fg} bg={tone.bg} />
        </Pressable>
        {q.outcome_overridden ? <Text style={styles.meta}>changed by you</Text> : null}
        {q.answer_source === 'terminal' ? (
          <Badge label="From terminal" fg={colors.slateFg} bg={colors.slateBg} />
        ) : null}
      </View>
      {/* Inline, not a sheet: correcting the score is a one-tap aside. */}
      {menu ? (
        <View style={styles.badges}>
          {OUTCOMES.filter((o) => o !== q.outcome).map((o) => (
            <Pressable
              key={o}
              style={styles.markBtn}
              disabled={setOutcome.isPending}
              onPress={() =>
                setOutcome.mutate(
                  { id: q.id, outcome: o },
                  { onSuccess: () => setMenu(false), onError: (e) => toast.show((e as Error).message) },
                )
              }
            >
              <Text style={styles.markText}>Mark as {OUTCOME_LABEL[o]}</Text>
            </Pressable>
          ))}
        </View>
      ) : null}
    </View>
  )
}

const styles = StyleSheet.create({
  optionRow: { flexDirection: 'row', flexWrap: 'wrap', gap: 8, marginBottom: 10 },
  optionBtn: {
    paddingVertical: 9,
    paddingHorizontal: 14,
    borderRadius: radius.lg,
    borderWidth: 1,
    borderColor: colors.border,
    backgroundColor: colors.cardAlt,
  },
  optionRec: { borderColor: colors.amberDeep, backgroundColor: colors.amberBgSoft },
  recLabel: { fontSize: 10.5, fontWeight: '700', color: colors.amberDeep, marginBottom: 2 },
  optionText: { fontSize: 13, fontWeight: '600', color: colors.text },
  comment: {
    minHeight: 44,
    padding: 10,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: radius.lg,
    fontSize: 13.5,
    color: colors.text,
    backgroundColor: colors.card,
    textAlignVertical: 'top',
  },
  summary: {
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: radius.lg,
    backgroundColor: colors.cardAlt,
    padding: 12,
    marginBottom: 14,
    gap: 6,
  },
  chose: { fontSize: 13.5, fontWeight: '600', color: colors.text },
  commentText: { fontSize: 13.5, lineHeight: 20, color: colors.textBody },
  badges: { flexDirection: 'row', alignItems: 'center', gap: 8, flexWrap: 'wrap' },
  meta: { fontSize: 11.5, color: colors.textFaint },
  markBtn: {
    paddingVertical: 6,
    paddingHorizontal: 10,
    borderRadius: radius.chip,
    borderWidth: 1,
    borderColor: colors.border,
    backgroundColor: colors.card,
  },
  markText: { fontSize: 12, fontWeight: '600', color: colors.textMid },
})
