// Agents tab: every registered agent, whatever project it belongs to —
// including the ones registered with no project at all, which a
// project-filtered list can never show (docs/10-agents.md).
//
// The filter is local to this screen on purpose: it must be able to sit on
// «No project», which is not a value the app's active project can hold.

import * as Clipboard from 'expo-clipboard'
import { router } from 'expo-router'
import { useMemo, useState } from 'react'
import { Pressable, RefreshControl, ScrollView, StyleSheet, Text, View } from 'react-native'
import { SafeAreaView } from 'react-native-safe-area-context'
import { useAgents, useProjects } from '../../src/api/queries'
import type { Agent } from '../../src/api/types'
import { ConnectionBanner } from '../../src/components/ConnectionBanner'
import { useToast } from '../../src/components/Toast'
import { Badge, Card, ChipTabs, Dot, EmptyState, MonoText } from '../../src/components/ui'
import { agentBadges } from '../../src/lib/agents'
import { ago } from '../../src/lib/format'
import { colors, radius } from '../../src/theme'

/** Filter key for agents whose `project` is empty. Project ids are
 *  `[a-z0-9-]+`, so the sentinel cannot collide with one. */
const NO_PROJECT = ' none'

function AgentCard({ agent, projectLabel }: { agent: Agent; projectLabel: string }) {
  const toast = useToast()

  function copyAttach() {
    Clipboard.setStringAsync(`rocket agent attach ${agent.id}`)
    toast.show('Attach command copied')
  }

  return (
    <Card>
      <Pressable onPress={() => router.navigate(`/agent/${agent.id}`)}>
        <View style={styles.head}>
          <Dot
            color={agent.session_alive ? colors.green : agent.enabled ? colors.slate : colors.textFaint}
            size={9}
          />
          <MonoText style={styles.name}>{agent.id}</MonoText>
          <View style={styles.idPill}>
            <MonoText style={{ fontSize: 11, color: colors.textFaint }}>{projectLabel}</MonoText>
          </View>
        </View>
        {agent.description ? (
          <Text style={styles.description} numberOfLines={2}>
            {agent.description}
          </Text>
        ) : null}
        <View style={styles.statsRow}>
          {agentBadges(agent).map((b) => (
            <Badge key={b.label} {...b} />
          ))}
        </View>
      </Pressable>

      {/* The agent's session is named after the agent, so the chat screen takes
          the agent id as-is; `agent=1` tells it to keep the composer open even
          with the session down — the message then waits in the inbox. */}
      <View style={styles.actionsRow}>
        <Pressable
          onPress={() => router.navigate(`/chat/${agent.id}?agent=1`)}
          style={styles.action}
        >
          <Text style={styles.actionLabel}>💬 Chat</Text>
        </Pressable>
        <Pressable onPress={copyAttach} style={styles.action}>
          <Text style={styles.actionLabel}>⧉ Attach</Text>
        </Pressable>
        {!agent.session_alive ? (
          <Text style={styles.actionHint} numberOfLines={1}>
            session down — chat goes to the inbox
          </Text>
        ) : null}
      </View>

      <View style={styles.footer}>
        {agent.command ? (
          <MonoText style={{ fontSize: 11.5, flex: 1 }} numberOfLines={1}>
            $ {agent.command}
          </MonoText>
        ) : (
          <View style={{ flex: 1 }} />
        )}
        <Text style={{ fontSize: 11.5, color: colors.textFaint }}>{ago(agent.updated_at)}</Text>
      </View>
    </Card>
  )
}

