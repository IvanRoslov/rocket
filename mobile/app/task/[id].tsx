import * as Clipboard from 'expo-clipboard'
import { router, useLocalSearchParams } from 'expo-router'
import { useState } from 'react'
import {
  Alert,
  KeyboardAvoidingView,
  Platform,
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
  useCancelTask,
  useCreateQuestion,
  useKillSession,
  useMessages,
  useMoveTask,
  useRestoreSession,
  useSendMessage,
  useSessions,
  useTaskDetail,
  useTaskDocs,
  useTaskGates,
  useTaskLog,
  useTaskQuestions,
} from '../../src/api/queries'
import { ActionSheet } from '../../src/components/ActionSheet'
import { BottomSheet } from '../../src/components/BottomSheet'
import { BrainstormTab } from '../../src/components/BrainstormTab'
import { Markdown } from '../../src/components/Markdown'
import { QuestionCard } from '../../src/components/QuestionCard'
import { useToast } from '../../src/components/Toast'
import type { Session, TaskLogKind, TaskStatus } from '../../src/api/types'
import { BackButton, Badge, Card, ChipTabs, Dot, EmptyState, GhostButton, MonoText, PrimaryButton } from '../../src/components/ui'
import { ago, sessionBadge, sessionDot } from '../../src/lib/format'
import { showBrainstormTab } from '../../src/lib/brainstorm'
import { questionPreview } from '../../src/lib/questions'
import { threadBadges, threadRefLabel } from '../../src/lib/threads'
import { colors, mono, radius } from '../../src/theme'

const STATUS_BADGE: Record<TaskStatus, { label: string; fg: string; bg: string }> = {
  backlog: { label: 'Backlog', fg: colors.slateFg, bg: colors.slateBg },
  brainstorm: { label: 'Brainstorm', fg: colors.amberFg, bg: colors.amberBg },
  in_progress: { label: 'In Progress', fg: colors.indigoFg, bg: colors.indigoBg },
  review: { label: 'Review', fg: colors.purpleFg, bg: colors.purpleBg },
  done: { label: 'Done', fg: colors.greenFg, bg: colors.greenBg },
  cancelled: { label: 'Cancelled', fg: colors.slateFg, bg: colors.slateBg },
}

const LOG_BADGE: Record<TaskLogKind, { fg: string; bg: string; dot: string }> = {
  problem: { fg: colors.redFg, bg: colors.redBg, dot: colors.red },
  decision: { fg: colors.greenFg, bg: colors.greenBg, dot: colors.green },
  note: { fg: colors.slateFg, bg: colors.slateBg, dot: colors.slate },
  status: { fg: colors.slateFg, bg: colors.slateBg, dot: colors.slate },
}

