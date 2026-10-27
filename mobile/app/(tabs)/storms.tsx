import { useMemo, useState } from 'react'
import { Pressable, RefreshControl, ScrollView, StyleSheet, Text, View } from 'react-native'
import { router } from 'expo-router'
import { SafeAreaView } from 'react-native-safe-area-context'
import { useBrainstorms } from '../../src/api/queries'
import type { BrainstormEntry } from '../../src/api/types'
import { ConnectionBanner } from '../../src/components/ConnectionBanner'
import { Card, ChipTabs, Dot } from '../../src/components/ui'
import {
  brainstormCounts,
  brainstormHref,
  filterBrainstorms,
  isMidWorkSpecChange,
  type BrainstormFilter,
} from '../../src/lib/brainstorms'
import { ago } from '../../src/lib/format'
import { colors, radius } from '../../src/theme'

const STATUS: Record<string, { label: string; dot: string }> = {
  backlog: { label: 'Backlog', dot: colors.textFaint },
  brainstorm: { label: 'Brainstorm', dot: colors.amber },
  in_progress: { label: 'In Progress', dot: colors.accent },
  blocked: { label: 'Blocked', dot: colors.red },
}

function statusOf(s: string): { label: string; dot: string } {
  return STATUS[s] ?? { label: s.replaceAll('_', ' '), dot: colors.textFaint }
}

type Tone = 'go' | 'midwork' | 'answer' | 'stale'

const TONES: Record<Tone, { fg: string; bg: string; border: string }> = {
  go: { fg: colors.amberDeep, bg: colors.amberBg, border: colors.amberBorder },
  midwork: { fg: colors.amberFg, bg: colors.card, border: colors.amberBorder },
  answer: { fg: colors.indigoFg, bg: colors.indigoBg, border: colors.indigoBorder },
  stale: { fg: colors.redFg, bg: colors.redBgSoft, border: colors.redBorder },
}

function Pill({ label, tone }: { label: string; tone: Tone }) {
  const t = TONES[tone]
  return (
    <View style={[styles.pill, { backgroundColor: t.bg, borderColor: t.border }]}>
      <Text style={[styles.pillText, { color: t.fg }]}>{label}</Text>
    </View>
  )
}

function StormCard({ e }: { e: BrainstormEntry }) {
  const status = statusOf(e.task_status)
  return (
    <Pressable onPress={() => router.navigate(brainstormHref(e) as never)}>
      {({ pressed }) => (
        <Card style={pressed ? { ...styles.card, backgroundColor: colors.cardAlt } : styles.card}>
          <View style={styles.topRow}>
            <View style={styles.pills}>
              {e.pending_gate ? <Pill tone="go" label={`Go · spec v${e.pending_gate.spec_version}`} /> : null}
              {isMidWorkSpecChange(e) ? <Pill tone="midwork" label="mid-work" /> : null}
              {e.questions_waiting > 0 ? (
                <Pill
                  tone="answer"
                  label={e.questions_waiting === 1 ? '1 question' : `${e.questions_waiting} questions`}
                />
              ) : null}
              {e.questions_waiting > 0 && e.questions_stale ? <Pill tone="stale" label="STALE" /> : null}
            </View>
            <Text style={styles.ago}>{ago(e.waiting_since)}</Text>
          </View>
          <Text style={styles.title} numberOfLines={3}>
            <Text style={styles.id}>{`#${e.task_id} `}</Text>
            {e.task_title}
          </Text>
          <View style={styles.metaRow}>
            <Dot color={status.dot} size={7} />
            <Text style={styles.meta}>{status.label}</Text>
            <Text style={styles.metaSep}>·</Text>
            <Text style={styles.meta}>{e.project_id ?? 'milestone'}</Text>
          </View>
        </Card>
      )}
    </Pressable>
  )
}

