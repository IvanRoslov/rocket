import { router, useLocalSearchParams } from 'expo-router'
import { useMemo, useRef, useState } from 'react'
import {
  FlatList,
  KeyboardAvoidingView,
  Platform,
  Pressable,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native'
import { SafeAreaView } from 'react-native-safe-area-context'
import { useChatFeed } from '../../src/api/chat'
import { useMessages, useSendMessage } from '../../src/api/queries'
import type { ChatEntry } from '../../src/api/types'
import { Markdown } from '../../src/components/Markdown'
import { useToast } from '../../src/components/Toast'
import { BackButton, Badge, Dot, MonoText, PrimaryButton } from '../../src/components/ui'
import { ago, sessionBadge, sessionDot } from '../../src/lib/format'
import { colors, mono, radius } from '../../src/theme'

const LARGE_POINTER = '[large message]'

interface OutgoingMsg {
  msgId: number
  body: string
}

type Row =
  | { kind: 'entry'; key: string; entry: ChatEntry }
  | { kind: 'outgoing'; key: string; body: string; status: string; reason?: string }

function EntryBubble({ entry }: { entry: ChatEntry }) {
  if (entry.role === 'tool') {
    return (
      <View style={styles.toolRow}>
        <Text style={styles.toolText} numberOfLines={1}>
          <Text style={{ color: colors.textMid, fontWeight: '600' }}>▸ {entry.tool_name ?? 'tool'} </Text>
          {entry.text}
        </Text>
      </View>
    )
  }
  const isUser = entry.role === 'user'
  return (
    <View style={{ alignItems: isUser ? 'flex-end' : 'flex-start', marginVertical: 3 }}>
      <View
        style={[
          styles.bubble,
          isUser
            ? { backgroundColor: colors.indigoBg, borderColor: colors.indigoBorder }
            : { backgroundColor: colors.card, borderColor: colors.border, maxWidth: '94%' },
        ]}
      >
        {isUser ? <Text style={styles.bubbleText}>{entry.text}</Text> : <Markdown>{entry.text}</Markdown>}
        {entry.ts > 0 ? <Text style={styles.bubbleMeta}>{ago(entry.ts)}</Text> : null}
      </View>
    </View>
  )
}

export default function ChatScreen() {
  const { id } = useLocalSearchParams<{ id: string }>()
  const { entries, session, error, loading } = useChatFeed(id)
  const send = useSendMessage()
  const toast = useToast()
  const { data: queueMessages } = useMessages(id)
  const [text, setText] = useState('')
  const [outgoing, setOutgoing] = useState<OutgoingMsg[]>([])
  const [showTools, setShowTools] = useState(false)
  const listRef = useRef<FlatList<Row>>(null)

  const canWrite = session?.kind === 'orchestrator' && session.state === 'running'
  const toolCount = useMemo(() => entries.filter((e) => e.role === 'tool').length, [entries])

  const rows = useMemo<Row[]>(() => {
    const transcriptUserTexts = new Set(
      entries.filter((e) => e.role === 'user').map((e) => e.text),
    )
    const items: Row[] = entries
      // The daemon injects a "[large message] …" pointer instead of a big
      // body; our optimistic bubble keeps the real text, so hide pointers.
      .filter((e) => !(e.role === 'user' && e.text.startsWith(LARGE_POINTER)))
      // Tool calls are noise for the phone view; hidden unless toggled on.
      .map((e, i) => ({ e, i }))
      .filter(({ e }) => showTools || e.role !== 'tool')
      .map(({ e, i }) => ({ kind: 'entry' as const, key: `e${i}`, entry: e }))
    for (const o of outgoing) {
      // Once the reply shows up in the transcript, the optimistic bubble is
      // redundant — unless it was delivered as a file (pointer hidden above).
      if (transcriptUserTexts.has(o.body)) continue
      const m = queueMessages?.find((qm) => qm.id === o.msgId)
      items.push({
        kind: 'outgoing',
        key: `o${o.msgId}`,
        body: o.body,
        status: m?.status ?? 'queued',
        reason: m?.reason,
      })
    }
    return items.reverse() // FlatList is inverted
  }, [entries, outgoing, queueMessages, showTools])

  const submit = () => {
    const body = text.trim()
    if (!body) return
    send.mutate(
      { to: id, body },
      {
        onSuccess: (m) => {
          setOutgoing((prev) => [...prev, { msgId: m.id, body }])
          setText('')
        },
        onError: (e) => toast.show((e as Error).message),
      },
    )
  }

  return (
    <SafeAreaView style={{ flex: 1, backgroundColor: colors.page }} edges={['top', 'bottom']}>
      <View style={styles.header}>
        <BackButton onPress={() => router.back()} />
        <Dot color={session ? sessionDot(session.state, session.activity) : colors.slate} size={9} />
        <MonoText style={styles.title} numberOfLines={1}>
          {session?.id ?? id}
        </MonoText>
        {session ? <Badge {...sessionBadge(session.state, session.activity)} /> : null}
        <Pressable
          hitSlop={8}
          onPress={() => setShowTools((v) => !v)}
          style={[styles.toolsToggle, showTools && { backgroundColor: colors.slateBg, borderColor: colors.border }]}
        >
          <Text style={{ fontSize: 11, fontWeight: '600', color: showTools ? colors.textMid : colors.textFaint }}>
            ⚙ {toolCount}
          </Text>
        </Pressable>
      </View>

      <KeyboardAvoidingView style={{ flex: 1 }} behavior={Platform.OS === 'ios' ? 'padding' : undefined}>
        <FlatList
          ref={listRef}
          inverted
          data={rows}
          keyExtractor={(r) => r.key}
          contentContainerStyle={{ padding: 14, paddingBottom: 18 }}
          renderItem={({ item }) =>
            item.kind === 'entry' ? (
              <EntryBubble entry={item.entry} />
            ) : (
              <View style={{ alignItems: 'flex-end', marginVertical: 3 }}>
                <View style={[styles.bubble, { backgroundColor: colors.indigoBg, borderColor: colors.indigoBorder }]}>
                  <Text style={styles.bubbleText}>{item.body}</Text>
                  <Text
                    style={[
                      styles.bubbleMeta,
                      item.status === 'failed' ? { color: colors.redFg } : null,
                      item.status === 'delivered' ? { color: colors.green } : null,
                    ]}
                  >
                    {item.status === 'delivered'
                      ? '✓ delivered'
                      : item.status === 'failed'
                        ? `✗ ${item.reason ?? 'failed'}`
                        : '⏳ sending…'}
                  </Text>
                </View>
              </View>
            )
          }
          ListEmptyComponent={
            <View style={{ transform: [{ scaleY: -1 }], padding: 30, alignItems: 'center' }}>
              <Text style={{ color: colors.textFaint, fontSize: 13.5 }}>
                {loading ? 'Loading transcript…' : error ? `Error: ${error}` : 'No transcript yet.'}
              </Text>
            </View>
          }
        />

        {canWrite ? (
          <View style={styles.composer}>
            <TextInput
              style={styles.input}
              placeholder="Message the orchestrator…"
              placeholderTextColor={colors.textFaint}
              value={text}
              onChangeText={setText}
              multiline
            />
            <PrimaryButton
              label="Send"
              disabled={!text.trim() || send.isPending}
              onPress={submit}
              style={{ height: 44, paddingHorizontal: 16, borderRadius: radius.lg }}
            />
          </View>
        ) : (
          <View style={styles.readonlyBar}>
            <Text style={{ fontSize: 12, color: colors.textFaint }}>
              {session?.kind === 'worker'
                ? 'Workers are read-only — talk to the orchestrator instead.'
                : 'Session is not running — transcript is read-only.'}
            </Text>
          </View>
        )}
      </KeyboardAvoidingView>
    </SafeAreaView>
  )
}

const styles = StyleSheet.create({
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 9,
    height: 54,
    paddingHorizontal: 14,
    backgroundColor: colors.card,
    borderBottomWidth: 1,
    borderBottomColor: colors.border,
  },
  title: { fontSize: 14, fontWeight: '600', color: colors.text, flex: 1 },
  toolsToggle: {
    paddingHorizontal: 8,
    paddingVertical: 4,
    borderRadius: 7,
    borderWidth: 1,
    borderColor: 'transparent',
  },
  toolRow: { marginVertical: 2, paddingHorizontal: 4 },
  toolText: { fontFamily: mono, fontSize: 11, color: colors.textFaint },
  bubble: {
    maxWidth: '85%',
    borderWidth: 1,
    borderRadius: radius.xl,
    padding: 10,
    paddingHorizontal: 12,
  },
  bubbleText: { fontSize: 13.5, lineHeight: 20, color: colors.text },
  bubbleMeta: { fontSize: 10.5, color: colors.textFaint, marginTop: 3 },
  composer: {
    flexDirection: 'row',
    alignItems: 'flex-end',
    gap: 8,
    padding: 12,
    backgroundColor: colors.card,
    borderTopWidth: 1,
    borderTopColor: colors.border,
  },
  input: {
    flex: 1,
    minHeight: 44,
    maxHeight: 120,
    padding: 12,
    paddingTop: 12,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: radius.lg,
    fontSize: 13.5,
    color: colors.text,
    backgroundColor: colors.page,
  },
  readonlyBar: {
    alignItems: 'center',
    padding: 10,
    backgroundColor: colors.card,
    borderTopWidth: 1,
    borderTopColor: colors.border,
  },
})
