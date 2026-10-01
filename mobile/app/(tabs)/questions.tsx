import { useEffect, useMemo, useRef, useState } from 'react'
import {
  AppState,
  KeyboardAvoidingView,
  Pressable,
  RefreshControl,
  ScrollView,
  StyleSheet,
  Text,
  View,
  type LayoutChangeEvent,
} from 'react-native'
import { router } from 'expo-router'
import { SafeAreaView } from 'react-native-safe-area-context'
import { useThreadAnswer, useThreads, type ThreadAnswer } from '../../src/api/queries'
import type { ThreadInboxEntry } from '../../src/api/types'
import { ConnectionBanner } from '../../src/components/ConnectionBanner'
import { ThreadCard } from '../../src/components/ThreadCard'
import { useToast } from '../../src/components/Toast'
import { Card, EmptyState, SectionTitle } from '../../src/components/ui'
import { isBrainstorm } from '../../src/lib/brainstorm'
import { createDeferredQueue } from '../../src/lib/deferred'
import { otherOpen, revealOffset, waitingOnYou } from '../../src/lib/questions'
import { stormGroups, stormHref, stormLabel } from '../../src/lib/storm'
import { colors } from '../../src/theme'

/** How long a tap can be taken back before the answer goes to the daemon (it has no undo). */
const UNDO_MS = 5000

