import { router } from 'expo-router'
import { useState } from 'react'
import { Pressable, StyleSheet, Text, TextInput, View } from 'react-native'
import type { ThreadAnswer } from '../api/queries'
import type { ThreadInboxEntry } from '../api/types'
import { isBrainstorm } from '../lib/brainstorm'
import { threadSource } from '../lib/questions'
import { participantLabel } from '../lib/threads'
import { colors, radius } from '../theme'
import { STORM_INPUT_PLACEHOLDER, StormOptions } from './BrainstormAnswer'
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
  onInputFocus,
  onInputBlur,
  busy,
}: {
  thread: ThreadInboxEntry
  onAnswer: (thread: ThreadInboxEntry, answer: ThreadAnswer, label: string) => void
  /** The answer input gained / lost focus — lets the screen keep it above the keyboard. */
  onInputFocus?: () => void
  onInputBlur?: () => void
  /** The card's answer is in flight (Undo window or POST): options are disabled. */
  busy?: boolean
}) {
  const [text, setText] = useState('')
  const source = threadSource(thread)
  const options = thread.options ?? []
  const actionable = thread.your_turn
  const storm = isBrainstorm(thread)

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
          {/* A storm question stars the recommendation; the typed text rides along as the comment. */}
          {storm && options.length > 0 ? (
            <View style={{ marginTop: 12 }}>
              <StormOptions
                q={thread}
                disabled={busy}
                onChoose={(choose, label) => {
                  const comment = text.trim()
                  onAnswer(
                    thread,
                    comment ? { choose, body: comment } : { choose },
                    comment ? `${label} — ${comment}` : label,
                  )
                }}
              />
            </View>
          ) : null}
          {!storm && options.length > 0 ? (
            <View style={styles.optionRow}>
              {options.map((label, i) => (
                <Pressable key={i} style={styles.optionBtn} onPress={() => onAnswer(thread, { choose: i + 1 }, label)}>
                  <Text style={styles.optionText}>{label}</Text>
                </Pressable>
              ))}
            </View>
          ) : null}
          <TextInput
            value={text}
            onChangeText={setText}
            onFocus={onInputFocus}
            onBlur={onInputBlur}
            placeholder={storm ? STORM_INPUT_PLACEHOLDER : 'Your answer…'}
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

const styles = StyleSheet.create({
  sourceRow: { flexDirection: 'row', gap: 8, marginBottom: 6 },
  source: { fontSize: 12, color: colors.accent, flex: 1 },
  ref: { fontSize: 11.5, color: colors.textFaint },
  meta: { fontSize: 11.5, color: colors.textFaint },
  metaRow: { flexDirection: 'row', alignItems: 'center', gap: 6, marginBottom: 8, flexWrap: 'wrap' },
  optionRow: { flexDirection: 'row', flexWrap: 'wrap', gap: 8, marginTop: 12, marginBottom: 12 },
  optionBtn: {
    paddingVertical: 9,
    paddingHorizontal: 14,
    borderRadius: radius.lg,
    borderWidth: 1,
    borderColor: colors.border,
    backgroundColor: colors.cardAlt,
  },
  optionText: { fontSize: 13, fontWeight: '600', color: colors.text },
  input: {
    minHeight: 60,
    padding: 12,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: radius.lg,
    fontSize: 14,
    lineHeight: 21,
    color: colors.text,
    backgroundColor: colors.card,
    textAlignVertical: 'top',
  },
  actions: { flexDirection: 'row', alignItems: 'center', marginTop: 10 },
  dismiss: { fontSize: 13, color: colors.textDim },
  sendBtn: {
    backgroundColor: colors.ink,
    borderRadius: radius.md,
    paddingHorizontal: 14,
    paddingVertical: 8,
  },
  sendText: { color: '#fff', fontWeight: '600' },
})
