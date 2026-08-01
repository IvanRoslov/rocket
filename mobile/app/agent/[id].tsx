import { router, useLocalSearchParams } from 'expo-router'
import { useEffect, useState } from 'react'
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
  useAgentItems,
  useAgentMemory,
  useAgentQuestionAnswer,
  useAgentQuestionDismiss,
  useAgentQuestionReply,
  useAgentQuestions,
  useCreateAgentQuestion,
  useSessions,
  useSetAgentEnabled,
  useWakeAgent,
} from '../../src/api/queries'
import type { AgentItem, AgentMemoryFile, AgentQuestion } from '../../src/api/types'
import { ActionSheet } from '../../src/components/ActionSheet'
import { BottomSheet } from '../../src/components/BottomSheet'
import { Markdown } from '../../src/components/Markdown'
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
import { inboxKindBadge, inboxStatusBadge, inboxSummary, isRoleRun, itemStateBadge, subscriptionLabel } from '../../src/lib/agents'
import { ago } from '../../src/lib/format'
import { colors, mono, radius } from '../../src/theme'

/**
 * One Q&A thread of a role. Mirrors the task thread card: a thread the user
 * opened (asked_by === "") is answered by the role and closed by the user; a
 * thread the role opened is an escalation the user answers.
 */
function RoleQuestionCard({ q, roleId }: { q: AgentQuestion; roleId: string }) {
  const reply = useAgentQuestionReply()
  const answer = useAgentQuestionAnswer()
  const dismiss = useAgentQuestionDismiss()
  const [text, setText] = useState('')
  const [ctxOpen, setCtxOpen] = useState(false)
  const busy = reply.isPending || answer.isPending || dismiss.isPending
  const toast = useToast()

  const mine = !q.asked_by

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
    m.mutate({ id: q.id, body }, { onSuccess: () => setText(''), onError: (e) => toast.show((e as Error).message) })
  }

  return (
    <View style={styles.qCard}>
      <View style={styles.qHead}>
        <Badge label={`Q${q.ordinal}`} fg={colors.amberDeep} bg={colors.amberBg} />
        <Badge
          label={q.whose_turn === 'user' ? 'awaiting you' : `waiting for ${roleId}`}
          fg={colors.amberDeep}
          bg={colors.amberBg}
        />
        <View style={{ flex: 1 }} />
        <MonoText style={{ fontSize: 11, color: '#a1621a' }}>{mine ? 'you asked' : roleId}</MonoText>
      </View>
      <View style={{ padding: 16 }}>
        <Text style={styles.qText}>{q.body}</Text>
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
        <Text style={styles.discussLabel}>DISCUSSION · {q.messages.length} REPLIES</Text>
        {q.messages.map((m) => {
          const isUser = !m.author
          return (
            <View key={m.id} style={styles.threadMsg}>
              <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8, marginBottom: 8 }}>
                <View style={[styles.avatar, { backgroundColor: isUser ? colors.indigoBg : colors.amberBg }]}>
                  <Text
                    style={{
                      fontFamily: mono,
                      fontSize: 11,
                      fontWeight: '700',
                      color: isUser ? colors.indigoFg : colors.amberDeep,
                    }}
                  >
                    {isUser ? 'Y' : roleId.slice(0, 1).toUpperCase()}
                  </Text>
                </View>
                <Text style={{ fontSize: 12.5, fontWeight: '600', color: isUser ? colors.indigoFg : colors.amberDeep }}>
                  {isUser ? 'you' : roleId}
                </Text>
                <Text style={{ fontSize: 11, color: colors.textFaint }}>{ago(m.created_at)}</Text>
              </View>
              <Text style={{ fontSize: 13.5, lineHeight: 22, color: '#27272a' }}>{m.body}</Text>
            </View>
          )
        })}
        <View style={styles.replyBox}>
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

