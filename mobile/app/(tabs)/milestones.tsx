// Milestones tab (task #1023, spec v2 «Дашборд и mobile»): the work the
// persistent agents hold. Same shape as the kanban tab — chips per status,
// one card per milestone — but a card carries an agent instead of an
// orchestrator, and leads into that agent's single tmux session.

import { router } from 'expo-router'
import { useMemo, useState } from 'react'
import { Pressable, RefreshControl, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native'
import { SafeAreaView } from 'react-native-safe-area-context'
import { useAgents, useAssignMilestone, useMilestones } from '../../src/api/queries'
import type { Task, TaskStatus } from '../../src/api/types'
import { ActionSheet } from '../../src/components/ActionSheet'
import { ConnectionBanner } from '../../src/components/ConnectionBanner'
import { useToast } from '../../src/components/Toast'
import { Badge, Card, ChipTabs, EmptyState, MonoText } from '../../src/components/ui'
import { milestoneBadges } from '../../src/lib/milestones'
import { colors, radius } from '../../src/theme'

const COLUMNS: { key: TaskStatus; title: string; dot: string }[] = [
  { key: 'backlog', title: 'Backlog', dot: colors.textFaint },
  { key: 'in_progress', title: 'In Progress', dot: colors.accent },
  { key: 'review', title: 'Review', dot: colors.purple },
  { key: 'done', title: 'Done', dot: colors.green },
]

function MilestoneCard({
  milestone,
  onAssign,
}: {
  milestone: Task
  onAssign: () => void
}) {
  const holder = milestone.assigned_role

  return (
    <Card style={{ borderRadius: radius.xl }}>
      <Pressable onPress={() => router.navigate(`/task/${milestone.id}`)}>
        <View style={{ flexDirection: 'row', alignItems: 'baseline', gap: 8, marginBottom: 9 }}>
          <MonoText style={{ fontSize: 12, fontWeight: '600', color: colors.textFaint }}>
            #{milestone.id}
          </MonoText>
          <Text style={styles.title}>{milestone.title}</Text>
        </View>
        <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: 6, marginBottom: 10 }}>
          {milestoneBadges(milestone).map((b) => (
            <Badge key={b.label} {...b} />
          ))}
        </View>
      </Pressable>

      <View style={{ flexDirection: 'row', gap: 8 }}>
        <Pressable style={styles.action} onPress={onAssign}>
          <Text style={styles.actionLabel}>◆ assign</Text>
        </Pressable>
        {holder ? (
          // The agent's tmux session is named after the agent, and `agent=1`
          // tells the chat screen the id addresses a standing agent.
          <Pressable style={styles.action} onPress={() => router.navigate(`/chat/${holder}?agent=1`)}>
            <Text style={styles.actionLabel}>💬 chat</Text>
          </Pressable>
        ) : null}
      </View>
    </Card>
  )
}

export default function MilestonesScreen() {
  const milestonesQ = useMilestones()
  const agentsQ = useAgents()
  const assign = useAssignMilestone()
  const toast = useToast()

  const [col, setCol] = useState<TaskStatus>('in_progress')
  const [search, setSearch] = useState('')
  const [assigning, setAssigning] = useState<Task | null>(null)

  const all = useMemo(() => milestonesQ.data ?? [], [milestonesQ.data])
  const visible = all.filter(
    (m) =>
      m.status === col &&
      (!search ||
        m.title.toLowerCase().includes(search.toLowerCase()) ||
        (m.assigned_role ?? '').toLowerCase().includes(search.toLowerCase())),
  )

  function handleAssign(milestone: Task, agentId: string | null) {
    setAssigning(null)
    assign.mutate(
      { id: milestone.id, agentId },
      { onError: (e) => toast.show((e as Error).message) },
    )
  }

  return (
    <SafeAreaView style={{ flex: 1, backgroundColor: colors.page }} edges={['top']}>
      <View style={styles.header}>
        <Text style={{ fontSize: 17, fontWeight: '700', letterSpacing: -0.2 }}>Milestones</Text>
      </View>

      <View style={{ paddingHorizontal: 16, paddingTop: 12, backgroundColor: colors.card }}>
        <TextInput
          style={styles.search}
          placeholder="Search milestones…"
          placeholderTextColor={colors.textFaint}
          value={search}
          onChangeText={setSearch}
        />
        <View style={{ paddingBottom: 12 }}>
          <ChipTabs
            chips={COLUMNS.map((c) => ({
              key: c.key,
              label: c.title,
              dot: c.dot,
              count: all.filter((m) => m.status === c.key).length,
            }))}
            active={col}
            onChange={(k) => setCol(k as TaskStatus)}
          />
        </View>
      </View>

      <ConnectionBanner />
      <ScrollView
        contentContainerStyle={{ padding: 16, gap: 10, paddingBottom: 90 }}
        refreshControl={<RefreshControl refreshing={false} onRefresh={() => milestonesQ.refetch()} />}
      >
        {visible.map((m) => (
          <MilestoneCard key={m.id} milestone={m} onAssign={() => setAssigning(m)} />
        ))}
        {visible.length === 0 ? <EmptyState text="No milestones in this column." /> : null}
      </ScrollView>

      <ActionSheet
        visible={assigning !== null}
        title={assigning ? `#${assigning.id} ${assigning.title}` : ''}
        onClose={() => setAssigning(null)}
        actions={
          assigning
            ? [
                ...(agentsQ.data ?? [])
                  .filter((a) => a.id !== assigning.assigned_role)
                  .map((a) => ({
                    label: a.id,
                    onPress: () => handleAssign(assigning, a.id),
                  })),
                ...(assigning.assigned_role
                  ? [
                      {
                        label: 'Unassign',
                        destructive: true,
                        onPress: () => handleAssign(assigning, null),
                      },
                    ]
                  : []),
              ]
            : []
        }
      />
    </SafeAreaView>
  )
}

const styles = StyleSheet.create({
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 11,
    height: 54,
    paddingHorizontal: 16,
    backgroundColor: colors.card,
    borderBottomWidth: 1,
    borderBottomColor: colors.border,
  },
  search: {
    height: 38,
    paddingHorizontal: 12,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: 10,
    fontSize: 13.5,
    backgroundColor: colors.cardAlt,
    color: colors.text,
    marginBottom: 12,
  },
  title: { fontSize: 15.5, fontWeight: '700', color: colors.text, flex: 1, letterSpacing: -0.15 },
  action: {
    height: 30,
    paddingHorizontal: 11,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: radius.md,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: colors.cardAlt,
  },
  actionLabel: { fontSize: 12, fontWeight: '600', color: colors.textMid },
})