export default function QuestionsScreen() {
  const threads = useThreads()
  const answer = useThreadAnswer()
  const toast = useToast()
  const [showOthers, setShowOthers] = useState(false)
  // Threads answered locally and not yet confirmed by a refetch: hidden from
  // the lists; the latest one is shown as the undo bar.
  const [hidden, setHidden] = useState<Set<number>>(new Set())
  const [undo, setUndo] = useState<{ id: number; label: string } | null>(null)
  const queue = useRef(createDeferredQueue(UNDO_MS)).current

  // Keeping the focused answer above the keyboard: KAV shrinks the list, but
  // nothing scrolls the card back into the smaller window on its own.
  const listRef = useRef<ScrollView>(null)
  const view = useRef({ offset: 0, height: 0 })
  const cards = useRef(new Map<number, { y: number; height: number }>())
  const focused = useRef<number | null>(null)
  const reveal = () => {
    const card = focused.current === null ? undefined : cards.current.get(focused.current)
    const y = card && revealOffset(card, view.current)
    if (y != null) listRef.current?.scrollTo({ y, animated: true })
  }
  const onCardLayout = (id: number) => (e: LayoutChangeEvent) => {
    const { y, height } = e.nativeEvent.layout
    cards.current.set(id, { y, height })
    if (focused.current === id) reveal()
  }

  // Leaving is not an Undo: unmount and going to background commit the pending answer.
  useEffect(() => {
    const sub = AppState.addEventListener('change', (s) => {
      // iOS reports 'inactive' for Control Center and the app switcher: not leaving yet.
      if (s === 'background') queue.flush()
    })
    return () => {
      sub.remove()
      queue.dispose()
    }
  }, [queue])

  const unhide = (id: number) =>
    setHidden((prev) => {
      const next = new Set(prev)
      next.delete(id)
      return next
    })

  const onAnswer = (thread: ThreadInboxEntry, a: ThreadAnswer, label: string) => {
    setHidden((prev) => new Set(prev).add(thread.id))
    setUndo({ id: thread.id, label })
    queue.schedule(() => {
      setUndo((u) => (u?.id === thread.id ? null : u))
      // mutateAsync, not mutate(…, {onError}): per-call callbacks of a shared
      // mutation are dropped when another answer starts or the screen unmounts.
      answer
        .mutateAsync({ thread, answer: a })
        // Unhide only once the refetch has landed: no flash of the answered
        // card, and a thread reopened later (same id) is not hidden forever.
        .then(() => threads.refetch())
        .catch((e) => toast.show((e as Error).message))
        .finally(() => unhide(thread.id))
    })
  }

  const onUndo = () => {
    if (!undo) return
    queue.cancel()
    unhide(undo.id)
    setUndo(null)
  }

  const data = threads.data
  // Storm threads are answered only in the task's Brainstorm tab (task #4901,
  // spec v2 §3.1): none is listed here, one row per task stands for them.
  const all = useMemo(() => (data ?? []).filter((t) => !isBrainstorm(t)), [data])
  const storms = useMemo(() => stormGroups(data ?? []), [data])
  const mine = useMemo(() => waitingOnYou(all), [all])
  const visible = mine.filter((t) => !hidden.has(t.id)).length + storms.length
  const others = useMemo(() => otherOpen(all), [all])

  return (
    <SafeAreaView style={{ flex: 1, backgroundColor: colors.page }} edges={['top']}>
      <ConnectionBanner />
      {/* No keyboardVerticalOffset: this scene already ends at the tab bar,
          so KAV pads only by the part of the keyboard that overlaps it. The
          inner View shrinks with it, lifting the absolute undo bar too. */}
      <KeyboardAvoidingView testID="questions-keyboard" style={{ flex: 1 }} behavior="padding">
        <View style={{ flex: 1 }}>
          <ScrollView
            ref={listRef}
            testID="questions-list"
            keyboardShouldPersistTaps="handled"
            onLayout={(e) => {
              view.current.height = e.nativeEvent.layout.height
              reveal()
            }}
            onScroll={(e) => {
              view.current.offset = e.nativeEvent.contentOffset.y
            }}
            scrollEventThrottle={16}
            contentContainerStyle={{ padding: 14, paddingBottom: 90 }}
            refreshControl={<RefreshControl refreshing={threads.isRefetching} onRefresh={() => threads.refetch()} />}
          >
            <Text style={styles.h1}>Questions</Text>
            <SectionTitle>{`Waiting on you (${visible})`}</SectionTitle>
            {storms.map((g) => (
              <Pressable key={`storm-${g.taskId}`} onPress={() => router.navigate(stormHref(g) as never)}>
                <Card style={styles.stormRow}>
                  <Text style={styles.stormText}>{stormLabel(g)}</Text>
                  <Text style={styles.stormHint}>Answer in the Brainstorm tab →</Text>
                </Card>
              </Pressable>
            ))}
            {/* Answered cards stay mounted (display none) so typed text survives a failed send. */}
            {mine.map((t) => (
              <View
                key={t.id}
                testID={`thread-${t.id}`}
                onLayout={onCardLayout(t.id)}
                style={hidden.has(t.id) ? { display: 'none' } : undefined}
              >
                <ThreadCard
                  thread={t}
                  onAnswer={onAnswer}
                  busy={hidden.has(t.id)}
                  onInputFocus={() => {
                    focused.current = t.id
                    reveal()
                  }}
                  onInputBlur={() => {
                    if (focused.current === t.id) focused.current = null
                  }}
                />
              </View>
            ))}
            {threads.isSuccess && visible === 0 ? <EmptyState text="No questions waiting on you." /> : null}
            {others.length > 0 ? (
              <Pressable onPress={() => setShowOthers((v) => !v)} style={{ paddingVertical: 10 }}>
                <Text style={styles.othersToggle}>
                  {showOthers ? '▾' : '▸'} Other open ({others.length})
                </Text>
              </Pressable>
            ) : null}
            {showOthers ? others.map((t) => <ThreadCard key={t.id} thread={t} onAnswer={onAnswer} />) : null}
          </ScrollView>
          {undo ? (
            <View style={styles.undoBar}>
              <Text style={styles.undoText} numberOfLines={1}>Answered: {undo.label}</Text>
              <Pressable onPress={onUndo} hitSlop={8}>
                <Text style={styles.undoBtn}>Undo</Text>
              </Pressable>
            </View>
          ) : null}
        </View>
      </KeyboardAvoidingView>
    </SafeAreaView>
  )
}

const styles = StyleSheet.create({
  h1: { fontSize: 22, fontWeight: '700', letterSpacing: -0.3, marginBottom: 12 },
  stormRow: { padding: 14, marginBottom: 12, gap: 4 },
  stormText: { fontSize: 14, fontWeight: '600', color: colors.text },
  stormHint: { fontSize: 12, color: colors.amberDeep },
  othersToggle: { fontSize: 13, fontWeight: '600', color: colors.textDim },
  undoBar: {
    position: 'absolute', left: 14, right: 14, bottom: 14, flexDirection: 'row', alignItems: 'center', gap: 12,
    backgroundColor: colors.text, borderRadius: 12, paddingHorizontal: 16, paddingVertical: 12,
  },
  undoText: { flex: 1, color: '#fff', fontSize: 13.5 },
  undoBtn: { color: '#9ec5ff', fontWeight: '700', fontSize: 13.5 },
})
