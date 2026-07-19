import * as Clipboard from 'expo-clipboard'
import { router, useLocalSearchParams } from 'expo-router'
import { useState } from 'react'
import {
  KeyboardAvoidingView,
  Modal,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native'
import { SafeAreaView } from 'react-native-safe-area-context'
import {
  useMessages,
  useQuestionAnswer,
  useQuestionReply,
  useSendMessage,
  useSessions,
  useTaskDetail,
  useTaskDocs,
  useTaskLog,
  useTaskQuestions,
} from '../../src/api/queries'
import type { Question, Session, TaskLogKind, TaskStatus } from '../../src/api/types'
import { Badge, Card, ChipTabs, Dot, EmptyState, GhostButton, MonoText, PrimaryButton } from '../../src/components/ui'
import { ago, sessionBadge, sessionDot } from '../../src/lib/format'
import { colors, mono, radius } from '../../src/theme'

const STATUS_BADGE: Record<TaskStatus, { label: string; fg: string; bg: string }> = {
  backlog: { label: 'Backlog', fg: colors.slateFg, bg: colors.slateBg },
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

function QuestionCard({ q }: { q: Question }) {
  const reply = useQuestionReply()
  const answer = useQuestionAnswer()
  const [text, setText] = useState('')
  const [ctxOpen, setCtxOpen] = useState(false)
  const busy = reply.isPending || answer.isPending

  const send = (final: boolean) => {
    const body = text.trim()
    if (!body) return
    const m = final ? answer : reply
    m.mutate({ id: q.id, body }, { onSuccess: () => setText('') })
  }

  return (
    <View style={styles.qCard}>
      <View style={styles.qHead}>
        <Badge label={`Q${q.ordinal}`} fg={colors.amberDeep} bg={colors.amberBg} />
        <Badge
          label={q.whose_turn === 'user' ? 'awaiting you' : 'waiting for orch'}
          fg={colors.amberDeep}
          bg={colors.amberBg}
        />
        <View style={{ flex: 1 }} />
        <MonoText style={{ fontSize: 11, color: '#a1621a' }}>orch</MonoText>
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
                    {isUser ? 'Y' : 'O'}
                  </Text>
                </View>
                <Text style={{ fontSize: 12.5, fontWeight: '600', color: isUser ? colors.indigoFg : colors.amberDeep }}>
                  {isUser ? 'you' : 'orch'}
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
            placeholder="Write a reply or give your final answer…"
            placeholderTextColor={colors.textFaint}
            value={text}
            onChangeText={setText}
            multiline
          />
          <View style={{ flexDirection: 'row', gap: 9 }}>
            <GhostButton label="Clarify" onPress={() => send(false)} style={{ flex: 1 }} />
            <PrimaryButton
              label={busy ? 'Sending…' : 'Answer & close'}
              disabled={busy || !text.trim()}
              onPress={() => send(true)}
              style={{ flex: 1 }}
            />
          </View>
        </View>
      </View>
    </View>
  )
}

function SessionsSheet({
  orch,
  workers,
  onClose,
}: {
  orch?: Session
  workers: Session[]
  onClose: () => void
}) {
  const [copied, setCopied] = useState(false)
  const copyAttach = (name: string) => {
    Clipboard.setStringAsync(`rocket attach ${name}`)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }
  const openTerm = (id: string) => {
    onClose()
    router.navigate(`/term/${id}`)
  }

  return (
    <Modal transparent animationType="slide" onRequestClose={onClose}>
      <Pressable style={styles.backdrop} onPress={onClose}>
        <Pressable style={styles.sheet} onPress={() => {}}>
          <View style={styles.grabber} />
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
                  <Badge {...sessionBadge(orch.state, orch.activity)} />
                </View>
                <MonoText style={{ fontSize: 14, fontWeight: '600', color: colors.text, marginBottom: 11 }}>
                  {orch.tmux_name}
                </MonoText>
                <View style={{ flexDirection: 'row', gap: 7 }}>
                  <PrimaryButton
                    label="▣ Open terminal"
                    onPress={() => openTerm(orch.id)}
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
                  <Pressable style={styles.workerTermBtn} onPress={() => openTerm(w.id)}>
                    <Text style={{ fontSize: 13, fontWeight: '600', color: colors.text }}>▣ Open terminal</Text>
                  </Pressable>
                </Card>
              ))}
            </View>
          </ScrollView>
        </Pressable>
      </Pressable>
    </Modal>
  )
}

