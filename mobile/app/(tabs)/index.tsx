import { router } from 'expo-router'
import { Pressable, RefreshControl, ScrollView, StyleSheet, Text, View } from 'react-native'
import { SafeAreaView } from 'react-native-safe-area-context'
import { useProjects, useTasks } from '../../src/api/queries'
import type { Project, Task } from '../../src/api/types'
import { ConnectionBanner } from '../../src/components/ConnectionBanner'
import { Badge, Card, Dot, EmptyState, MonoText } from '../../src/components/ui'
import { ago } from '../../src/lib/format'
import { useServers } from '../../src/servers/ServerContext'
import { colors, mono, radius } from '../../src/theme'

function projectStats(tasks: Task[]) {
  const brainstorm = tasks.filter((t) => t.status === 'brainstorm').length
  const inProgress = tasks.filter((t) => t.status === 'in_progress').length
  const review = tasks.filter((t) => t.status === 'review').length
  const updated = Math.max(0, ...tasks.map((t) => t.updated_at))
  return { brainstorm, inProgress, review, updated }
}

function ProjectCard({ project, tasks }: { project: Project; tasks: Task[] }) {
  const { setActiveProjectId } = useServers()
  const { brainstorm, inProgress, review, updated } = projectStats(tasks)
  // A brainstormed task (#1077) is work in flight, so it keeps the project off "idle".
  const idle = brainstorm + inProgress + review + project.live_sessions === 0

  return (
    <Pressable
      onPress={() => {
        setActiveProjectId(project.id)
        router.navigate('/(tabs)/kanban')
      }}
    >
      <Card>
        <View style={styles.head}>
          <Dot color={project.live_sessions > 0 ? colors.green : colors.slate} size={9} />
          <Text style={styles.name}>{project.name}</Text>
          <View style={styles.idPill}>
            <MonoText style={{ fontSize: 11, color: colors.textFaint }}>{project.id}</MonoText>
          </View>
        </View>
        <View style={styles.repoRow}>
          <MonoText style={{ fontSize: 12.5 }} numberOfLines={1}>
            ⌂ {project.main}
            {project.linked.length > 0 ? (
              <MonoText style={{ fontSize: 12.5, color: colors.textDim }}>
                {'  +  '}
                {project.linked.length <= 2
                  ? project.linked.join(', ')
                  : `${project.linked.slice(0, 2).join(', ')} +${project.linked.length - 2} more`}
              </MonoText>
            ) : null}
          </MonoText>
        </View>
        <View style={styles.statsRow}>
          {brainstorm > 0 ? (
            <Badge label={`${brainstorm} brainstorm`} fg={colors.slateFg} bg={colors.slateBg} />
          ) : null}
          {inProgress > 0 ? (
            <Badge label={`${inProgress} in progress`} fg={colors.indigoFg} bg={colors.indigoBg} />
          ) : null}
          {review > 0 ? <Badge label={`${review} review`} fg={colors.purpleFg} bg={colors.purpleBg} /> : null}
          {project.live_sessions > 0 ? (
            <Badge label={`● ${project.live_sessions} live`} fg={colors.greenFg} bg={colors.greenBg} />
          ) : null}
          {idle ? <Badge label="idle" fg={colors.slateFg} bg={colors.slateBg} /> : null}
        </View>
        <View style={styles.footer}>
          <View style={{ flex: 1 }} />
          <Text style={{ fontSize: 11.5, color: colors.textFaint }}>{updated ? ago(updated) : ''}</Text>
        </View>
      </Card>
    </Pressable>
  )
}