export default function AgentsScreen() {
  const projects = useProjects()
  // No project argument: the list is global, the filter below is client-side.
  const agents = useAgents()
  const [filter, setFilter] = useState<string>('all')

  const projectName = useMemo(
    () => new Map((projects.data ?? []).map((p) => [p.id, p.name])),
    [projects.data],
  )

  const all = agents.data ?? []
  // Only the buckets that actually hold agents get a chip — this is a list of
  // agents, not of projects.
  const present = useMemo(() => {
    const keys = new Set(all.map((a) => a.project || NO_PROJECT))
    const projectKeys = [...keys]
      .filter((k) => k !== NO_PROJECT)
      .sort((a, b) => (projectName.get(a) ?? a).localeCompare(projectName.get(b) ?? b))
    return keys.has(NO_PROJECT) ? [...projectKeys, NO_PROJECT] : projectKeys
  }, [all, projectName])

  const chips = [
    { key: 'all', label: 'All' },
    ...present.map((key) => ({
      key,
      label: key === NO_PROJECT ? 'No project' : (projectName.get(key) ?? key),
    })),
  ]

  const shown =
    filter === 'all' ? all : all.filter((a) => (a.project || NO_PROJECT) === filter)

  return (
    <SafeAreaView style={{ flex: 1, backgroundColor: colors.page }} edges={['top']}>
      <ConnectionBanner />
      <ScrollView
        contentContainerStyle={{ padding: 16, paddingBottom: 24 }}
        refreshControl={<RefreshControl refreshing={false} onRefresh={() => agents.refetch()} />}
      >
        <Text style={styles.h1}>Agents</Text>
        <Text style={styles.lede}>
          An agent is a registration plus a tmux session named after it: write to it and the message lands in its
          session, or in its inbox when nothing is running. An agent needs no project — those sit under «No
          project».
        </Text>
        <View style={{ marginBottom: 14 }}>
          <ChipTabs chips={chips} active={filter} onChange={setFilter} />
        </View>
        {agents.isError ? (
          <Card style={{ borderColor: colors.redBorder, backgroundColor: colors.redBgSoft }}>
            <Text style={{ color: colors.redFg, fontSize: 13, fontWeight: '600' }}>Could not load agents</Text>
            <Text style={{ color: colors.redFg, fontSize: 12.5, marginTop: 4 }}>
              {(agents.error as Error).message}
            </Text>
          </Card>
        ) : (
          <View style={{ gap: 12 }}>
            {shown.map((a) => (
              <AgentCard
                key={a.id}
                agent={a}
                projectLabel={a.project ? (projectName.get(a.project) ?? a.project) : 'no project'}
              />
            ))}
            {agents.isSuccess && shown.length === 0 ? (
              <EmptyState
                text={
                  all.length === 0
                    ? 'No agents yet — register one with `rocket agent add`.'
                    : 'No agents in this filter.'
                }
              />
            ) : null}
          </View>
        )}
      </ScrollView>
    </SafeAreaView>
  )
}

const styles = StyleSheet.create({
  h1: { fontSize: 24, fontWeight: '700', color: colors.text, letterSpacing: -0.4, marginBottom: 3 },
  lede: { fontSize: 13.5, lineHeight: 20, color: colors.textDim, marginBottom: 18 },
  head: { flexDirection: 'row', alignItems: 'center', gap: 9, marginBottom: 9 },
  name: { fontSize: 16, fontWeight: '700', color: colors.text, flex: 1 },
  description: { fontSize: 13, lineHeight: 19, color: colors.textBody, marginBottom: 11 },
  idPill: { backgroundColor: colors.page, paddingHorizontal: 8, paddingVertical: 3, borderRadius: radius.sm },
  statsRow: { flexDirection: 'row', flexWrap: 'wrap', gap: 6, marginBottom: 12 },
  actionsRow: { flexDirection: 'row', alignItems: 'center', gap: 8, marginBottom: 12 },
  action: {
    paddingHorizontal: 10,
    paddingVertical: 6,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: radius.sm,
    backgroundColor: colors.page,
  },
  actionOff: { opacity: 0.45 },
  actionLabel: { fontSize: 12, fontWeight: '600', color: colors.textBody },
  actionHint: { fontSize: 11.5, color: colors.textFaint, flex: 1 },
  footer: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
    borderTopWidth: 1,
    borderTopColor: colors.borderSoft,
    paddingTop: 11,
  },
})
