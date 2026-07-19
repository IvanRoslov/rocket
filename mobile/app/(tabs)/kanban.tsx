import { router } from 'expo-router'
import { useMemo, useState } from 'react'
import {
  Modal,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native'
import { SafeAreaView } from 'react-native-safe-area-context'
import { useCreateTask, useProjects, useSessions, useStartTask, useTasks } from '../../src/api/queries'
import type { Task, TaskStatus } from '../../src/api/types'
import { Badge, Card, ChipTabs, Dot, EmptyState, GhostButton, MonoText, PrimaryButton } from '../../src/components/ui'
import { sessionDot } from '../../src/lib/format'
import { useServers } from '../../src/servers/ServerContext'
import { colors, mono, radius } from '../../src/theme'

const COLUMNS: { key: TaskStatus; title: string; dot: string }[] = [
  { key: 'backlog', title: 'Backlog', dot: colors.textFaint },
  { key: 'in_progress', title: 'In Progress', dot: colors.accent },
  { key: 'review', title: 'Review', dot: colors.purple },
  { key: 'done', title: 'Done', dot: colors.green },
]

function TaskCard({ task, workers }: { task: Task; workers: number }) {
  const start = useStartTask()
  const { data: sessions } = useSessions(task.project_id)
  const orch = sessions?.find((s) => s.id === task.session_id)

  return (
    <Card style={{ borderRadius: radius.xl }}>
      <Pressable onPress={() => router.navigate(`/task/${task.id}`)}>
        <View style={{ flexDirection: 'row', alignItems: 'baseline', gap: 8, marginBottom: 9 }}>
          <MonoText style={{ fontSize: 12, fontWeight: '600', color: colors.textFaint }}>#{task.id}</MonoText>
          <Text style={styles.taskTitle}>{task.title}</Text>
        </View>
        {task.repo_id ? (
          <MonoText style={{ marginBottom: 10, color: colors.textDim }}>{task.repo_id}</MonoText>
        ) : null}
        {orch ? (
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: 7, marginBottom: 4 }}>
            <Dot color={sessionDot(orch.state, orch.activity)} size={7} />
            <Text style={{ fontSize: 12.5, color: colors.textMid }}>
              orch: {orch.activity ?? orch.state}
              {workers > 0 ? `  ·  ${workers} workers` : ''}
            </Text>
          </View>
        ) : null}
      </Pressable>
      {task.status === 'backlog' ? (
        <PrimaryButton
          label={start.isPending ? 'Starting…' : 'Start orchestrator ▸'}
          disabled={start.isPending}
          onPress={() => start.mutate(task.id)}
          style={{ marginTop: 8, height: 38, borderRadius: radius.md }}
        />
      ) : null}
    </Card>
  )
}

function NewTaskModal({ projectId, onClose }: { projectId: string; onClose: () => void }) {
  const create = useCreateTask()
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')

  return (
    <Modal transparent animationType="slide" onRequestClose={onClose}>
      <Pressable style={styles.backdrop} onPress={onClose}>
        <Pressable style={styles.sheet} onPress={() => {}}>
          <View style={styles.grabber} />
          <Text style={{ fontSize: 15, fontWeight: '700', marginBottom: 14 }}>New task</Text>
          <TextInput
            style={styles.input}
            placeholder="Title"
            placeholderTextColor={colors.textFaint}
            value={title}
            onChangeText={setTitle}
            autoFocus
          />
          <TextInput
            style={[styles.input, { height: 90, paddingTop: 12, textAlignVertical: 'top' }]}
            placeholder="Description (optional)"
            placeholderTextColor={colors.textFaint}
            value={description}
            onChangeText={setDescription}
            multiline
          />
          <View style={{ flexDirection: 'row', gap: 9 }}>
            <GhostButton label="Cancel" onPress={onClose} style={{ flex: 1 }} />
            <PrimaryButton
              label={create.isPending ? 'Creating…' : 'Create task'}
              disabled={!title.trim() || create.isPending}
              onPress={() =>
                create.mutate(
                  { title: title.trim(), description: description.trim() || undefined, project: projectId },
                  { onSuccess: onClose },
                )
              }
              style={{ flex: 1 }}
            />
          </View>
        </Pressable>
      </Pressable>
    </Modal>
  )
}

