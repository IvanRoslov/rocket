import { router } from 'expo-router'
import { useState } from 'react'
import { Pressable, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native'
import { SafeAreaView } from 'react-native-safe-area-context'
import { useRepos, useSaveSettings, useSettings } from '../../src/api/queries'
import { Card, ChipTabs, Dot, EmptyState, MonoText, PrimaryButton } from '../../src/components/ui'
import { useServers } from '../../src/servers/ServerContext'
import { colors, mono, radius } from '../../src/theme'

function GithubSection() {
  const settings = useSettings()
  const save = useSaveSettings()
  const [token, setToken] = useState('')
  const [login, setLogin] = useState<string | null>(null)

  return (
    <View>
      <Text style={styles.h2}>GitHub</Text>
      <Text style={styles.lede}>Token used to list and clone your repositories. Validated on save.</Text>
      <Card style={{ padding: 16 }}>
        {login ? (
          <View style={styles.okBanner}>
            <Dot color={colors.green} size={8} />
            <Text style={{ fontSize: 13, color: colors.greenFg }}>Authorized as</Text>
            <MonoText style={{ fontSize: 13, fontWeight: '600', color: colors.greenFg }}>@{login}</MonoText>
          </View>
        ) : null}
        <Text style={styles.label}>Personal access token</Text>
        <TextInput
          style={[styles.input, { fontFamily: mono }]}
          placeholder={settings.data?.github_token || 'ghp_…'}
          placeholderTextColor={colors.textFaint}
          autoCapitalize="none"
          autoCorrect={false}
          secureTextEntry
          value={token}
          onChangeText={setToken}
        />
        <PrimaryButton
          label={save.isPending ? 'Validating…' : 'Save token'}
          disabled={!token.trim() || save.isPending}
          onPress={() =>
            save.mutate(
              { github_token: token.trim() },
              {
                onSuccess: (res) => {
                  setLogin(res.login ?? null)
                  setToken('')
                },
              },
            )
          }
        />
        {save.isError ? (
          <Text style={{ marginTop: 10, fontSize: 12.5, color: colors.redFg }}>
            {(save.error as Error).message}
          </Text>
        ) : null}
        <Text style={styles.hint}>Needs repo scope. Masked after saving.</Text>
      </Card>
    </View>
  )
}

function ReposSection() {
  const repos = useRepos()
  return (
    <View>
      <Text style={styles.h2}>Repositories</Text>
      <Text style={styles.lede}>Global registry. A repo can belong to several projects. Read-only here.</Text>
      <View style={{ gap: 8 }}>
        {(repos.data ?? []).map((r) => (
          <Card key={r.id} style={{ padding: 13 }}>
            <MonoText style={{ fontSize: 14.5, fontWeight: '600', color: colors.text, marginBottom: 5 }}>
              {r.id}
            </MonoText>
            <MonoText style={{ fontSize: 11.5, color: colors.textFaint }}>{r.path}</MonoText>
            <Text style={{ fontSize: 12, color: colors.textDim, marginTop: 4 }}>
              default branch: {r.default_branch}
            </Text>
          </Card>
        ))}
        {repos.isSuccess && repos.data.length === 0 ? <EmptyState text="No repositories registered." /> : null}
      </View>
    </View>
  )
}

function ServerSection() {
  const { active, servers } = useServers()
  return (
    <View>
      <Text style={styles.h2}>Server</Text>
      <Text style={styles.lede}>The rocketd instance this app is talking to.</Text>
      <Card style={{ gap: 9, marginBottom: 14 }}>
        <Row k="name" v={active?.name ?? '—'} />
        <Row k="address" v={active ? `${active.host}:${active.port}` : '—'} />
        <Row k="saved servers" v={String(servers.length)} />
      </Card>
      <PrimaryButton label="Switch server" onPress={() => router.navigate('/servers')} />
      <Text style={styles.hint}>
        To reach the daemon from your phone, rocketd must listen on the LAN: `host: 0.0.0.0` in
        ~/.rocket/config.yaml.
      </Text>
    </View>
  )
}

function Row({ k, v }: { k: string; v: string }) {
  return (
    <View style={{ flexDirection: 'row', justifyContent: 'space-between', gap: 12 }}>
      <Text style={{ fontSize: 13, color: colors.textDim }}>{k}</Text>
      <MonoText style={{ fontSize: 12.5, fontWeight: '600', color: colors.text }}>{v}</MonoText>
    </View>
  )
}

export default function SettingsScreen() {
  const [section, setSection] = useState('server')

  return (
    <SafeAreaView style={{ flex: 1, backgroundColor: colors.page }} edges={['top']}>
      <View style={styles.header}>
        <View style={styles.logo}>
          <Text style={{ color: '#fff', fontFamily: mono, fontSize: 13, fontWeight: '700' }}>R</Text>
        </View>
        <Text style={{ fontSize: 17, fontWeight: '700', letterSpacing: -0.2 }}>Settings</Text>
      </View>
      <View style={styles.chipBar}>
        <ChipTabs
          chips={[
            { key: 'server', label: 'Server' },
            { key: 'github', label: 'GitHub' },
            { key: 'repos', label: 'Repositories' },
          ]}
          active={section}
          onChange={setSection}
        />
      </View>
      <ScrollView contentContainerStyle={{ padding: 16, paddingBottom: 24 }}>
        {section === 'server' ? <ServerSection /> : null}
        {section === 'github' ? <GithubSection /> : null}
        {section === 'repos' ? <ReposSection /> : null}
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
  chipBar: {
    padding: 12,
    paddingHorizontal: 16,
    backgroundColor: colors.card,
    borderBottomWidth: 1,
    borderBottomColor: colors.border,
  },
  h2: { fontSize: 20, fontWeight: '700', letterSpacing: -0.3, marginBottom: 4, color: colors.text },
  lede: { fontSize: 13.5, lineHeight: 20, color: colors.textDim, marginBottom: 16 },
  label: { fontSize: 12.5, fontWeight: '600', color: colors.text, marginBottom: 6 },
  input: {
    height: 42,
    paddingHorizontal: 12,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: 10,
    fontSize: 14,
    color: colors.text,
    marginBottom: 10,
    backgroundColor: colors.card,
  },
  okBanner: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 9,
    padding: 11,
    paddingHorizontal: 13,
    backgroundColor: colors.greenBgSoft,
    borderWidth: 1,
    borderColor: colors.greenBorder,
    borderRadius: radius.md + 1,
    marginBottom: 16,
  },
  hint: { marginTop: 10, fontSize: 12, lineHeight: 17, color: colors.textFaint },
})
