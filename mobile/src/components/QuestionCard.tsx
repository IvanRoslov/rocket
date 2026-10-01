import { useState } from 'react'
import { Alert, Pressable, StyleSheet, Text, TextInput, View } from 'react-native'
import { useQuestionAnswer, useQuestionDismiss, useQuestionReply } from '../api/queries'
import type { Question } from '../api/types'
import { isBrainstorm } from '../lib/brainstorm'
import { ago } from '../lib/format'
import {
  addresseeLabel,
  answerableBy,
  isHuman,
  participantInitial,
  participantLabel,
  threadBadges,
  threadRefLabel,
  toggleAddressee,
} from '../lib/threads'
import { colors, mono, radius } from '../theme'
import { STORM_INPUT_PLACEHOLDER, StormAnswerSummary, StormOptions } from './BrainstormAnswer'
import { QuestionText } from './QuestionText'
import { useToast } from './Toast'
import { Badge, GhostButton, MonoText, PrimaryButton } from './ui'

/** One task thread in full: question, options, discussion and the reply box. */
export function QuestionCard({ q }: { q: Question }) {
  const reply = useQuestionReply()
  const answer = useQuestionAnswer()
  const dismiss = useQuestionDismiss()
  const [text, setText] = useState('')
  const [ctxOpen, setCtxOpen] = useState(false)
  const [to, setTo] = useState<string[]>([])
  const busy = reply.isPending || answer.isPending || dismiss.isPending
  const toast = useToast()

  // Threads opened by the user flip the roles: the orchestrator owes the
  // answer, and closing the thread is the user's call. The human is "" on the
  // wire today and "human" after subtask #736, so recognise both.
  const mine = isHuman(q.asked_by)
  const storm = isBrainstorm(q)
  const open = q.status === 'open'
  const others = answerableBy(q.participants ?? [])

  const confirmDismiss = () =>
    Alert.alert(
      mine ? 'Resolve thread' : 'Dismiss question',
      mine ? `Close Q${q.ordinal} — got what you needed?` : `Close Q${q.ordinal} without an answer?`,
      [
        { text: 'Cancel', style: 'cancel' },
        {
          text: mine ? 'Resolve' : 'Dismiss',
          style: mine ? 'default' : 'destructive',
          onPress: () => dismiss.mutate(q.id),
        },
      ],
    )

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

  return (
    <View style={styles.qCard}>
      <View style={styles.qHead}>
        <Badge label={threadRefLabel(q)} fg={colors.amberDeep} bg={colors.amberBg} />
        {threadBadges(q).map((b) => (
          <Badge
            key={b.label}
            label={b.label}
            fg={b.label === 'stale' ? colors.amberDeep : colors.textDim}
            bg={b.label === 'stale' ? colors.amberBg : colors.cardAlt}
          />
        ))}
        {/* An fyi note waits for nobody, so it never gets a turn chip. */}
        {q.status === 'open' && q.type !== 'fyi' ? (
          <Badge
            label={
              q.your_turn
                ? 'awaiting you'
                : `waiting for ${(q.waiting_on ?? []).map(participantLabel).join(', ') || 'orch'}`
            }
            fg={colors.amberDeep}
            bg={colors.amberBg}
          />
        ) : null}
        <View style={{ flex: 1 }} />
        <MonoText style={{ fontSize: 11, color: '#a1621a' }}>{participantLabel(q.asked_by)} asked</MonoText>
      </View>
      <View style={{ padding: 16 }}>
        <QuestionText q={q} />
        {(q.participants ?? []).length > 0 ? (
          <>
            <Text style={styles.discussLabel}>PARTICIPANTS</Text>
            <Text style={{ fontSize: 12.5, color: colors.textDim, marginBottom: 14 }}>
              {q.participants.map(participantLabel).join(', ')}
            </Text>
          </>
        ) : null}
        {q.context ? (
          ctxOpen ? (
            <View style={styles.ctxBox}>
              <View style={styles.ctxHead}>
                <Text style={styles.ctxLabel}>CONTEXT</Text>
                <Pressable onPress={() => setCtxOpen(false)}>
                  <Text style={{ fontSize: 12, color: colors.textDim }}>Hide ▴</Text>
                </Pressable>
              </View>
              <MonoText style={{ padding: 13, fontSize: 12.5, lineHeight: 20 }}>{q.context}</MonoText>
            </View>
          ) : (
            <Pressable onPress={() => setCtxOpen(true)} style={{ marginBottom: 14 }}>
              <Text style={{ fontSize: 12.5, color: colors.accent }}>＋ Show context</Text>
            </Pressable>
          )
        ) : null}
        {/* One tap closes the thread with that option — the cheapest answer
            there is, and the reason threads stop piling up (spec v1
            §«Варианты ответа»). `choose` is a 1-based index. */}
        {storm && !open ? <StormAnswerSummary q={q} /> : null}
        {storm && open && (q.options ?? []).length > 0 ? (
          <StormOptions
            q={q}
            disabled={busy}
            onChoose={(choose) =>
              answer.mutate(
                // The reply box doubles as the option's comment.
                { id: q.id, choose, body: text },
                { onSuccess: () => setText(''), onError: (e: unknown) => toast.show((e as Error).message) },
              )
            }
          />
        ) : null}
        {!storm && open && (q.options ?? []).length > 0 ? (
          <View style={styles.optionRow}>
            {(q.options ?? []).map((label, i) => (
              <Pressable
                key={label}
                testID={`option-${i + 1}`}
                style={styles.optionBtn}
                disabled={busy}
                onPress={() =>
                  answer.mutate(
                    { id: q.id, choose: i + 1 },
                    { onError: (e: unknown) => toast.show((e as Error).message) },
                  )
                }
              >
                <Text style={styles.optionText}>{label}</Text>
              </Pressable>
            ))}
          </View>
        ) : null}
        <Text style={styles.discussLabel}>DISCUSSION · {q.messages.length} REPLIES</Text>
        {q.messages.map((m) => {
          const isUser = isHuman(m.author)
          const addressees = addresseeLabel(m.addressed_to)
          return (
            <View key={m.id} style={styles.threadMsg}>
              <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8, marginBottom: 8, flexWrap: 'wrap' }}>
                <View
                  style={[
                    styles.avatar,
                    { backgroundColor: isUser ? colors.indigoBg : colors.amberBg },
                  ]}
                >
                  <Text
                    style={{
                      fontFamily: mono,
                      fontSize: 11,
                      fontWeight: '700',
                      color: isUser ? colors.indigoFg : colors.amberDeep,
                    }}
                  >
                    {participantInitial(m.author)}
                  </Text>
                </View>
                <Text style={{ fontSize: 12.5, fontWeight: '600', color: isUser ? colors.indigoFg : colors.amberDeep }}>
                  {participantLabel(m.author)}
                </Text>
                {addressees ? (
                  <Text style={{ fontSize: 11, color: colors.textDim }}>{addressees}</Text>
                ) : null}
                <Text style={{ fontSize: 11, color: colors.textFaint }}>{ago(m.created_at)}</Text>
              </View>
              <Text style={{ fontSize: 13.5, lineHeight: 22, color: '#27272a' }}>{m.body}</Text>
            </View>
          )
        })}
        {/* A closed thread takes no more answers. */}
        {open ? (
        <View style={styles.replyBox}>
          {others.length > 0 ? (
            <View style={styles.toRow}>
              <Text style={styles.toLabel}>TO</Text>
              {others.map((p) => {
                const on = to.includes(p)
                return (
                  <Pressable
                    key={p}
                    testID={`to-${p}`}
                    accessibilityRole="checkbox"
                    accessibilityState={{ selected: on }}
                    onPress={() => setTo((sel) => toggleAddressee(sel, p))}
                    style={[styles.toChip, on ? styles.toChipOn : null]}
                  >
                    <Text style={{ fontSize: 12, color: on ? colors.amberDeep : colors.textDim }}>{p}</Text>
                  </Pressable>
                )
              })}
            </View>
          ) : null}
          <TextInput
            style={styles.replyInput}
            placeholder={
              storm ? STORM_INPUT_PLACEHOLDER : mine ? 'Ask a follow-up…' : 'Write a reply or give your final answer…'
            }
            placeholderTextColor={colors.textFaint}
            value={text}
            onChangeText={setText}
            multiline
          />
          {mine ? (
            <View style={{ flexDirection: 'row', gap: 9 }}>
              <GhostButton
                label={busy ? 'Sending…' : 'Ask follow-up'}
                onPress={() => send(false)}
                style={{ flex: 1 }}
              />
              <PrimaryButton label="Resolve thread" disabled={busy} onPress={confirmDismiss} style={{ flex: 1 }} />
            </View>
          ) : (
            <>
              <View style={{ flexDirection: 'row', gap: 9 }}>
                <GhostButton label="Clarify" onPress={() => send(false)} style={{ flex: 1 }} />
                <PrimaryButton
                  label={busy ? 'Sending…' : 'Answer & close'}
                  disabled={busy || !text.trim()}
                  onPress={() => send(true)}
                  style={{ flex: 1 }}
                />
              </View>
              <Pressable onPress={confirmDismiss} style={{ alignSelf: 'center', marginTop: 12 }}>
                <Text style={{ fontSize: 12.5, fontWeight: '600', color: colors.textFaint }}>
                  Dismiss without answer
                </Text>
              </Pressable>
            </>
          )}
        </View>
        ) : null}
      </View>
    </View>
  )
}