/** Composer for a user-initiated question thread (docs: reverse Q&A). */
function AskQuestionSheet({
  visible,
  taskId,
  onClose,
}: {
  visible: boolean
  taskId: number
  onClose: () => void
}) {
  const create = useCreateQuestion()
  const toast = useToast()
  const [body, setBody] = useState('')
  const [context, setContext] = useState('')

  return (
    <BottomSheet visible={visible} onClose={onClose}>
      <View>
        <Text style={{ fontSize: 15, fontWeight: '700', marginBottom: 4 }}>Ask the orchestrator</Text>
        <Text style={{ fontSize: 12.5, color: colors.textDim, marginBottom: 14 }}>
          Opens a thread on this task — the orchestrator replies, you can follow up and resolve it.
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
                { taskId, body: body.trim(), context: context.trim() || undefined },
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

function SessionsSheet({
  visible,
  orch,
  workers,
  onClose,
}: {
  visible: boolean
  orch?: Session
  workers: Session[]
  onClose: () => void
}) {
  const [copied, setCopied] = useState(false)
  const [menuFor, setMenuFor] = useState<Session | null>(null)
  const kill = useKillSession()
  const restore = useRestoreSession()

  const confirmKill = (s: Session, cleanup: boolean) =>
    Alert.alert(
      cleanup ? 'Kill + cleanup' : 'Kill session',
      `${s.tmux_name}: destroy the tmux session${cleanup ? ' and remove its worktree' : ''}?`,
      [
        { text: 'Cancel', style: 'cancel' },
        { text: 'Kill', style: 'destructive', onPress: () => kill.mutate({ id: s.id, cleanup }) },
      ],
    )
  const copyAttach = (name: string) => {
    Clipboard.setStringAsync(`rocket attach ${name}`)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }
  const openChat = (id: string) => {
    onClose()
    router.navigate(`/chat/${id}`)
  }

  return (
    <BottomSheet visible={visible} onClose={onClose}>
      <View>
          <View style={{ flexDirection: 'row', alignItems: 'center', marginBottom: 14 }}>
            <Text style={{ fontSize: 15, fontWeight: '700' }}>Sessions</Text>
            <View style={{ flex: 1 }} />
            <Pressable onPress={onClose}>
              <Text style={{ fontSize: 13, fontWeight: '600', color: colors.textDim }}>Done</Text>
            </Pressable>
          </View>
          <ScrollView style={{ maxHeight: 520 }}>
            {orch ? (
              <Card style={{ marginBottom: 14 }}>
                <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8, marginBottom: 4 }}>
                  <Dot color={sessionDot(orch.state, orch.activity)} size={8} />
                  <Text style={styles.sheetKindLabel}>ORCHESTRATOR</Text>
                  <View style={{ flex: 1 }} />
                  {orch.pending_quiz ? (
                    <Badge label="? quiz" fg={colors.amberDeep} bg={colors.amberBg} />
                  ) : null}
                  <Badge {...sessionBadge(orch.state, orch.activity)} />
                  <Pressable onPress={() => setMenuFor(orch)} hitSlop={8}>
                    <Text style={{ fontSize: 16, color: colors.textDim }}>⋯</Text>
                  </Pressable>
                </View>
                <MonoText style={{ fontSize: 14, fontWeight: '600', color: colors.text, marginBottom: 11 }}>
                  {orch.tmux_name}
                </MonoText>
                <View style={{ flexDirection: 'row', gap: 7 }}>
                  <PrimaryButton
                    label="💬 Open chat"
                    onPress={() => openChat(orch.id)}
                    style={{ flex: 1, height: 38, borderRadius: radius.md }}
                  />
                  <GhostButton
                    label="attach ⧉"
                    onPress={() => copyAttach(orch.tmux_name)}
                    style={{ height: 38, paddingHorizontal: 13, borderRadius: radius.md }}
                  />
                </View>
                {copied ? (
                  <MonoText style={{ fontSize: 11, color: colors.green, marginTop: 8 }}>
                    copied to clipboard
                  </MonoText>
                ) : null}
              </Card>
            ) : (
              <EmptyState text="No orchestrator for this task yet." />
            )}
            {workers.length > 0 ? <Text style={styles.sheetKindLabel}>WORKERS</Text> : null}
            <View style={{ gap: 10, marginTop: 8 }}>
              {workers.map((w) => (
                <Card key={w.id}>
                  <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8, marginBottom: 8 }}>
                    <Dot color={sessionDot(w.state, w.activity)} size={7} />
                    <MonoText style={{ fontSize: 13.5, fontWeight: '600', color: colors.text, flex: 1 }}>
                      {w.tmux_name}
                    </MonoText>
                    <MonoText style={{ fontSize: 11, color: colors.textFaint }}>{w.repo_id}</MonoText>
                    <Pressable onPress={() => setMenuFor(w)} hitSlop={8}>
                      <Text style={{ fontSize: 16, color: colors.textDim }}>⋯</Text>
                    </Pressable>
                  </View>
                  <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8, marginBottom: 11 }}>
                    <Badge {...sessionBadge(w.state, w.activity)} />
                    {w.pr_number ? (
                      <Text style={{ fontSize: 11.5, color: w.ci_state === 'failing' ? colors.redFg : colors.textDim }}>
                        PR #{w.pr_number}
                        {w.ci_state === 'passing' ? ' ✔' : w.ci_state === 'failing' ? ' ✖' : ''}
                      </Text>
                    ) : null}
                  </View>
                  <Pressable style={styles.workerTermBtn} onPress={() => openChat(w.id)}>
                    <Text style={{ fontSize: 13, fontWeight: '600', color: colors.text }}>💬 Open chat</Text>
                  </Pressable>
                </Card>
              ))}
            </View>
          </ScrollView>
          <ActionSheet
            visible={menuFor !== null}
            title={menuFor?.tmux_name ?? ''}
            onClose={() => setMenuFor(null)}
            actions={
              menuFor
                ? [
                    {
                      label: 'Kill session',
                      destructive: true,
                      disabled: menuFor.state !== 'running' && menuFor.state !== 'spawning',
                      onPress: () => confirmKill(menuFor, false),
                    },
                    {
                      label: 'Kill + remove worktree',
                      destructive: true,
                      disabled: menuFor.state === 'done',
                      onPress: () => confirmKill(menuFor, true),
                    },
                    {
                      label: restore.isPending ? 'Restoring…' : 'Restore session',
                      disabled: menuFor.state !== 'errored' && menuFor.state !== 'killed',
                      onPress: () => restore.mutate(menuFor.id),
                    },
                  ]
                : []
            }
          />
      </View>
    </BottomSheet>
  )
}

