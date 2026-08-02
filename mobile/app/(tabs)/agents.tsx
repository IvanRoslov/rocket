import { router } from 'expo-router'
import { Pressable, RefreshControl, ScrollView, StyleSheet, Text, View } from 'react-native'
import { SafeAreaView } from 'react-native-safe-area-context'
import { useAgents, useProjects } from '../../src/api/queries'
import type { Agent } from '../../src/api/types'
import { ConnectionBanner } from '../../src/components/ConnectionBanner'
import { Badge, Card, ChipTabs, Dot, EmptyState, MonoText } from '../../src/components/ui'
import { agentBadges } from '../../src/lib/agents'
import { ago } from '../../src/lib/format'
import { useServers } from '../../src/servers/ServerContext'
import { colors, radius } from '../../src/theme'

function AgentCard({ agent }: { agent: Agent }) {
  return (
    <Pressable onPress={() => router.navigate(`/agent/${agent.id}`)}>
      <Card>
        <View style={styles.head}>
          <Dot
            color={agent.session_alive ? colors.green : agent.enabled ? colors.slate : colors.textFaint}
            size={9}
          />
          <MonoText style={styles.name}>{agent.id}</MonoText>
          {agent.project ? (
            <View style={styles.idPill}>
              <MonoText style={{ fontSize: 11, color: colors.textFaint }}>{agent.project}</MonoText>
            </View>
          ) : null}
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
    </Pressable>
  )
}

export default function AgentsScreen() {
  const { activeProjectId, setActiveProjectId } = useServers()
  const projects = useProjects()
  const agents = useAgents(activeProjectId ?? undefined)

  const chips = [
    { key: 'all', label: 'All projects' },
    ...(projects.data ?? []).map((p) => ({ key: p.id, label: p.name })),
  ]

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
          session, or in its inbox when nothing is running.
        </Text>
        <View style={{ marginBottom: 14 }}>
          <ChipTabs
            chips={chips}
            active={activeProjectId ?? 'all'}
            onChange={(k) => setActiveProjectId(k === 'all' ? null : k)}
          />
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
            {(agents.data ?? []).map((a) => (
              <AgentCard key={a.id} agent={a} />
            ))}
            {agents.isSuccess && agents.data.length === 0 ? (
              <EmptyState text="No agents yet — register one with `rocket agent add`." />
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
  footer: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
    borderTopWidth: 1,
    borderTopColor: colors.borderSoft,
    paddingTop: 11,
  },
})
