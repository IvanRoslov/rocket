import { router, useLocalSearchParams } from 'expo-router'
import { useState } from 'react'
import {
  Alert,
  KeyboardAvoidingView,
  Pressable,
  RefreshControl,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native'
import { SafeAreaView, useSafeAreaInsets } from 'react-native-safe-area-context'
import {
  useAgent,
  useAgentInbox,
  useAgentQuestionAnswer,
  useAgentQuestionDismiss,
  useAgentQuestionReply,
  useAgentQuestions,
  useCreateAgentQuestion,
  useSendAgentMessage,
  useSetAgentEnabled,
  useStartAgent,
  useStopAgent,
} from '../../src/api/queries'
import type { AgentInboxMessage, AgentQuestion } from '../../src/api/types'
import { ActionSheet } from '../../src/components/ActionSheet'
import { BottomSheet } from '../../src/components/BottomSheet'
import { useToast } from '../../src/components/Toast'
import {
  BackButton,
  Badge,
  Card,
  ChipTabs,
  Dot,
  EmptyState,
  GhostButton,
  MonoText,
  PrimaryButton,
} from '../../src/components/ui'
import { inboxStatusBadge } from '../../src/lib/agents'
import { ago } from '../../src/lib/format'
import {
  addresseeLabel,
  answerableBy,
  isHuman,
  participantInitial,
  participantLabel,
  toggleAddressee,
  threadBadges,
  threadRefLabel,
} from '../../src/lib/threads'
import { colors, mono, radius } from '../../src/theme'

/**
 * One Q&A thread of an agent. Mirrors the task thread card: a thread the user
 * opened (asked_by === "") is answered by the agent and closed by the user; a
 * thread the agent opened is an escalation the user answers.
 */
function AgentQuestionCard({ q, agentId }: { q: AgentQuestion; agentId: string }) {
  const reply = useAgentQuestionReply()
  const answer = useAgentQuestionAnswer()
  const dismiss = useAgentQuestionDismiss()
  const [text, setText] = useState('')
  const [ctxOpen, setCtxOpen] = useState(false)
  const [to, setTo] = useState<string[]>([])
  const busy = reply.isPending || answer.isPending || dismiss.isPending
  const toast = useToast()

  // A thread we opened flips the roles. The human is "" on the wire today and
  // "human" after subtask #736, so recognise both.
  const mine = isHuman(q.asked_by)
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
          onPress: () => dismiss.mutate(q.id, { onError: (e) => toast.show((e as Error).message) }),
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
                : `waiting for ${(q.waiting_on ?? []).map(participantLabel).join(', ') || agentId}`
            }
            fg={colors.amberDeep}
            bg={colors.amberBg}
          />
        ) : null}
        <View style={{ flex: 1 }} />
        <MonoText style={{ fontSize: 11, color: '#a1621a' }}>{participantLabel(q.asked_by)} asked</MonoText>
      </View>
      <View style={{ padding: 16 }}>
        <Text style={styles.qText}>{q.body}</Text>
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
        {q.status === 'open' && (q.options ?? []).length > 0 ? (
          <View style={styles.optionRow}>
            {(q.options ?? []).map((label, i) => (
              <Pressable
                key={label}
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
                <View style={[styles.avatar, { backgroundColor: isUser ? colors.indigoBg : colors.amberBg }]}>
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
            placeholder={mine ? 'Ask a follow-up…' : 'Write a reply or give your final answer…'}
            placeholderTextColor={colors.textFaint}
            value={text}
            onChangeText={setText}
            multiline
          />
          {mine ? (
            <View style={{ flexDirection: 'row', gap: 9 }}>
              <GhostButton label={busy ? 'Sending…' : 'Ask follow-up'} onPress={() => send(false)} style={{ flex: 1 }} />
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
      </View>
    </View>
  )
}

/** Composer for a thread the user opens on an agent. */
function AskAgentSheet({ visible, agentId, onClose }: { visible: boolean; agentId: string; onClose: () => void }) {
  const create = useCreateAgentQuestion()
  const toast = useToast()
  const [body, setBody] = useState('')
  const [context, setContext] = useState('')

  return (
    <BottomSheet visible={visible} onClose={onClose}>
      <View>
        <Text style={{ fontSize: 15, fontWeight: '700', marginBottom: 4 }}>Ask {agentId}</Text>
        <Text style={{ fontSize: 12.5, color: colors.textDim, marginBottom: 14 }}>
          Opens a thread on this agent — it lands in its session or its inbox, and you can follow up or resolve.
        </Text>
        <TextInput
          style={styles.askInput}
          placeholder="Your question"
          placeholderTextColor={colors.textFaint}
          value={body}
          onChangeText={setBody}
          multiline
          autoFocus
        />
        <TextInput
          style={[styles.askInput, { minHeight: 70 }]}
          placeholder="Context (optional)"
          placeholderTextColor={colors.textFaint}
          value={context}
          onChangeText={setContext}
          multiline
        />
        <View style={{ flexDirection: 'row', gap: 9 }}>
          <GhostButton label="Cancel" onPress={onClose} style={{ flex: 1 }} />
          <PrimaryButton
            label={create.isPending ? 'Asking…' : 'Ask'}
            disabled={!body.trim() || create.isPending}
            onPress={() =>
              create.mutate(
                { agentId, body: body.trim(), context: context.trim() || undefined },
                {
                  onSuccess: () => {
                    setBody('')
                    setContext('')
                    onClose()
                  },
                  onError: (e) => toast.show((e as Error).message),
                },
              )
            }
            style={{ flex: 1 }}
          />
        </View>
      </View>
    </BottomSheet>
  )
}

/** One inbox message: who wrote it, whether the agent has pulled it, the text. */
function InboxCard({ m }: { m: AgentInboxMessage }) {
  return (
    <Card>
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8, marginBottom: 7 }}>
        <MonoText style={{ fontSize: 12.5, fontWeight: '600', color: colors.text }}>{m.from || 'unknown'}</MonoText>
        <Badge {...inboxStatusBadge(m.status)} />
        <View style={{ flex: 1 }} />
        <Text style={{ fontSize: 11, color: colors.textFaint }}>{ago(m.created_at)}</Text>
      </View>
      <Text style={{ fontSize: 13.5, lineHeight: 20, color: colors.textBody }}>{m.body}</Text>
    </Card>
  )
}

export default function AgentScreen() {
  const { id } = useLocalSearchParams<{ id: string }>()
  const agentId = String(id)
  const detail = useAgent(agentId)
  const questions = useAgentQuestions(agentId)
  const [tab, setTab] = useState('questions')
  const inbox = useAgentInbox(agentId, tab === 'inbox')
  const send = useSendAgentMessage()
  const start = useStartAgent()
  const stop = useStopAgent()
  const setEnabled = useSetAgentEnabled()
  const [msg, setMsg] = useState('')
  const [menu, setMenu] = useState(false)
  const [asking, setAsking] = useState(false)
  const insets = useSafeAreaInsets()
  const toast = useToast()
  const onErr = (e: unknown) => toast.show((e as Error).message)

  const a = detail.data
  const open = (questions.data ?? []).filter((q) => q.status === 'open')
  const resolved = (questions.data ?? []).filter((q) => q.status === 'resolved')
  const awaiting = open.filter((q) => q.your_turn)

  if (!a) {
    return (
      <SafeAreaView style={{ flex: 1, backgroundColor: colors.page }}>
        <EmptyState text={detail.isError ? 'Failed to load agent.' : 'Loading…'} />
      </SafeAreaView>
    )
  }

  const chips = [
    {
      key: 'questions',
      label: 'Questions',
      ...(open.length > 0 ? { count: open.length } : {}),
      warn: awaiting.length > 0,
    },
    { key: 'inbox', label: 'Inbox', ...(a.unread > 0 ? { count: a.unread } : {}) },
  ]

  return (
    <SafeAreaView style={{ flex: 1, backgroundColor: colors.page }} edges={['top', 'bottom']}>
      <View style={styles.header}>
        <BackButton onPress={() => router.back()} />
        <Dot color={a.session_alive ? colors.green : a.enabled ? colors.slate : colors.textFaint} size={9} />
        <MonoText style={styles.headerTitle} numberOfLines={1}>
          {a.id}
        </MonoText>
        {!a.enabled ? <Badge label="disabled" fg={colors.slateFg} bg={colors.slateBg} /> : null}
        {a.session_alive ? <Badge label="● live" fg={colors.greenFg} bg={colors.greenBg} /> : null}
        <Pressable onPress={() => setMenu(true)} hitSlop={8}>
          <Text style={{ fontSize: 18, color: colors.textDim }}>⋯</Text>
        </Pressable>
      </View>

      <ActionSheet
        visible={menu}
        title={a.id}
        onClose={() => setMenu(false)}
        actions={[
          {
            label: a.enabled ? 'Disable agent' : 'Enable agent',
            destructive: a.enabled,
            onPress: () => setEnabled.mutate({ id: a.id, enabled: !a.enabled }, { onError: onErr }),
          },
          ...(a.session_alive
            ? [
                {
                  label: 'Stop session',
                  destructive: true,
                  onPress: () => stop.mutate(a.id, { onError: onErr }),
                },
              ]
            : []),
        ]}
      />

      <KeyboardAvoidingView style={{ flex: 1 }} behavior="padding">
        <ScrollView
          contentContainerStyle={{ paddingBottom: 96 + insets.bottom }}
          refreshControl={
            <RefreshControl
              refreshing={false}
              onRefresh={() => {
                detail.refetch()
                questions.refetch()
                inbox.refetch()
              }}
            />
          }
        >
          <View style={{ padding: 16, paddingBottom: 0 }}>
            {a.description ? <Text style={styles.description}>{a.description}</Text> : null}
            <View style={styles.metaRow}>
              {a.project ? <MonoText style={{ fontSize: 12 }}>{a.project}</MonoText> : null}
              <Text style={{ fontSize: 12, color: colors.textDim }}>updated {ago(a.updated_at)}</Text>
            </View>
            {a.dir ? (
              <MonoText style={styles.launcher} numberOfLines={1}>
                {a.dir}
                {a.command ? ` · ${a.command}` : ''}
              </MonoText>
            ) : null}

            {/* The session is external: rocket can start it and kill it, and
                that is the whole of its lifecycle management. */}
            <View style={styles.sessionRow}>
              {a.session_alive ? (
                <GhostButton
                  label="Open terminal"
                  onPress={() => router.navigate(`/chat/${a.id}?agent=1`)}
                  style={{ flex: 1 }}
                />
              ) : (
                <PrimaryButton
                  label={start.isPending ? 'Starting…' : 'Start'}
                  disabled={start.isPending || !a.enabled}
                  onPress={() =>
                    start.mutate(a.id, { onSuccess: () => toast.show(`${a.id} started`), onError: onErr })
                  }
                  style={{ flex: 1 }}
                />
              )}
              {/* Chat is reachable whatever the session does: what you write
                  reaches the live session or waits in the inbox. */}
              <GhostButton
                label="Chat"
                onPress={() => router.navigate(`/chat/${a.id}?agent=1`)}
                style={{ flex: 1 }}
              />
            </View>

            {/* Milestones (task #1023, spec v2): what this agent has taken
                on. They live outside every project and open as task cards. */}
            <View style={{ marginTop: 16 }}>
              <Text style={styles.milestonesTitle}>MILESTONES</Text>
              {(a.milestones ?? []).length === 0 ? (
                <Text style={styles.milestonesEmpty}>No milestones — nothing taken yet.</Text>
              ) : (
                <View style={{ gap: 8 }}>
                  {(a.milestones ?? []).map((m) => (
                    <Pressable
                      key={m.id}
                      style={styles.milestoneRow}
                      onPress={() => router.navigate(`/task/${m.id}`)}
                    >
                      <MonoText style={{ fontSize: 11.5, color: colors.textFaint }}>#{m.id}</MonoText>
                      <Text style={styles.milestoneTitle} numberOfLines={1}>
                        {m.title}
                      </Text>
                      <Badge label={m.status.replace('_', ' ')} fg={colors.slateFg} bg={colors.slateBg} />
                    </Pressable>
                  ))}
                </View>
              )}
            </View>

            {awaiting.length > 0 && tab !== 'questions' ? (
              <Pressable style={styles.awaitBanner} onPress={() => setTab('questions')}>
                <Badge label="? awaiting" fg={colors.amberDeep} bg={colors.amberBg} />
                <Text style={styles.awaitText} numberOfLines={2}>
                  {awaiting[0].body}
                </Text>
                <Text style={{ color: colors.amberDeep, fontSize: 16 }}>→</Text>
              </Pressable>
            ) : null}
          </View>

          <View style={{ paddingHorizontal: 16, paddingVertical: 12 }}>
            <ChipTabs chips={chips} active={tab} onChange={setTab} />
          </View>

          <View style={{ paddingHorizontal: 16 }}>
            {tab === 'questions' ? (
              <View style={{ gap: 14 }}>
                <GhostButton label={`＋ Ask ${a.id}`} onPress={() => setAsking(true)} />
                {open.map((q) => (
                  <AgentQuestionCard key={q.id} q={q} agentId={a.id} />
                ))}
                {open.length === 0 ? <EmptyState text="No open questions." /> : null}
                {resolved.length > 0 ? <Text style={styles.discussLabel}>RESOLVED</Text> : null}
                {resolved.map((q) => (
                  <Card key={q.id} style={{ flexDirection: 'row', alignItems: 'center', gap: 9, padding: 12 }}>
                    <Badge label={threadRefLabel(q)} fg={colors.slateFg} bg={colors.slateBg} />
                    {/* An fyi note is a status message, not an answered
                        question — the history says which it was. */}
                    {threadBadges(q).map((b) => (
                      <Badge key={b.label} label={b.label} fg={colors.textDim} bg={colors.cardAlt} />
                    ))}
                    <Text numberOfLines={1} style={{ flex: 1, fontSize: 13, color: colors.textMid }}>
                      {q.body}
                    </Text>
                    <Text style={{ fontSize: 11, color: colors.textFaint }}>{ago(q.resolved_at)}</Text>
                  </Card>
                ))}
              </View>
            ) : null}

            {tab === 'inbox' ? (
              <View style={{ gap: 10 }}>
                {(inbox.data ?? []).map((m) => (
                  <InboxCard key={m.id} m={m} />
                ))}
                {inbox.isSuccess && inbox.data.length === 0 ? <EmptyState text="Inbox is empty." /> : null}
              </View>
            ) : null}
          </View>
        </ScrollView>

        <View style={[styles.sendBar, { paddingBottom: Math.max(12, insets.bottom) }]}>
          <TextInput
            style={styles.sendInput}
            placeholder={a.session_alive ? `Message ${a.id}…` : `Leave a message for ${a.id}…`}
            placeholderTextColor={colors.textFaint}
            value={msg}
            onChangeText={setMsg}
            multiline
          />
          <PrimaryButton
            label={send.isPending ? 'Sending…' : 'Send'}
            disabled={!msg.trim() || send.isPending}
            onPress={() =>
              send.mutate(
                { id: a.id, body: msg.trim() },
                {
                  onSuccess: () => {
                    setMsg('')
                    toast.show(a.session_alive ? `delivered to ${a.id}` : `queued in ${a.id}'s inbox`)
                  },
                  onError: onErr,
                },
              )
            }
            style={{ height: 44, paddingHorizontal: 16, borderRadius: radius.lg }}
          />
        </View>
      </KeyboardAvoidingView>

      <AskAgentSheet visible={asking} agentId={a.id} onClose={() => setAsking(false)} />
    </SafeAreaView>
  )
}

const styles = StyleSheet.create({
  milestonesTitle: {
    fontSize: 10.5,
    fontWeight: '700',
    letterSpacing: 0.6,
    color: colors.textFaint,
    marginBottom: 8,
  },
  milestonesEmpty: { fontSize: 12.5, color: colors.textFaint },
  milestoneRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 9,
    paddingHorizontal: 11,
    paddingVertical: 9,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: radius.md,
    backgroundColor: colors.card,
  },
  milestoneTitle: { flex: 1, fontSize: 13.5, fontWeight: '600', color: colors.text },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
    height: 54,
    paddingHorizontal: 14,
    backgroundColor: colors.card,
    borderBottomWidth: 1,
    borderBottomColor: colors.border,
  },
  headerTitle: { fontSize: 15, fontWeight: '700', color: colors.text, flex: 1 },
  description: { fontSize: 14, lineHeight: 21, color: colors.textBody, marginBottom: 10 },
  metaRow: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', gap: 9, marginBottom: 10 },
  launcher: { fontSize: 11.5, color: colors.textFaint, marginBottom: 12 },
  sessionRow: { flexDirection: 'row', gap: 9, marginBottom: 4 },
  awaitBanner: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 11,
    borderWidth: 1.5,
    borderColor: colors.amberBorder,
    backgroundColor: colors.amberBgSoft,
    borderRadius: radius.xl,
    padding: 13,
    marginTop: 16,
  },
  awaitText: { flex: 1, fontSize: 13, lineHeight: 18, color: '#78350f', fontWeight: '500' },
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
  qText: { fontSize: 17, lineHeight: 24, fontWeight: '700', letterSpacing: -0.2, marginBottom: 14 },
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
  ctxBox: { borderWidth: 1, borderColor: colors.border, borderRadius: radius.lg, overflow: 'hidden', marginBottom: 18 },
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
  askInput: {
    minHeight: 90,
    padding: 12,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: radius.lg,
    fontSize: 14,
    lineHeight: 20,
    color: colors.text,
    backgroundColor: colors.card,
    marginBottom: 11,
    textAlignVertical: 'top',
  },
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
  sendBar: {
    flexDirection: 'row',
    gap: 8,
    alignItems: 'flex-end',
    paddingHorizontal: 16,
    paddingTop: 10,
    backgroundColor: colors.card,
    borderTopWidth: 1,
    borderTopColor: colors.border,
  },
  sendInput: {
    flex: 1,
    minHeight: 44,
    maxHeight: 120,
    padding: 12,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: radius.lg,
    fontSize: 13.5,
    color: colors.text,
    backgroundColor: colors.page,
  },
})