export default function TaskScreen() {
  const { id } = useLocalSearchParams<{ id: string }>()
  const taskId = Number(id)
  const detail = useTaskDetail(taskId)
  const questions = useTaskQuestions(taskId)
  const gates = useTaskGates(taskId)
  // null until the user picks a tab. The default follows the status the task
  // had when first loaded, then stays put — Go moving the task out of
  // brainstorm must not yank the user off the Brainstorm tab.
  const [picked, setTab] = useState<string | null>(null)
  const [initialTab, setInitialTab] = useState<string | null>(null)
  if (initialTab === null && detail.data) {
    setInitialTab(detail.data.status === 'brainstorm' ? 'brainstorm' : 'overview')
  }
  const tab = picked ?? initialTab ?? 'overview'
  const docs = useTaskDocs(taskId, tab === 'docs')
  const log = useTaskLog(taskId, tab === 'journal')
  const { data: allSessions } = useSessions(detail.data?.project_id)
  const messages = useMessages(detail.data?.session?.id)
  const sendMsg = useSendMessage()
  const [msgText, setMsgText] = useState('')
  const [sheetOpen, setSheetOpen] = useState(false)
  const [taskMenu, setTaskMenu] = useState(false)
  const [asking, setAsking] = useState(false)
  const insets = useSafeAreaInsets()
  const move = useMoveTask()
  const cancel = useCancelTask()
  const toast = useToast()
  const onErr = (e: unknown) => toast.show((e as Error).message)

  const t = detail.data
  const open = (questions.data ?? []).filter((q) => q.status === 'open')
  const resolved = (questions.data ?? []).filter((q) => q.status === 'resolved')
  const awaiting = open.filter((q) => q.your_turn)
  const orch = allSessions?.find((s) => s.id === t?.session?.id)
  const workers = (allSessions ?? []).filter((s) => s.kind === 'worker' && s.parent_id === t?.session?.id)
  const liveWorkers = workers.filter((w) => w.state === 'running' || w.state === 'spawning')
  const liveCount = (orch && (orch.state === 'running' || orch.state === 'spawning') ? 1 : 0) + liveWorkers.length
  const hasSessions = !!orch || workers.length > 0

  if (!t) {
    return (
      <SafeAreaView style={{ flex: 1, backgroundColor: colors.page }}>
        <EmptyState text={detail.isError ? 'Failed to load task.' : 'Loading…'} />
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
    ...(showBrainstormTab(t, questions.data ?? [], gates.data ?? [])
      ? [{ key: 'brainstorm', label: 'Brainstorm' }]
      : []),
    { key: 'overview', label: 'Overview' },
    { key: 'docs', label: 'Docs' },
    { key: 'journal', label: 'Journal' },
    ...(t.session ? [{ key: 'messages', label: 'Messages' }] : []),
  ]

  return (
    <SafeAreaView style={{ flex: 1, backgroundColor: colors.page }} edges={['top', 'bottom']}>
      <View style={styles.header}>
        <BackButton onPress={() => router.back()} />
        <MonoText style={{ fontSize: 13, fontWeight: '600', color: colors.textFaint }}>#{t.id}</MonoText>
        <Text style={styles.headerTitle} numberOfLines={1}>
          {t.title}
        </Text>
        <Badge {...STATUS_BADGE[t.status]} />
        <Pressable onPress={() => setTaskMenu(true)} hitSlop={8}>
          <Text style={{ fontSize: 18, color: colors.textDim }}>⋯</Text>
        </Pressable>
      </View>

      <ActionSheet
        visible={taskMenu}
        title={`#${t.id} ${t.title}`}
        onClose={() => setTaskMenu(false)}
        actions={[
          ...(['backlog', 'brainstorm', 'in_progress', 'review', 'done'] as const)
            .filter((s) => s !== t.status)
            .map((s) => ({
              label: `Move to ${STATUS_BADGE[s].label}`,
              onPress: () => move.mutate({ id: t.id, status: s }, { onError: onErr }),
            })),
          {
            label: 'Cancel task',
            destructive: true,
            disabled: t.status === 'done' || t.status === 'cancelled',
            onPress: () =>
              Alert.alert('Cancel task', `Cancel #${t.id} and kill all its sessions?`, [
                { text: 'Keep', style: 'cancel' },
                { text: 'Cancel task', style: 'destructive', onPress: () => cancel.mutate(t.id, { onError: onErr }) },
              ]),
          },
        ]}
      />

      <KeyboardAvoidingView style={{ flex: 1 }} behavior="padding">
        <ScrollView
          contentContainerStyle={{ paddingBottom: hasSessions ? 90 + insets.bottom : 24 }}
          refreshControl={
            <RefreshControl
              refreshing={false}
              onRefresh={() => {
                detail.refetch()
                questions.refetch()
                gates.refetch()
              }}
            />
          }
        >
          <View style={{ padding: 16, paddingBottom: 0 }}>
            <View style={styles.metaRow}>
              {t.feature_slug ? <MonoText style={{ fontSize: 12 }}>feature/{t.feature_slug}</MonoText> : null}
              <Text style={{ fontSize: 12, color: colors.textDim }}>
                {t.created_by === 'user' ? 'you' : 'orch'} · {ago(t.created_at)} · updated {ago(t.updated_at)}
              </Text>
            </View>
            {awaiting.length > 0 && tab !== 'questions' ? (
              <Pressable style={styles.awaitBanner} onPress={() => setTab('questions')}>
                <Badge label="? awaiting" fg={colors.amberDeep} bg={colors.amberBg} />
                <Text style={styles.awaitText} numberOfLines={2}>
                  {questionPreview(awaiting[0])}
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
                {!t.parent_id ? (
                  <GhostButton label="＋ Ask the orchestrator" onPress={() => setAsking(true)} />
                ) : null}
                {open.map((q) => (
                  <QuestionCard key={q.id} q={q} />
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
                      {questionPreview(q)}
                    </Text>
                    <Text style={{ fontSize: 11, color: colors.textFaint }}>{ago(q.resolved_at)}</Text>
                  </Card>
                ))}
              </View>
            ) : null}

            {tab === 'brainstorm' ? (
              <BrainstormTab
                taskId={t.id}
                questions={questions.data ?? []}
                gates={gates.data ?? []}
                gatesError={gates.isError ? (gates.error as Error).message : undefined}
                gatesLoading={gates.isPending}
              />
            ) : null}

            {tab === 'overview' ? (
              <View>
                {t.description ? <Text style={styles.description}>{t.description}</Text> : null}
                <Text style={styles.discussLabel}>SUBTASKS · DECOMPOSITION</Text>
                <View style={{ gap: 8, marginBottom: 10 }}>
                  {t.subtasks.map((s) => (
                    <Pressable key={s.id} onPress={() => router.push(`/task/${s.id}`)}>
                      <Card style={{ padding: 13 }}>
                        <View style={{ flexDirection: 'row', alignItems: 'center', gap: 9, marginBottom: 7 }}>
                          <MonoText style={{ fontSize: 11.5, fontWeight: '600', color: colors.textFaint }}>
                            #{s.id}
                          </MonoText>
                          <Text style={{ fontSize: 13.5, fontWeight: '600', flex: 1 }}>{s.title}</Text>
                        </View>
                        <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
                          <Badge {...STATUS_BADGE[s.status]} />
                          {s.repo_id ? <MonoText style={{ fontSize: 11.5 }}>{s.repo_id}</MonoText> : null}
                        </View>
                      </Card>
                    </Pressable>
                  ))}
                  {t.subtasks.length === 0 ? (
                    <Text style={{ fontSize: 13, color: colors.textFaint }}>No subtasks yet.</Text>
                  ) : null}
                </View>
              </View>
            ) : null}

            {tab === 'docs' ? (
              <View style={{ gap: 10 }}>
                {(docs.data ?? []).map((d) => (
                  <Card key={d.id}>
                    <View style={{ flexDirection: 'row', alignItems: 'center', gap: 9, marginBottom: 8 }}>
                      <Badge
                        label={d.kind}
                        fg={d.kind === 'spec' ? colors.indigoFg : d.kind === 'plan' ? colors.purpleFg : colors.slateFg}
                        bg={d.kind === 'spec' ? colors.indigoBg : d.kind === 'plan' ? colors.purpleBg : colors.slateBg}
                      />
                      <Text style={{ fontSize: 13.5, fontWeight: '600', flex: 1 }}>{d.title}</Text>
                    </View>
                    <Text style={{ fontSize: 11, color: colors.textFaint, marginBottom: 6 }}>
                      v{d.version} · {ago(d.created_at)}
                    </Text>
                    <Markdown>{d.body}</Markdown>
                  </Card>
                ))}
                {docs.isSuccess && docs.data.length === 0 ? <EmptyState text="No documents yet." /> : null}
              </View>
            ) : null}

            {tab === 'journal' ? (
              <View style={styles.timeline}>
                <View style={styles.timelineBar} />
                <View style={{ gap: 16 }}>
                  {(log.data ?? []).map((j) => {
                    const b = LOG_BADGE[j.kind] ?? LOG_BADGE.note
                    return (
                      <View key={j.id} style={{ position: 'relative' }}>
                        <View style={[styles.timelineDot, { backgroundColor: b.dot }]} />
                        <View style={{ flexDirection: 'row', alignItems: 'center', gap: 7, marginBottom: 4, flexWrap: 'wrap' }}>
                          <Badge label={j.kind} fg={b.fg} bg={b.bg} />
                          <MonoText style={{ fontSize: 11, color: colors.textFaint }}>{j.author || 'you'}</MonoText>
                          <Text style={{ fontSize: 11, color: colors.textFaint }}>{ago(j.created_at)}</Text>
                        </View>
                        <Text style={{ fontSize: 13.5, lineHeight: 20, color: colors.textBody }}>{j.body}</Text>
                      </View>
                    )
                  })}
                  {log.isSuccess && log.data.length === 0 ? <EmptyState text="Journal is empty." /> : null}
                </View>
              </View>
            ) : null}

            {tab === 'messages' && t.session ? (
              <View style={{ gap: 10 }}>
                {(messages.data ?? [])
                  .slice()
                  .reverse()
                  .map((m) => {
                    const fromUser = !m.from
                    return (
                      <View key={m.id} style={{ alignItems: fromUser ? 'flex-end' : 'flex-start' }}>
                        <View
                          style={[
                            styles.bubble,
                            fromUser
                              ? { backgroundColor: colors.indigoBg, borderColor: colors.indigoBorder }
                              : { backgroundColor: colors.card, borderColor: colors.border },
                          ]}
                        >
                          <View style={{ flexDirection: 'row', gap: 7, marginBottom: 3 }}>
                            <MonoText
                              style={{ fontSize: 11.5, fontWeight: '600', color: fromUser ? colors.indigoFg : colors.textMid }}
                            >
                              {fromUser ? 'you' : m.from}
                            </MonoText>
                            <Text style={{ fontSize: 10.5, color: colors.textFaint }}>{ago(m.created_at)}</Text>
                          </View>
                          <Text style={{ fontSize: 13.5, lineHeight: 21 }}>{m.body}</Text>
                          {fromUser && m.status === 'delivered' ? (
                            <Text style={{ fontSize: 10.5, color: colors.green, marginTop: 3 }}>✓ delivered</Text>
                          ) : null}
                        </View>
                      </View>
                    )
                  })}
                <View style={{ flexDirection: 'row', gap: 8, alignItems: 'flex-end', marginTop: 6 }}>
                  <TextInput
                    style={styles.msgInput}
                    placeholder="Message the orchestrator…"
                    placeholderTextColor={colors.textFaint}
                    value={msgText}
                    onChangeText={setMsgText}
                    multiline
                  />
                  <PrimaryButton
                    label="Send"
                    disabled={!msgText.trim() || sendMsg.isPending}
                    onPress={() =>
                      sendMsg.mutate(
                        { to: t.session!.id, body: msgText.trim() },
                        { onSuccess: () => setMsgText(''), onError: onErr },
                      )
                    }
                    style={{ height: 44, paddingHorizontal: 16, borderRadius: radius.lg }}
                  />
                </View>
              </View>
            ) : null}
          </View>
        </ScrollView>
      </KeyboardAvoidingView>

      {hasSessions ? (
        <Pressable
          style={[styles.sessionsBar, { bottom: Math.max(20, insets.bottom + 12) }]}
          onPress={() => setSheetOpen(true)}
        >
          <Dot color={liveCount > 0 ? '#22c55e' : colors.slate} size={8} />
          <Text style={{ color: '#fff', fontSize: 14, fontWeight: '600' }}>
            Sessions · {orch ? '1 orch' : '0 orch'}
            {workers.length > 0 ? ` + ${workers.length} workers` : ''}
          </Text>
        </Pressable>
      ) : null}
      <SessionsSheet visible={sheetOpen} orch={orch} workers={workers} onClose={() => setSheetOpen(false)} />
      <AskQuestionSheet visible={asking} taskId={t.id} onClose={() => setAsking(false)} />
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
  headerTitle: { fontSize: 15, fontWeight: '700', color: colors.text, flex: 1, letterSpacing: -0.15 },
  metaRow: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', gap: 7, marginBottom: 16 },
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
  discussLabel: {
    fontSize: 10.5,
    fontWeight: '600',
    color: colors.textFaint,
    letterSpacing: 0.5,
    marginBottom: 8,
    marginTop: 4,
  },
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
  description: { fontSize: 14.5, lineHeight: 24, color: colors.textBody, marginBottom: 20 },
  timeline: { position: 'relative', paddingLeft: 18 },
  timelineBar: { position: 'absolute', left: 4, top: 6, bottom: 6, width: 2, backgroundColor: colors.border },
  timelineDot: {
    position: 'absolute',
    left: -18,
    top: 3,
    width: 10,
    height: 10,
    borderRadius: 5,
    borderWidth: 2,
    borderColor: colors.page,
  },
  bubble: { maxWidth: '82%', borderWidth: 1, borderRadius: radius.xl, padding: 10, paddingHorizontal: 12 },
  msgInput: {
    flex: 1,
    minHeight: 44,
    maxHeight: 120,
    padding: 12,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: radius.lg,
    fontSize: 13.5,
    color: colors.text,
    backgroundColor: colors.card,
  },
  sessionsBar: {
    position: 'absolute',
    left: 16,
    right: 16,
    bottom: 20,
    height: 50,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 9,
    backgroundColor: colors.ink,
    borderRadius: radius.xl,
    shadowColor: '#000',
    shadowOpacity: 0.24,
    shadowRadius: 11,
    shadowOffset: { width: 0, height: 6 },
    elevation: 6,
  },
  sheetKindLabel: { fontSize: 10.5, fontWeight: '600', color: colors.textFaint, letterSpacing: 0.5 },
  workerTermBtn: {
    height: 38,
    backgroundColor: '#f4f4f2',
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: radius.md,
    alignItems: 'center',
    justifyContent: 'center',
  },
})