export default function ProjectsScreen() {
  const { active } = useServers()
  const projects = useProjects()
  const tasks = useTasks()

  return (
    <SafeAreaView style={{ flex: 1, backgroundColor: colors.page }} edges={['top']}>
      <View style={styles.header}>
        <View style={styles.logo}>
          <Text style={{ color: '#fff', fontFamily: mono, fontSize: 13, fontWeight: '700' }}>R</Text>
        </View>
        <Pressable style={styles.serverChip} onPress={() => router.navigate('/servers')}>
          <Dot color={projects.isError ? colors.red : colors.green} size={7} />
          <Text style={{ fontSize: 13, fontWeight: '600', color: colors.text }}>{active?.name ?? 'server'}</Text>
          <Text style={{ fontSize: 10, color: colors.textFaint }}>▾</Text>
        </Pressable>
        <View style={{ flex: 1 }} />
        <Pressable style={styles.addBtn} onPress={() => router.navigate('/project/new')}>
          <Text style={{ color: '#fff', fontSize: 19, lineHeight: 22 }}>＋</Text>
        </Pressable>
      </View>
      <ConnectionBanner />
      <ScrollView
        contentContainerStyle={{ padding: 16, paddingBottom: 24 }}
        refreshControl={
          <RefreshControl
            refreshing={false}
            onRefresh={() => {
              projects.refetch()
              tasks.refetch()
            }}
          />
        }
      >
        <Text style={styles.h1}>Projects</Text>
        <Text style={styles.lede}>Each project is a product — a main repo plus linked repos where workers run.</Text>
        {projects.isError ? (
          <Card style={{ borderColor: colors.redBorder, backgroundColor: colors.redBgSoft }}>
            <Text style={{ color: colors.redFg, fontSize: 13, fontWeight: '600', marginBottom: 4 }}>
              Server unreachable
            </Text>
            <Text style={{ color: colors.redFg, fontSize: 12.5, marginBottom: 10 }}>
              {active ? `${active.host}:${active.port} is not answering.` : 'No server selected.'}
            </Text>
            <Pressable onPress={() => router.navigate('/servers')}>
              <Text style={{ color: colors.accent, fontSize: 13, fontWeight: '600' }}>Switch server →</Text>
            </Pressable>
          </Card>
        ) : (
          <View style={{ gap: 12 }}>
            {(projects.data ?? []).map((p) => (
              <ProjectCard
                key={p.id}
                project={p}
                tasks={(tasks.data ?? []).filter((t) => t.project_id === p.id && !t.parent_id)}
              />
            ))}
            <Pressable style={styles.createCard} onPress={() => router.navigate('/project/new')}>
              <Text style={{ fontSize: 20, color: colors.textDim }}>＋</Text>
              <Text style={{ fontSize: 13.5, fontWeight: '600', color: colors.textDim }}>Create project</Text>
            </Pressable>
          </View>
        )}
      </ScrollView>
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
  logo: {
    width: 26,
    height: 26,
    borderRadius: 7,
    backgroundColor: colors.ink,
    alignItems: 'center',
    justifyContent: 'center',
  },
  addBtn: {
    width: 36,
    height: 36,
    borderRadius: 10,
    backgroundColor: colors.ink,
    alignItems: 'center',
    justifyContent: 'center',
  },
  createCard: {
    height: 56,
    borderWidth: 1.5,
    borderStyle: 'dashed',
    borderColor: '#d4d4d1',
    borderRadius: radius.xxl,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 8,
  },
  serverChip: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 7,
    height: 34,
    paddingHorizontal: 11,
    backgroundColor: colors.page,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: radius.md,
  },
  h1: { fontSize: 24, fontWeight: '700', color: colors.text, letterSpacing: -0.4, marginBottom: 3 },
  lede: { fontSize: 13.5, lineHeight: 20, color: colors.textDim, marginBottom: 18 },
  head: { flexDirection: 'row', alignItems: 'center', gap: 9, marginBottom: 11 },
  name: { fontSize: 16.5, fontWeight: '700', color: colors.text, flex: 1, letterSpacing: -0.2 },
  idPill: {
    backgroundColor: colors.page,
    paddingHorizontal: 8,
    paddingVertical: 3,
    borderRadius: radius.sm,
  },
  repoRow: { flexDirection: 'row', alignItems: 'center', marginBottom: 12, flexWrap: 'wrap' },
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