/** Composer for a thread the user opens on a role; it lands in the role's inbox. */
function AskRoleSheet({ visible, roleId, onClose }: { visible: boolean; roleId: string; onClose: () => void }) {
  const create = useCreateAgentQuestion()
  const toast = useToast()
  const [body, setBody] = useState('')
  const [context, setContext] = useState('')

  return (
    <BottomSheet visible={visible} onClose={onClose}>
      <View>
        <Text style={{ fontSize: 15, fontWeight: '700', marginBottom: 4 }}>Ask {roleId}</Text>
        <Text style={{ fontSize: 12.5, color: colors.textDim, marginBottom: 14 }}>
          Opens a thread on this role — it wakes, replies, and you can follow up or resolve.
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
                { roleId, body: body.trim(), context: context.trim() || undefined },
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

function DossierRow({ item }: { item: AgentItem }) {
  return (
    <Card>
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8, marginBottom: 8 }}>
        <Badge {...itemStateBadge(item.state)} />
        <MonoText style={{ fontSize: 12.5, flex: 1 }} numberOfLines={1}>
          {item.ref}
        </MonoText>
        <MonoText style={{ fontSize: 11, color: colors.textFaint }}>{item.kind}</MonoText>
      </View>
      {item.note ? (
        <Text style={{ fontSize: 13, lineHeight: 20, color: colors.textBody, marginBottom: 8 }}>{item.note}</Text>
      ) : null}
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: 12 }}>
        {item.task_id ? (
          <Pressable onPress={() => router.push(`/task/${item.task_id}`)}>
            <Text style={{ fontSize: 12.5, fontWeight: '600', color: colors.accent }}>→ task #{item.task_id}</Text>
          </Pressable>
        ) : null}
        {item.snooze_until ? (
          <MonoText style={{ fontSize: 11.5, color: colors.amberFg }}>⏰ until {ago(item.snooze_until)}</MonoText>
        ) : null}
        <View style={{ flex: 1 }} />
        <Text style={{ fontSize: 11, color: colors.textFaint }}>{ago(item.updated_at)}</Text>
      </View>
    </Card>
  )
}

/** One memory fact file, collapsed to its name until tapped. */
function MemoryFileCard({ file }: { file: AgentMemoryFile }) {
  const [open, setOpen] = useState(false)
  return (
    <Card>
      <Pressable onPress={() => setOpen(!open)} style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
        <MonoText style={{ fontSize: 12.5, fontWeight: '600', color: colors.text, flex: 1 }} numberOfLines={1}>
          {file.name}
        </MonoText>
        <Text style={{ fontSize: 11, color: colors.textFaint }}>
          {file.size} B · {ago(file.updated_at)}
        </Text>
        <Text style={{ fontSize: 12, color: colors.textDim }}>{open ? '▴' : '▾'}</Text>
      </Pressable>
      {open ? (
        <View style={{ marginTop: 10, borderTopWidth: 1, borderTopColor: colors.borderSoft, paddingTop: 10 }}>
          <Markdown>{file.body}</Markdown>
        </View>
      ) : null}
    </Card>
  )
}

