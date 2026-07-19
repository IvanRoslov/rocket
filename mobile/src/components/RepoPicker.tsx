import { useState } from 'react'
import { Modal, Pressable, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native'
import { useGithubRepos, useRegisterRepo, useRepos } from '../api/queries'
import { colors, mono, radius } from '../theme'
import { GhostButton, MonoText, PrimaryButton } from './ui'

type Source = 'registered' | 'github' | 'path'

/**
 * Bottom-sheet repo picker with three sources: already-registered repos,
 * GitHub search (registers the repo on pick — daemon clones it), and a
 * manual local checkout path. Calls `onPick(repoId)` once the repo exists
 * in the daemon registry.
 */
export function RepoPicker({
  visible,
  title,
  exclude = [],
  onPick,
  onClose,
}: {
  visible: boolean
  title: string
  exclude?: string[]
  onPick: (repoId: string) => void
  onClose: () => void
}) {
  const [source, setSource] = useState<Source>('registered')
  const [query, setQuery] = useState('')
  const [path, setPath] = useState('')
  const [error, setError] = useState<string | null>(null)
  const repos = useRepos()
  const github = useGithubRepos(query, visible && source === 'github')
  const register = useRegisterRepo()

  if (!visible) return null

  const registered = (repos.data ?? []).filter((r) => !exclude.includes(r.id))

  const pickRegistered = (id: string) => {
    onClose()
    onPick(id)
  }

  const registerAndPick = (payload: { github?: string; path?: string }) => {
    setError(null)
    register.mutate(payload, {
      onSuccess: (repo) => {
        onClose()
        onPick(repo.id)
      },
      onError: (e) => setError((e as Error).message),
    })
  }

  return (
    <Modal transparent animationType="slide" onRequestClose={onClose}>
      <Pressable style={styles.backdrop} onPress={onClose}>
        <Pressable style={styles.sheet} onPress={() => {}}>
          <View style={styles.grabber} />
          <Text style={{ fontSize: 15, fontWeight: '700', marginBottom: 12 }}>{title}</Text>

          <View style={{ flexDirection: 'row', gap: 6, marginBottom: 14 }}>
            {(
              [
                { key: 'registered', label: 'Registered' },
                { key: 'github', label: 'GitHub' },
                { key: 'path', label: 'Local path' },
              ] as { key: Source; label: string }[]
            ).map((s) => {
              const on = source === s.key
              return (
                <Pressable
                  key={s.key}
                  onPress={() => setSource(s.key)}
                  style={[styles.srcChip, on && { backgroundColor: colors.ink, borderColor: colors.ink }]}
                >
                  <Text style={{ fontSize: 12.5, fontWeight: '600', color: on ? '#fff' : colors.textMid }}>
                    {s.label}
                  </Text>
                </Pressable>
              )
            })}
          </View>

          {error ? (
            <View style={styles.errorBox}>
              <Text style={{ fontSize: 12.5, color: colors.redFg }}>{error}</Text>
            </View>
          ) : null}
          {register.isPending ? (
            <View style={styles.cloneBox}>
              <Text style={{ fontSize: 12.5, color: colors.amberFg }}>Registering repo… (clone may take a while)</Text>
            </View>
          ) : null}

          {source === 'registered' ? (
            <ScrollView style={{ maxHeight: 340 }} contentContainerStyle={{ gap: 8 }}>
              {registered.map((r) => (
                <Pressable key={r.id} style={styles.repoRow} onPress={() => pickRegistered(r.id)}>
                  <MonoText style={{ fontSize: 13.5, fontWeight: '600', color: colors.text }}>{r.id}</MonoText>
                  <MonoText style={{ fontSize: 11, color: colors.textFaint }} >
                    {r.path}
                  </MonoText>
                </Pressable>
              ))}
              {registered.length === 0 ? (
                <Text style={{ padding: 20, textAlign: 'center', color: colors.textFaint, fontSize: 13 }}>
                  No registered repos — use GitHub or a local path.
                </Text>
              ) : null}
            </ScrollView>
          ) : null}

          {source === 'github' ? (
            <View>
              <TextInput
                style={styles.input}
                placeholder="Search your repositories…"
                placeholderTextColor={colors.textFaint}
                autoCapitalize="none"
                autoCorrect={false}
                value={query}
                onChangeText={setQuery}
              />
              <ScrollView style={{ maxHeight: 300 }} contentContainerStyle={{ gap: 8 }}>
                {(github.data ?? []).slice(0, 20).map((r) => (
                  <Pressable
                    key={r.full_name}
                    style={styles.repoRow}
                    disabled={register.isPending}
                    onPress={() => registerAndPick({ github: r.full_name })}
                  >
                    <View style={{ flexDirection: 'row', alignItems: 'center', gap: 7 }}>
                      <MonoText style={{ fontSize: 13.5, fontWeight: '600', color: colors.text, flex: 1 }}>
                        {r.full_name}
                      </MonoText>
                      {r.private ? (
                        <Text style={{ fontSize: 10.5, color: colors.textFaint }}>private</Text>
                      ) : null}
                    </View>
                  </Pressable>
                ))}
                {github.isError ? (
                  <Text style={{ padding: 14, color: colors.redFg, fontSize: 12.5 }}>
                    {(github.error as Error).message} — set a GitHub token in Settings.
                  </Text>
                ) : null}
              </ScrollView>
            </View>
          ) : null}

          {source === 'path' ? (
            <View>
              <TextInput
                style={[styles.input, { fontFamily: mono }]}
                placeholder="/Users/you/projects/repo"
                placeholderTextColor={colors.textFaint}
                autoCapitalize="none"
                autoCorrect={false}
                value={path}
                onChangeText={setPath}
              />
              <PrimaryButton
                label={register.isPending ? 'Registering…' : 'Register path'}
                disabled={!path.trim() || register.isPending}
                onPress={() => registerAndPick({ path: path.trim() })}
              />
              <Text style={{ marginTop: 8, fontSize: 11.5, color: colors.textFaint }}>
                Path on the daemon machine, not on this phone.
              </Text>
            </View>
          ) : null}

          <GhostButton label="Cancel" onPress={onClose} style={{ marginTop: 14 }} />
        </Pressable>
      </Pressable>
    </Modal>
  )
}

const styles = StyleSheet.create({
  backdrop: { flex: 1, backgroundColor: 'rgba(15,15,17,.4)', justifyContent: 'flex-end' },
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
  srcChip: {
    height: 32,
    paddingHorizontal: 13,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: radius.chip,
    backgroundColor: colors.card,
    alignItems: 'center',
    justifyContent: 'center',
  },
  repoRow: {
    backgroundColor: colors.card,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: radius.lg,
    padding: 12,
    gap: 3,
  },
  input: {
    height: 42,
    paddingHorizontal: 12,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: 10,
    fontSize: 14,
    color: colors.text,
    backgroundColor: colors.card,
    marginBottom: 10,
  },
  errorBox: {
    backgroundColor: colors.redBgSoft,
    borderWidth: 1,
    borderColor: colors.redBorder,
    borderRadius: radius.md,
    padding: 10,
    marginBottom: 10,
  },
  cloneBox: {
    backgroundColor: colors.amberBgSoft,
    borderWidth: 1,
    borderColor: colors.amberBorder,
    borderRadius: radius.md,
    padding: 10,
    marginBottom: 10,
  },
})