export default function KanbanScreen() {
  const { activeProjectId, setActiveProjectId } = useServers()
  const projects = useProjects()
  const projectId = activeProjectId ?? projects.data?.[0]?.id
  const project = projects.data?.find((p) => p.id === projectId)
  const tasksQ = useTasks(projectId)
  const { data: sessions } = useSessions(projectId)
  const [col, setCol] = useState<TaskStatus>('in_progress')
  const [search, setSearch] = useState('')
  const [creating, setCreating] = useState(false)

  const roots = useMemo(() => (tasksQ.data ?? []).filter((t) => !t.parent_id), [tasksQ.data])
  const visible = roots.filter(
    (t) => t.status === col && (!search || t.title.toLowerCase().includes(search.toLowerCase())),
  )
  const workersOf = (t: Task) =>
    (sessions ?? []).filter((s) => s.kind === 'worker' && s.state === 'running' && s.feature_slug === t.feature_slug)
      .length

  const nextProject = () => {
    const list = projects.data ?? []
    if (list.length < 2 || !projectId) return
    const i = list.findIndex((p) => p.id === projectId)
    setActiveProjectId(list[(i + 1) % list.length].id)
  }

  return (
    <SafeAreaView style={{ flex: 1, backgroundColor: colors.page }} edges={['top']}>
      <View style={styles.header}>
        <Pressable onPress={() => router.navigate('/(tabs)')}>
          <Text style={{ fontSize: 20, color: colors.textDim }}>‹</Text>
        </Pressable>
        <Pressable style={{ flexDirection: 'row', alignItems: 'center', gap: 7 }} onPress={nextProject}>
          <Dot color={(project?.live_sessions ?? 0) > 0 ? colors.green : colors.slate} size={8} />
          <Text style={{ fontSize: 17, fontWeight: '700', letterSpacing: -0.2 }}>
            {project?.name ?? 'Kanban'}
          </Text>
          {(projects.data?.length ?? 0) > 1 ? (
            <Text style={{ fontSize: 10, color: colors.textFaint }}>▾</Text>
          ) : null}
        </Pressable>
      </View>

      <View style={{ paddingHorizontal: 16, paddingTop: 12, backgroundColor: colors.card }}>
        <TextInput
          style={styles.search}
          placeholder="Search tasks…"
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
              count: roots.filter((t) => t.status === c.key).length,
            }))}
            active={col}
            onChange={(k) => setCol(k as TaskStatus)}
          />
        </View>
      </View>

      <ScrollView contentContainerStyle={{ padding: 16, gap: 10, paddingBottom: 90 }}>
        {visible.map((t) => (
          <TaskCard key={t.id} task={t} workers={workersOf(t)} />
        ))}
        {visible.length === 0 ? <EmptyState text="No tasks in this column." /> : null}
      </ScrollView>

      {projectId ? (
        <Pressable style={styles.fab} onPress={() => setCreating(true)}>
          <Text style={{ color: '#fff', fontSize: 26, lineHeight: 30 }}>＋</Text>
        </Pressable>
      ) : null}
      {creating && projectId ? <NewTaskModal projectId={projectId} onClose={() => setCreating(false)} /> : null}
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
  taskTitle: { fontSize: 15.5, fontWeight: '700', color: colors.text, flex: 1, letterSpacing: -0.15 },
  fab: {
    position: 'absolute',
    right: 18,
    bottom: 24,
    width: 52,
    height: 52,
    borderRadius: 26,
    backgroundColor: colors.ink,
    alignItems: 'center',
    justifyContent: 'center',
    shadowColor: '#000',
    shadowOpacity: 0.28,
    shadowRadius: 10,
    shadowOffset: { width: 0, height: 6 },
    elevation: 6,
  },
  backdrop: {
    flex: 1,
    backgroundColor: 'rgba(15,15,17,.4)',
    justifyContent: 'flex-end',
  },
  sheet: {
    backgroundColor: '#fbfbfa',
    borderTopLeftRadius: 18,
    borderTopRightRadius: 18,
    padding: 16,
    paddingBottom: 32,
  },
  grabber: {
    alignSelf: 'center',
    width: 38,
    height: 5,
    borderRadius: 3,
    backgroundColor: '#d4d4d1',
    marginBottom: 14,
  },
  input: {
    height: 44,
    paddingHorizontal: 12,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: radius.lg,
    fontSize: 14,
    color: colors.text,
    backgroundColor: colors.card,
    marginBottom: 11,
  },
})
