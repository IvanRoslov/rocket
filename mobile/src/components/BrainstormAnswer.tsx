// The brainstorm-only parts of a thread card (task #4901, spec §2.2/§3.1):
// options with the recommendation starred plus an optional comment, and the
// answer summary with its outcome, which the human may correct.
import { useState } from 'react'
import { Pressable, StyleSheet, Text, View } from 'react-native'
import { useSetQuestionOutcome } from '../api/queries'
import type { BrainstormFields, BrainstormOutcome } from '../api/types'
import { OUTCOME_LABEL, OUTCOMES, chosenLabel, isRecommended } from '../lib/brainstorm'
import { isHuman } from '../lib/threads'
import { colors, radius } from '../theme'
import { useToast } from './Toast'
import { Badge } from './ui'

/** Placeholder of a storm card's one text box: a comment for an option, or the whole answer. */
export const STORM_INPUT_PLACEHOLDER = 'Comment or your own answer…'

/**
 * The option buttons of an open storm question, the recommendation starred.
 * The card owns the one text box: what is typed there goes along with a
 * tapped option as its comment, so nothing typed is lost.
 */
export function StormOptions({
  q,
  disabled,
  onChoose,
}: {
  q: { options?: string[] } & Pick<BrainstormFields, 'recommended_option'>
  disabled?: boolean
  /** `choose` is 1-based. */
  onChoose: (choose: number, label: string) => void
}) {
  return (
    <View style={styles.optionRow}>
      {(q.options ?? []).map((label, i) => {
        const rec = isRecommended(q, i)
        return (
          <Pressable
            key={i}
            testID={`option-${i + 1}`}
            disabled={disabled}
            style={[styles.optionBtn, rec ? styles.optionRec : null]}
            onPress={() => onChoose(i + 1, label)}
          >
            {rec ? <Text style={styles.recLabel}>★ Recommended</Text> : null}
            <Text style={styles.optionText}>{label}</Text>
          </Pressable>
        )
      })}
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
        {!isHuman(q.answered_by) ? <Text style={styles.meta}>{`answered by ${q.answered_by}`}</Text> : null}
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
  optionRow: { flexDirection: 'row', flexWrap: 'wrap', gap: 8, marginBottom: 14 },
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
