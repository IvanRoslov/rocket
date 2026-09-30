import { useEffect, useMemo, useRef, useState } from 'react'
import { AppState, Pressable, RefreshControl, ScrollView, StyleSheet, Text, View } from 'react-native'
import { SafeAreaView } from 'react-native-safe-area-context'
import { useThreadAnswer, useThreads, type ThreadAnswer } from '../../src/api/queries'
import type { ThreadInboxEntry } from '../../src/api/types'
import { ConnectionBanner } from '../../src/components/ConnectionBanner'
import { ThreadCard } from '../../src/components/ThreadCard'
import { useToast } from '../../src/components/Toast'
import { EmptyState, SectionTitle } from '../../src/components/ui'
import { createDeferredQueue } from '../../src/lib/deferred'
import { otherOpen, waitingOnYou } from '../../src/lib/questions'
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

  const all = threads.data ?? []
  const mine = useMemo(() => waitingOnYou(all), [all])
  const visible = mine.filter((t) => !hidden.has(t.id)).length
  const others = useMemo(() => otherOpen(all), [all])

  return (
    <SafeAreaView style={{ flex: 1, backgroundColor: colors.page }} edges={['top']}>
      <ConnectionBanner />
      <ScrollView
        contentContainerStyle={{ padding: 14, paddingBottom: 90 }}
        refreshControl={<RefreshControl refreshing={threads.isRefetching} onRefresh={() => threads.refetch()} />}
      >
        <Text style={styles.h1}>Questions</Text>
        <SectionTitle>{`Waiting on you (${visible})`}</SectionTitle>
        {/* Answered cards stay mounted (display none) so typed text survives a failed send. */}
        {mine.map((t) => (
          <View key={t.id} style={hidden.has(t.id) ? { display: 'none' } : undefined}>
            <ThreadCard thread={t} onAnswer={onAnswer} />
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
    </SafeAreaView>
  )
}

const styles = StyleSheet.create({
  h1: { fontSize: 22, fontWeight: '700', letterSpacing: -0.3, marginBottom: 12 },
  othersToggle: { fontSize: 13, fontWeight: '600', color: colors.textDim },
  undoBar: {
    position: 'absolute', left: 14, right: 14, bottom: 14, flexDirection: 'row', alignItems: 'center', gap: 12,
    backgroundColor: colors.text, borderRadius: 12, paddingHorizontal: 16, paddingVertical: 12,
  },
  undoText: { flex: 1, color: '#fff', fontSize: 13.5 },
  undoBtn: { color: '#9ec5ff', fontWeight: '700', fontSize: 13.5 },
})