export default function TaskScreen() {
  const { id } = useLocalSearchParams<{ id: string }>()
  const taskId = Number(id)
  const detail = useTaskDetail(taskId)
  const questions = useTaskQuestions(taskId)
  const [tab, setTab] = useState('overview')
  const docs = useTaskDocs(taskId, tab === 'docs')
  const log = useTaskLog(taskId, tab === 'journal')
  const { data: allSessions } = useSessions(detail.data?.project_id)
  const messages = useMessages(detail.data?.session?.id)
  const sendMsg = useSendMessage()
  const [msgText, setMsgText] = useState('')
  const [sheetOpen, setSheetOpen] = useState(false)

  const t = detail.data
  const open = (questions.data ?? []).filter((q) => q.status === 'open')
  const resolved = (questions.data ?? []).filter((q) => q.status === 'resolved')
  const awaiting = open.filter((q) => q.whose_turn === 'user')
  const orch = allSessions?.find((s) => s.id === t?.session?.id)
  const workers = (allSessions ?? []).filter(
    (s) => s.kind === 'worker' && s.parent_id === t?.session?.id && s.state === 'running',
  )
  const liveCount = (orch && orch.state === 'running' ? 1 : 0) + workers.length

  if (!t) {
    return (
      <SafeAreaView style={{ flex: 1, backgroundColor: colors.page }}>
        <EmptyState text={detail.isError ? 'Failed to load task.' : 'Loading…'} />
      </SafeAreaView>
    )
  }

  const chips = [
    ...(open.length > 0 ? [{ key: 'questions', label: 'Questions', count: open.length, warn: awaiting.length > 0 }] : []),
    { key: 'overview', label: 'Overview' },
    { key: 'docs', label: 'Docs' },
    { key: 'journal', label: 'Journal' },
    ...(t.session ? [{ key: 'messages', label: 'Messages' }] : []),
  ]

  return (
    <SafeAreaView style={{ flex: 1, backgroundColor: colors.page }} edges={['top', 'bottom']}>
      <View style={styles.header}>
        <Pressable onPress={() => router.back()}>
          <Text style={{ fontSize: 20, color: colors.textDim }}>‹</Text>
        </Pressable>
        <MonoText style={{ fontSize: 13, fontWeight: '600', color: colors.textFaint }}>#{t.id}</MonoText>
        <Text style={styles.headerTitle} numberOfLines={1}>
          {t.title}
        </Text>
        <Badge {...STATUS_BADGE[t.status]} />
      </View>

      <KeyboardAvoidingView style={{ flex: 1 }} behavior={Platform.OS === 'ios' ? 'padding' : undefined}>
        <ScrollView contentContainerStyle={{ paddingBottom: liveCount > 0 ? 90 : 24 }}>
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
                {open.map((q) => (
                  <QuestionCard key={q.id} q={q} />
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
                    <Text style={{ fontSize: 13, lineHeight: 20, color: colors.textMid }} numberOfLines={6}>
                      {d.body}
                    </Text>
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
                        { onSuccess: () => setMsgText('') },
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

      {liveCount > 0 ? (
        <Pressable style={styles.sessionsBar} onPress={() => setSheetOpen(true)}>
          <Dot color="#22c55e" size={8} />
          <Text style={{ color: '#fff', fontSize: 14, fontWeight: '600' }}>
            Sessions · {orch ? '1 orch' : '0 orch'}
            {workers.length > 0 ? ` + ${workers.length} workers` : ''}
          </Text>
        </Pressable>
      ) : null}
      {sheetOpen ? <SessionsSheet orch={orch} workers={workers} onClose={() => setSheetOpen(false)} /> : null}
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
  backdrop: { flex: 1, backgroundColor: 'rgba(15,15,17,.4)', justifyContent: 'flex-end' },
  sheet: {
    backgroundColor: '#fbfbfa',
    borderTopLeftRadius: 18,
    borderTopRightRadius: 18,
    padding: 16,
    paddingBottom: 32,
    maxHeight: '82%',
  },
  grabber: {
    alignSelf: 'center',
    width: 38,
    height: 5,
    borderRadius: 3,
    backgroundColor: '#d4d4d1',
    marginBottom: 14,
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