const styles = StyleSheet.create({
  qCard: {
    borderWidth: 1.5,
    borderColor: colors.amberBorder,
    backgroundColor: colors.card,
    borderRadius: radius.xxl,
    overflow: 'hidden',
  },
  qHead: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 9,
    padding: 12,
    paddingHorizontal: 14,
    backgroundColor: colors.amberBgSoft,
    borderBottomWidth: 1,
    borderBottomColor: '#fde68a',
  },
  optionRow: { flexDirection: 'row', flexWrap: 'wrap', gap: 8, marginBottom: 16 },
  optionBtn: {
    paddingVertical: 9,
    paddingHorizontal: 14,
    borderRadius: radius.lg,
    borderWidth: 1,
    borderColor: colors.border,
    backgroundColor: colors.cardAlt,
  },
  optionText: { fontSize: 13, fontWeight: '600', color: colors.text },
  ctxBox: {
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: radius.lg,
    overflow: 'hidden',
    marginBottom: 18,
  },
  ctxHead: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: 9,
    paddingHorizontal: 13,
    backgroundColor: colors.cardAlt,
    borderBottomWidth: 1,
    borderBottomColor: colors.borderSoft,
  },
  ctxLabel: { fontSize: 10.5, fontWeight: '600', color: colors.textDim, letterSpacing: 0.5 },
  discussLabel: {
    fontSize: 10.5,
    fontWeight: '600',
    color: colors.textFaint,
    letterSpacing: 0.5,
    marginBottom: 8,
    marginTop: 4,
  },
  threadMsg: { paddingVertical: 14, borderTopWidth: 1, borderTopColor: '#f4f4f2' },
  avatar: { width: 24, height: 24, borderRadius: 7, alignItems: 'center', justifyContent: 'center' },
  replyBox: { marginTop: 8, borderTopWidth: 1, borderTopColor: colors.border, paddingTop: 16 },
  toRow: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', gap: 7, marginBottom: 10 },
  toLabel: { fontSize: 11, fontWeight: '700', letterSpacing: 0.6, color: colors.textFaint },
  toChip: {
    paddingHorizontal: 10,
    paddingVertical: 5,
    borderRadius: radius.chip,
    borderWidth: 1,
    borderColor: colors.border,
  },
  toChipOn: { borderColor: colors.amberDeep, backgroundColor: colors.amberBg },
  replyInput: {
    minHeight: 84,
    padding: 12,
    paddingTop: 12,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: radius.lg,
    fontSize: 14,
    lineHeight: 21,
    color: colors.text,
    backgroundColor: colors.card,
    marginBottom: 11,
    textAlignVertical: 'top',
  },
})
