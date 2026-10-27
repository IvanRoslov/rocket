import { useMemo, useState } from 'react'
import { Pressable, RefreshControl, ScrollView, StyleSheet, Text, View } from 'react-native'
import { router } from 'expo-router'
import { SafeAreaView } from 'react-native-safe-area-context'
import { useBrainstorms } from '../../src/api/queries'
import type { BrainstormEntry } from '../../src/api/types'
import { ConnectionBanner } from '../../src/components/ConnectionBanner'
import { Card, ChipTabs, EmptyState } from '../../src/components/ui'
import {
  brainstormCounts,
  brainstormHref,
  filterBrainstorms,
  isMidWorkSpecChange,
  type BrainstormFilter,
} from '../../src/lib/brainstorms'
import { ago } from '../../src/lib/format'
import { colors } from '../../src/theme'

function StormCard({ e }: { e: BrainstormEntry }) {
  return (
    <Pressable onPress={() => router.navigate(brainstormHref(e) as never)}>
      <Card style={styles.card}>
        <Text style={styles.title}>{`#${e.task_id} ${e.task_title}`}</Text>
        <Text style={styles.meta}>
          {[e.project_id, e.task_status.replace('_', ' '), ago(e.waiting_since)].filter(Boolean).join(' · ')}
        </Text>
        <View style={styles.waits}>
          {e.questions_waiting > 0 ? (
            <Text style={styles.wait}>
              {e.questions_waiting === 1 ? '1 question waiting' : `${e.questions_waiting} questions waiting`}
            </Text>
          ) : null}
          {e.questions_waiting > 0 && e.questions_stale ? <Text style={styles.tag}>STALE</Text> : null}
          {e.pending_gate ? <Text style={styles.wait}>{`Needs Go · spec v${e.pending_gate.spec_version}`}</Text> : null}
          {isMidWorkSpecChange(e) ? <Text style={styles.tag}>spec change mid-work</Text> : null}
        </View>
      </Card>
    </Pressable>
  )
}

export default function StormsScreen() {
  const storms = useBrainstorms()
  const [filter, setFilter] = useState<BrainstormFilter>('all')
  const data = storms.data
  const counts = useMemo(() => brainstormCounts(data ?? []), [data])
  const rows = useMemo(() => filterBrainstorms(data ?? [], filter), [data, filter])

  return (
    <SafeAreaView style={{ flex: 1, backgroundColor: colors.page }} edges={['top']}>
      <ConnectionBanner />
      <ScrollView
        contentContainerStyle={{ padding: 14, paddingBottom: 90 }}
        refreshControl={<RefreshControl refreshing={storms.isRefetching} onRefresh={() => storms.refetch()} />}
      >
        <Text style={styles.h1}>Storms</Text>
        <ChipTabs
          chips={[
            { key: 'all', label: 'All' },
            { key: 'answer', label: `Needs answer (${counts.answer})` },
            { key: 'go', label: `Needs Go (${counts.go})` },
          ]}
          active={filter}
          onChange={(k) => setFilter(k as BrainstormFilter)}
        />
        <View style={{ height: 12 }} />
        {rows.map((e) => (
          <StormCard key={e.task_id} e={e} />
        ))}
        {storms.isSuccess && (data ?? []).length === 0 ? <EmptyState text="No brainstorms are waiting on you" /> : null}
      </ScrollView>
    </SafeAreaView>
  )
}

const styles = StyleSheet.create({
  h1: { fontSize: 22, fontWeight: '700', letterSpacing: -0.3, marginBottom: 12 },
  card: { padding: 14, marginBottom: 12, gap: 4 },
  title: { fontSize: 14.5, fontWeight: '600', color: colors.text },
  meta: { fontSize: 12, color: colors.textDim },
  waits: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', gap: 8, marginTop: 2 },
  wait: { fontSize: 12.5, color: colors.amberDeep },
  tag: {
    fontSize: 10.5, fontWeight: '700', color: colors.redFg, backgroundColor: colors.redBg,
    paddingHorizontal: 6, paddingVertical: 1, borderRadius: 4, overflow: 'hidden',
  },
})
