import { Alert, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native'
import { SafeAreaView } from 'react-native-safe-area-context'
import { useSessions, useSystem, useSystemCleanup } from '../../src/api/queries'
import { Badge, Card, Dot, EmptyState, MonoText, SectionTitle } from '../../src/components/ui'
import { bytes, sessionBadge, sessionDot, uptime } from '../../src/lib/format'
import { useServers } from '../../src/servers/ServerContext'
import { colors, mono, radius } from '../../src/theme'

export default function SystemScreen() {
  const { active } = useServers()
  const system = useSystem()
  const { data: sessions } = useSessions()
  const cleanup = useSystemCleanup()
  const s = system.data

  const confirmCleanup = () =>
    Alert.alert('Cleanup', 'Kill orphaned tmux sessions and remove orphaned worktrees?', [
      { text: 'Cancel', style: 'cancel' },
      {
        text: 'Cleanup',
        style: 'destructive',
        onPress: () =>
          cleanup.mutate(undefined, {
            onSuccess: (r) =>
              Alert.alert(
                'Cleanup done',
                `Killed tmux: ${r.killed_tmux.length ? r.killed_tmux.join(', ') : 'none'}\nRemoved worktrees: ${
                  r.removed_worktrees.length ? r.removed_worktrees.join('\n') : 'none'
                }`,
              ),
            onError: (e) => Alert.alert('Cleanup failed', (e as Error).message),
          }),
      },
    ])

  const live = (sessions ?? []).filter((x) => x.state === 'running' || x.state === 'spawning')
  const orphans = (s?.tmux ?? []).filter((t) => t.orphan).length + (s?.worktrees ?? []).filter((w) => w.orphan).length
  const wtTotal = (s?.worktrees ?? []).reduce((acc, w) => acc + w.size_bytes, 0)

  const stats = [
    { label: 'Live sessions', value: String(live.length), unit: 'tmux', fg: colors.greenFg },
    { label: 'Agents running', value: String(live.length), unit: 'claude/codex', fg: colors.text },
    { label: 'Orphans', value: String(orphans), unit: 'reconcile', fg: orphans > 0 ? colors.amberFg : colors.text },
    {
      label: 'Queue depth',
      value: String(s?.queue.queued ?? 0),
      unit: s?.queue.failed ? `+${s.queue.failed} failed` : 'messages',
      fg: colors.text,
    },
  ]

  return (
    <SafeAreaView style={{ flex: 1, backgroundColor: colors.page }} edges={['top']}>
      <View style={styles.header}>
        <View style={styles.logo}>
          <Text style={{ color: '#fff', fontFamily: mono, fontSize: 13, fontWeight: '700' }}>R</Text>
        </View>
        <Text style={{ fontSize: 17, fontWeight: '700', letterSpacing: -0.2 }}>System</Text>
        <View style={{ flex: 1 }} />
        <Pressable style={styles.cleanupBtn} onPress={confirmCleanup} disabled={cleanup.isPending}>
          <Text style={{ fontSize: 12.5, fontWeight: '600', color: colors.redFg }}>
            {cleanup.isPending ? 'Cleaning…' : '⌦ Cleanup'}
          </Text>
        </Pressable>
      </View>
      {system.isError ? (
        <EmptyState text="Server unreachable." />
      ) : (
        <ScrollView contentContainerStyle={{ padding: 16, paddingBottom: 24 }}>
          <View style={styles.grid}>
            {stats.map((c) => (
              <View key={c.label} style={styles.statCard}>
                <Text style={{ fontSize: 12, color: colors.textDim, marginBottom: 7 }}>{c.label}</Text>
                <View style={{ flexDirection: 'row', alignItems: 'baseline', gap: 6 }}>
                  <Text style={{ fontFamily: mono, fontSize: 23, fontWeight: '700', color: c.fg }}>{c.value}</Text>
                  <Text style={{ fontSize: 11.5, color: colors.textFaint }}>{c.unit}</Text>
                </View>
              </View>
            ))}
          </View>

          <SectionTitle right={<Text style={{ fontSize: 11.5, color: colors.textFaint }}>daemon-reconciled</Text>}>
            Sessions & agents
          </SectionTitle>
          <View style={{ gap: 8, marginBottom: 20 }}>
            {(sessions ?? []).map((x) => (
              <View key={x.id} style={styles.sessionRow}>
                <Dot color={sessionDot(x.state, x.activity)} size={8} />
                <View style={{ flex: 1, minWidth: 0 }}>
                  <MonoText style={{ fontSize: 13, fontWeight: '600', color: colors.text }} >
                    {x.tmux_name}
                  </MonoText>
                  <Text style={{ fontSize: 11.5, color: colors.textDim, marginTop: 2 }}>
                    {x.kind} · {x.agent}
                  </Text>
                </View>
                <Badge {...sessionBadge(x.state, x.activity)} />
              </View>
            ))}
            {(sessions ?? []).length === 0 ? <EmptyState text="No sessions." /> : null}
          </View>

          <SectionTitle>Message queue</SectionTitle>
          <View style={{ flexDirection: 'row', gap: 10, marginBottom: 20 }}>
            <View style={[styles.queueCard, { backgroundColor: colors.greenBgSoft, borderColor: colors.greenBorder }]}>
              <Text style={{ fontFamily: mono, fontSize: 22, fontWeight: '700', color: colors.greenFg }}>
                {s?.queue.queued ?? 0}
              </Text>
              <Text style={{ fontSize: 12, color: colors.textMid }}>queued</Text>
            </View>
            <View style={[styles.queueCard, { backgroundColor: colors.redBgSoft, borderColor: colors.redBorder }]}>
              <Text style={{ fontFamily: mono, fontSize: 22, fontWeight: '700', color: colors.redFg }}>
                {s?.queue.failed ?? 0}
              </Text>
              <Text style={{ fontSize: 12, color: colors.textMid }}>failed</Text>
            </View>
          </View>

          <SectionTitle
            right={<MonoText style={{ fontSize: 12, color: colors.textFaint }}>{bytes(wtTotal)}</MonoText>}
          >
            Worktrees on disk
          </SectionTitle>
          <Card style={{ padding: 0, overflow: 'hidden', marginBottom: 20 }}>
            {(s?.worktrees ?? []).map((w) => (
              <View key={w.path} style={styles.wtRow}>
                <MonoText style={{ fontSize: 11.5, flex: 1 }} >
                  {w.path.replace(/^\/Users\/[^/]+/, '~')}
                  {w.orphan ? '  ⚠' : ''}
                </MonoText>
                <MonoText style={{ fontSize: 11.5, color: colors.textFaint }}>{bytes(w.size_bytes)}</MonoText>
              </View>
            ))}
            {(s?.worktrees ?? []).length === 0 ? (
              <Text style={{ padding: 13, fontSize: 12.5, color: colors.textFaint }}>No worktrees.</Text>
            ) : null}
          </Card>

          <SectionTitle>Daemon</SectionTitle>
          <Card style={{ gap: 9, marginBottom: 20 }}>
            <Row k="status" v="● running" vColor={colors.greenFg} />
            <Row k="version" v={`rocketd ${s?.daemon.version ?? '…'}`} />
            <Row k="address" v={active ? `${active.host}:${active.port}` : ''} />
            <Row k="uptime" v={s ? uptime(s.daemon.uptime_s) : '…'} />
          </Card>

          <View style={styles.logBox}>
            <View style={styles.logHead}>
              <MonoText style={{ fontSize: 12.5, fontWeight: '600', color: '#e4e4e7' }}>rocketd.log</MonoText>
              <MonoText style={{ fontSize: 11, color: colors.textDim }}>tail</MonoText>
            </View>
            <ScrollView horizontal showsHorizontalScrollIndicator={false}>
              <Text style={styles.logText}>{(s?.log_tail ?? []).slice(-40).join('\n') || '—'}</Text>
            </ScrollView>
          </View>
        </ScrollView>
      )}
    </SafeAreaView>
  )
}

function Row({ k, v, vColor }: { k: string; v: string; vColor?: string }) {
  return (
    <View style={{ flexDirection: 'row', justifyContent: 'space-between', gap: 12 }}>
      <Text style={{ fontSize: 13, color: colors.textDim }}>{k}</Text>
      <MonoText style={{ fontSize: 12.5, fontWeight: '600', color: vColor ?? colors.text }}>{v}</MonoText>
    </View>
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
  logo: {
    width: 26,
    height: 26,
    borderRadius: 7,
    backgroundColor: colors.ink,
    alignItems: 'center',
    justifyContent: 'center',
  },
  cleanupBtn: {
    height: 34,
    paddingHorizontal: 12,
    backgroundColor: colors.card,
    borderWidth: 1,
    borderColor: '#d4d4d1',
    borderRadius: radius.md,
    alignItems: 'center',
    justifyContent: 'center',
  },
  grid: { flexDirection: 'row', flexWrap: 'wrap', gap: 10, marginBottom: 16 },
  statCard: {
    flexBasis: '47%',
    flexGrow: 1,
    backgroundColor: colors.card,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: radius.md + 3,
    padding: 14,
  },
  sessionRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 11,
    backgroundColor: colors.card,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: radius.md + 3,
    padding: 12,
    paddingHorizontal: 13,
  },
  queueCard: { flex: 1, borderWidth: 1, borderRadius: radius.md + 3, padding: 13 },
  wtRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
    padding: 11,
    paddingHorizontal: 13,
    borderBottomWidth: 1,
    borderBottomColor: colors.hairline,
  },
  logBox: { backgroundColor: colors.termBg, borderRadius: radius.md + 3, overflow: 'hidden' },
  logHead: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 9,
    justifyContent: 'space-between',
    padding: 11,
    paddingHorizontal: 14,
    borderBottomWidth: 1,
    borderBottomColor: '#000',
  },
  logText: { fontFamily: mono, fontSize: 11, lineHeight: 19, color: colors.termLog, padding: 12, paddingHorizontal: 14 },
})
