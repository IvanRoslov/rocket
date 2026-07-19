import { router, useLocalSearchParams } from 'expo-router'
import { useEffect, useState } from 'react'
import { Alert, Pressable, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native'
import { SafeAreaView } from 'react-native-safe-area-context'
import { useDeleteProject, useProjects, useUpdateProject } from '../../../src/api/queries'
import { RepoPicker } from '../../../src/components/RepoPicker'
import { Card, MonoText } from '../../../src/components/ui'
import { useServers } from '../../../src/servers/ServerContext'
import { colors, radius } from '../../../src/theme'

export default function ProjectSettingsScreen() {
  const { id } = useLocalSearchParams<{ id: string }>()
  const { setActiveProjectId } = useServers()
  const projects = useProjects()
  const project = projects.data?.find((p) => p.id === id)
  const update = useUpdateProject()
  const del = useDeleteProject()
  const [name, setName] = useState('')
  const [picker, setPicker] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (project && !name) setName(project.name)
  }, [project, name])

  if (!project) {
    return (
      <SafeAreaView style={{ flex: 1, backgroundColor: colors.page }}>
        <Text style={{ padding: 40, textAlign: 'center', color: colors.textFaint }}>Loading…</Text>
      </SafeAreaView>
    )
  }

  const saveName = () => {
    if (!name.trim() || name.trim() === project.name) return
    setError(null)
    update.mutate({ id: project.id, name: name.trim() }, { onError: (e) => setError((e as Error).message) })
  }

  const setLinked = (linked: string[]) => {
    setError(null)
    update.mutate({ id: project.id, linked }, { onError: (e) => setError((e as Error).message) })
  }

  const confirmDelete = () =>
    Alert.alert('Delete project', `Delete “${project.name}”? Tasks must be done/cancelled and no sessions live.`, [
      { text: 'Cancel', style: 'cancel' },
      {
        text: 'Delete',
        style: 'destructive',
        onPress: () =>
          del.mutate(project.id, {
            onSuccess: () => {
              setActiveProjectId(null)
              router.replace('/(tabs)')
            },
            onError: (e) => setError((e as Error).message),
          }),
      },
    ])

  return (
    <SafeAreaView style={{ flex: 1, backgroundColor: colors.page }} edges={['top', 'bottom']}>
      <View style={styles.header}>
        <Pressable onPress={() => router.back()}>
          <Text style={{ fontSize: 20, color: colors.textDim }}>‹</Text>
        </Pressable>
        <Text style={{ fontSize: 17, fontWeight: '700', letterSpacing: -0.2 }}>Project · {project.name}</Text>
      </View>

      <ScrollView contentContainerStyle={{ padding: 16, gap: 14 }}>
        {error ? (
          <View style={styles.errorBox}>
            <Text style={{ fontSize: 12.5, color: colors.redFg }}>{error}</Text>
          </View>
        ) : null}

        <Card style={{ padding: 16 }}>
          <Text style={styles.label}>Name</Text>
          <TextInput
            style={styles.input}
            value={name}
            onChangeText={setName}
            onBlur={saveName}
            onSubmitEditing={saveName}
            returnKeyType="done"
          />

          <Text style={[styles.label, { marginTop: 16 }]}>Repositories</Text>
          <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: 7 }}>
            <View style={[styles.chip, { backgroundColor: colors.indigoBg }]}>
              <MonoText style={{ fontSize: 12.5, color: colors.indigoFg }}>⌂ {project.main}</MonoText>
              <Text style={{ fontSize: 10, color: '#818cf8' }}>main</Text>
            </View>
            {project.linked.map((r) => (
              <Pressable
                key={r}
                onPress={() =>
                  Alert.alert('Unlink repo', `Remove ${r} from linked repos?`, [
                    { text: 'Cancel', style: 'cancel' },
                    {
                      text: 'Unlink',
                      style: 'destructive',
                      onPress: () => setLinked(project.linked.filter((x) => x !== r)),
                    },
                  ])
                }
              >
                <View style={styles.chip}>
                  <MonoText style={{ fontSize: 12.5 }}>{r}</MonoText>
                  <Text style={{ fontSize: 11, color: colors.textFaint }}>✕</Text>
                </View>
              </Pressable>
            ))}
            <Pressable style={styles.addChip} onPress={() => setPicker(true)}>
              <Text style={{ fontSize: 12.5, color: colors.textDim }}>＋ linked</Text>
            </Pressable>
          </View>
          {update.isPending ? (
            <Text style={{ marginTop: 10, fontSize: 12, color: colors.textFaint }}>Saving…</Text>
          ) : null}
        </Card>

        <View style={styles.dangerCard}>
          <Text style={{ fontSize: 13.5, fontWeight: '600', color: '#991b1b' }}>Delete project</Text>
          <Text style={{ fontSize: 12.5, lineHeight: 18, color: colors.redFg, marginTop: 3, marginBottom: 12 }}>
            Available only when all tasks are done/cancelled and no sessions are live.
          </Text>
          <Pressable style={styles.deleteBtn} onPress={confirmDelete} disabled={del.isPending}>
            <Text style={{ fontSize: 13, fontWeight: '600', color: colors.redFg }}>
              {del.isPending ? 'Deleting…' : 'Delete'}
            </Text>
          </Pressable>
        </View>
      </ScrollView>

      <RepoPicker
        visible={picker}
        title="Add linked repo"
        exclude={[project.main, ...project.linked]}
        onClose={() => setPicker(false)}
        onPick={(rid) => setLinked([...project.linked, rid])}
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
  label: { fontSize: 12.5, fontWeight: '600', color: colors.text, marginBottom: 8 },
  input: {
    height: 42,
    paddingHorizontal: 12,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: 10,
    fontSize: 14,
    color: colors.text,
    backgroundColor: colors.card,
  },
  chip: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    backgroundColor: '#f4f4f2',
    paddingHorizontal: 10,
    paddingVertical: 6,
    borderRadius: radius.md - 1,
  },
  addChip: {
    borderWidth: 1,
    borderStyle: 'dashed',
    borderColor: '#d4d4d1',
    paddingHorizontal: 11,
    paddingVertical: 6,
    borderRadius: radius.md - 1,
  },
  deleteBtn: {
    height: 40,
    backgroundColor: '#fff',
    borderWidth: 1,
    borderColor: '#fca5a5',
    borderRadius: radius.md,
    alignItems: 'center',
    justifyContent: 'center',
  },
  dangerCard: {
    backgroundColor: colors.redBgSoft,
    borderWidth: 1,
    borderColor: colors.redBorder,
    borderRadius: radius.xl,
    padding: 15,
  },
  errorBox: {
    backgroundColor: colors.redBgSoft,
    borderWidth: 1,
    borderColor: colors.redBorder,
    borderRadius: radius.md,
    padding: 10,
  },
})