function Empty({ title, text }: { title: string; text?: string }) {
  return (
    <View style={styles.empty}>
      <View style={styles.emptyMark}>
        <Text style={{ color: colors.greenFg, fontSize: 18, fontWeight: '600' }}>✓</Text>
      </View>
      <Text style={styles.emptyTitle}>{title}</Text>
      {text ? <Text style={styles.emptyText}>{text}</Text> : null}
    </View>
  )
}

export default function StormsScreen() {
  const storms = useBrainstorms()
  const [filter, setFilter] = useState<BrainstormFilter>('all')
  const data = storms.data
  const counts = useMemo(() => brainstormCounts(data ?? []), [data])
  const rows = useMemo(() => filterBrainstorms(data ?? [], filter), [data, filter])
  const hasAny = (data ?? []).length > 0

  return (
    <SafeAreaView style={{ flex: 1, backgroundColor: colors.page }} edges={['top']}>
      <ConnectionBanner />
      <ScrollView
        contentContainerStyle={{ padding: 14, paddingBottom: 90 }}
        refreshControl={<RefreshControl refreshing={storms.isRefetching} onRefresh={() => storms.refetch()} />}
      >
        <Text style={styles.h1}>Brainstorms</Text>
        <Text style={styles.lede}>Specs waiting for your Go and storm questions waiting for your answer.</Text>
        {hasAny ? (
          <>
            <ChipTabs
              chips={[
                { key: 'all', label: 'All', count: counts.all },
                { key: 'answer', label: 'Needs answer', count: counts.answer },
                { key: 'go', label: 'Needs Go', count: counts.go },
              ]}
              active={filter}
              onChange={(k) => setFilter(k as BrainstormFilter)}
            />
            <View style={{ height: 12 }} />
          </>
        ) : null}
        {hasAny && rows.length === 0 ? (
          <Text style={styles.none}>{filter === 'go' ? 'Nothing needs a Go' : 'Nothing needs an answer'}</Text>
        ) : null}
        {rows.map((e) => (
          <StormCard key={e.task_id} e={e} />
        ))}
        {storms.isSuccess && !hasAny ? (
          <Empty title="No brainstorms are waiting on you" text="New storm questions and Go requests show up here." />
        ) : null}
      </ScrollView>
    </SafeAreaView>
  )
}

const styles = StyleSheet.create({
  h1: { fontSize: 22, fontWeight: '700', letterSpacing: -0.3 },
  lede: { fontSize: 13, color: colors.textDim, marginTop: 4, marginBottom: 12, lineHeight: 18 },
  card: { padding: 14, marginBottom: 10, gap: 8 },
  topRow: { flexDirection: 'row', alignItems: 'flex-start', gap: 8 },
  pills: { flex: 1, flexDirection: 'row', flexWrap: 'wrap', gap: 5 },
  pill: { paddingHorizontal: 8, paddingVertical: 2, borderRadius: radius.chip, borderWidth: 1 },
  pillText: { fontSize: 11.5, fontWeight: '600' },
  ago: { fontSize: 12, color: colors.textFaint, marginTop: 2 },
  title: { fontSize: 14.5, fontWeight: '600', color: colors.text, lineHeight: 20 },
  id: { color: colors.textFaint, fontWeight: '500' },
  metaRow: { flexDirection: 'row', alignItems: 'center', gap: 6 },
  meta: { fontSize: 12.5, color: colors.textDim },
  metaSep: { fontSize: 12.5, color: colors.textFaint },
  none: { fontSize: 13.5, color: colors.textFaint, paddingVertical: 24, textAlign: 'center' },
  empty: {
    alignItems: 'center',
    gap: 6,
    paddingVertical: 48,
    paddingHorizontal: 16,
    marginTop: 4,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: radius.xl,
    backgroundColor: colors.card,
  },
  emptyMark: {
    width: 44,
    height: 44,
    marginBottom: 6,
    borderRadius: 12,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: colors.greenBgSoft,
  },
  emptyTitle: { fontSize: 15.5, fontWeight: '700', color: colors.text, textAlign: 'center' },
  emptyText: { fontSize: 13, color: colors.textDim, textAlign: 'center' },
})