export default function AgentScreen() {
  const { id } = useLocalSearchParams<{ id: string }>()
  const roleId = String(id)
  const detail = useAgent(roleId)
  const questions = useAgentQuestions(roleId)
  const [tab, setTab] = useState('questions')
  const inbox = useAgentInbox(roleId, tab === 'inbox')
  const items = useAgentItems(roleId, tab === 'dossier')
  // Probed on mount: the endpoint ships with the dashboard work, and the tab
  // must not appear at all on a daemon that lacks it.
  const memory = useAgentMemory(roleId, true)
  const [itemState, setItemState] = useState('all')
  const { data: sessions } = useSessions(detail.data?.project)
  const wake = useWakeAgent()
  const setEnabled = useSetAgentEnabled()
  const [wakeText, setWakeText] = useState('')
  const [menu, setMenu] = useState(false)
  const [asking, setAsking] = useState(false)
  const insets = useSafeAreaInsets()
  const toast = useToast()
  const onErr = (e: unknown) => toast.show((e as Error).message)

  // A tab that disappears must not leave the screen blank.
  useEffect(() => {
    if (tab === 'memory' && memory.isError) setTab('prompt')
  }, [tab, memory.isError])

  const a = detail.data
  const open = (questions.data ?? []).filter((q) => q.status === 'open')
  const resolved = (questions.data ?? []).filter((q) => q.status === 'resolved')
  const awaiting = open.filter((q) => q.whose_turn === 'user')
  const run = sessions?.find((s) => isRoleRun(s.id, roleId) && (s.state === 'running' || s.state === 'spawning'))

  if (!a) {
    return (
      <SafeAreaView style={{ flex: 1, backgroundColor: colors.page }}>
        <EmptyState text={detail.isError ? 'Failed to load role.' : 'Loading…'} />
      </SafeAreaView>
    )
  }

  const states = Array.from(new Set((items.data ?? []).map((it) => it.state || 'new')))
  const shownItems = (items.data ?? []).filter((it) => itemState === 'all' || (it.state || 'new') === itemState)

  const chips = [
    {
      key: 'questions',
      label: 'Questions',
      ...(open.length > 0 ? { count: open.length } : {}),
      warn: awaiting.length > 0,
    },
    { key: 'inbox', label: 'Inbox', ...(a.inbox_queued > 0 ? { count: a.inbox_queued } : {}) },
    { key: 'dossier', label: 'Dossier', ...(a.items > 0 ? { count: a.items } : {}) },
    { key: 'prompt', label: 'Prompt' },
    ...(memory.isError ? [] : [{ key: 'memory', label: 'Memory' }]),
  ]

  return (
    <SafeAreaView style={{ flex: 1, backgroundColor: colors.page }} edges={['top', 'bottom']}>
      <View style={styles.header}>
        <BackButton onPress={() => router.back()} />
        <Dot color={run ? colors.green : a.enabled ? colors.slate : colors.textFaint} size={9} />
        <MonoText style={styles.headerTitle} numberOfLines={1}>
          {a.id}
        </MonoText>
        {!a.enabled ? <Badge label="disabled" fg={colors.slateFg} bg={colors.slateBg} /> : null}
        {run ? <Badge label="● live" fg={colors.greenFg} bg={colors.greenBg} /> : null}
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
            label: 'Wake now',
            onPress: () => wake.mutate({ id: a.id, text: 'ping from mobile' }, { onError: onErr }),
          },
          ...(run ? [{ label: 'Open chat', onPress: () => router.navigate(`/chat/${run.id}`) }] : []),
          {
            label: a.enabled ? 'Disable role' : 'Enable role',
            destructive: a.enabled,
            onPress: () => setEnabled.mutate({ id: a.id, enabled: !a.enabled }, { onError: onErr }),
          },
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
              }}
            />
          }
        >
          <View style={{ padding: 16, paddingBottom: 0 }}>
            <View style={styles.metaRow}>
              <MonoText style={{ fontSize: 12 }}>{a.project}</MonoText>
              <Text style={{ fontSize: 12, color: colors.textDim }}>
                {a.agent} · updated {ago(a.updated_at)}
              </Text>
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

          <View style={{ paddingHorizontal: 16, paddingBottom: 12 }}>
            <ChipTabs chips={chips} active={tab} onChange={setTab} />
          </View>

          <View style={{ paddingHorizontal: 16 }}>
            {tab === 'questions' ? (
              <View style={{ gap: 14 }}>
                <GhostButton label={`＋ Ask ${a.id}`} onPress={() => setAsking(true)} />
                {open.map((q) => (
                  <RoleQuestionCard key={q.id} q={q} roleId={a.id} />
                ))}
                {open.length === 0 ? <EmptyState text="No open questions." /> : null}
                {resolved.length > 0 ? <Text style={styles.discussLabel}>RESOLVED</Text> : null}
                {resolved.map((q) => (
                  <Card key={q.id} style={{ flexDirection: 'row', alignItems: 'center', gap: 9, padding: 12 }}>
                    <Badge label={`Q${q.ordinal}`} fg={colors.slateFg} bg={colors.slateBg} />
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
                {(inbox.data ?? [])
                  .slice()
                  .reverse()
                  .map((e) => {
                    const summary = inboxSummary(e)
                    return (
                      <Card key={e.id}>
                        <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8, marginBottom: 7 }}>
                          <Badge {...inboxKindBadge(e.kind)} />
                          <Badge {...inboxStatusBadge(e.status)} />
                          <View style={{ flex: 1 }} />
                          <Text style={{ fontSize: 11, color: colors.textFaint }}>{ago(e.created_at)}</Text>
                        </View>
                        {summary ? (
                          <Text style={{ fontSize: 13.5, lineHeight: 20, color: colors.textBody }} numberOfLines={4}>
                            {summary}
                          </Text>
                        ) : null}
                      </Card>
                    )
                  })}
                {inbox.isSuccess && inbox.data.length === 0 ? <EmptyState text="Inbox is empty." /> : null}
              </View>
            ) : null}

            {tab === 'dossier' ? (
              <View style={{ gap: 10 }}>
                {states.length > 0 ? (
                  <ChipTabs
                    chips={[{ key: 'all', label: 'All' }, ...states.map((s) => ({ key: s, label: s }))]}
                    active={itemState}
                    onChange={setItemState}
                  />
                ) : null}
                {shownItems.map((it) => (
                  <DossierRow key={it.id} item={it} />
                ))}
                {items.isSuccess && shownItems.length === 0 ? (
                  <EmptyState text="Nothing in the dossier yet." />
                ) : null}
              </View>
            ) : null}

            {tab === 'prompt' ? (
              <View style={{ gap: 12 }}>
                <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
                  <Badge label="read-only" fg={colors.slateFg} bg={colors.slateBg} />
                  <MonoText style={{ fontSize: 11, flex: 1 }} numberOfLines={1}>
                    {a.prompt_path}
                  </MonoText>
                </View>
                <Card>
                  <Text style={styles.discussLabel}>SUBSCRIPTIONS</Text>
                  {a.subscriptions.length > 0 ? (
                    a.subscriptions.map((s) => (
                      <MonoText key={s.repo} style={{ fontSize: 12.5, marginBottom: 3 }}>
                        {subscriptionLabel(s)}
                      </MonoText>
                    ))
                  ) : (
                    <Text style={{ fontSize: 13, color: colors.textFaint }}>No repo subscriptions.</Text>
                  )}
                  <Text style={[styles.discussLabel, { marginTop: 12 }]}>SCHEDULE</Text>
                  <MonoText style={{ fontSize: 12.5 }}>{a.cron || 'no cron — event-driven only'}</MonoText>
                </Card>
                <Card>
                  <Markdown>{a.prompt ?? ''}</Markdown>
                </Card>
              </View>
            ) : null}

            {tab === 'memory' ? (
              <View style={{ gap: 12 }}>
                <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
                  <Badge label="read-only" fg={colors.slateFg} bg={colors.slateBg} />
                  <MonoText style={{ fontSize: 11, flex: 1 }} numberOfLines={1}>
                    {memory.data?.path ?? ''}
                  </MonoText>
                </View>
                <Card>
                  {memory.data?.index ? (
                    <Markdown>{memory.data.index}</Markdown>
                  ) : (
                    <Text style={{ fontSize: 13, color: colors.textFaint }}>The memory index is empty.</Text>
                  )}
                </Card>
                {(memory.data?.files ?? []).map((f) => (
                  <MemoryFileCard key={f.name} file={f} />
                ))}
              </View>
            ) : null}
          </View>
        </ScrollView>

        <View style={[styles.wakeBar, { paddingBottom: Math.max(12, insets.bottom) }]}>
          <TextInput
            style={styles.wakeInput}
            placeholder={`Ping ${a.id}…`}
            placeholderTextColor={colors.textFaint}
            value={wakeText}
            onChangeText={setWakeText}
            multiline
          />
          <PrimaryButton
            label={wake.isPending ? 'Waking…' : 'Wake'}
            disabled={!wakeText.trim() || wake.isPending}
            onPress={() =>
              wake.mutate(
                { id: a.id, text: wakeText.trim() },
                {
                  onSuccess: () => {
                    setWakeText('')
                    toast.show(`${a.id} woken`)
                  },
                  onError: onErr,
                },
              )
            }
            style={{ height: 44, paddingHorizontal: 16, borderRadius: radius.lg }}
          />
        </View>
      </KeyboardAvoidingView>

      <AskRoleSheet visible={asking} roleId={a.id} onClose={() => setAsking(false)} />
    </SafeAreaView>
  )
}

const styles = StyleSheet.create({
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
  metaRow: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', gap: 9, marginBottom: 16 },
  awaitBanner: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 11,
    borderWidth: 1.5,
    borderColor: colors.amberBorder,
    backgroundColor: colors.amberBgSoft,
    borderRadius: radius.xl,
    padding: 13,
    marginBottom: 16,
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
  wakeBar: {
    flexDirection: 'row',
    gap: 8,
    alignItems: 'flex-end',
    paddingHorizontal: 16,
    paddingTop: 10,
    backgroundColor: colors.card,
    borderTopWidth: 1,
    borderTopColor: colors.border,
  },
  wakeInput: {
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
