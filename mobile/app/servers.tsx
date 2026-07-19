import { router } from 'expo-router'
import { useState } from 'react'
import {
  Alert,
  KeyboardAvoidingView,
  Platform,
  Pressable,
  RefreshControl,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native'
import { SafeAreaView } from 'react-native-safe-area-context'
import { useQueryClient } from '@tanstack/react-query'
import { useHealth } from '../src/api/queries'
import { ActionSheet } from '../src/components/ActionSheet'
import { Card, Dot, GhostButton, MonoText, PrimaryButton } from '../src/components/ui'
import { uptime } from '../src/lib/format'
import { useServers, type ServerEntry } from '../src/servers/ServerContext'
import { colors, mono, radius } from '../src/theme'

function ServerCard({ server }: { server: ServerEntry }) {
  const { activeId, setActive, removeServer } = useServers()
  const health = useHealth(`http://${server.host}:${server.port}`)
  const online = health.isSuccess
  const isActive = activeId === server.id
  const [menu, setMenu] = useState(false)

  const confirmRemove = () =>
    Alert.alert('Remove server', `Remove “${server.name}” from the list?`, [
      { text: 'Cancel', style: 'cancel' },
      { text: 'Remove', style: 'destructive', onPress: () => removeServer(server.id) },
    ])

  return (
    <Pressable
      onPress={() => {
        setActive(server.id)
        router.replace('/(tabs)')
      }}
      onLongPress={() => setMenu(true)}
    >
      <Card style={isActive ? { borderColor: colors.indigoBorder, backgroundColor: '#fbfbff' } : undefined}>
        <View style={styles.cardHead}>
          <Dot color={online ? colors.green : health.isLoading || health.isFetching ? colors.amber : colors.red} size={9} />
          <Text style={styles.serverName}>{server.name}</Text>
          {isActive ? (
            <View style={styles.activePill}>
              <Text style={{ color: colors.indigoFg, fontSize: 10.5, fontWeight: '600' }}>active</Text>
            </View>
          ) : null}
          <Pressable onPress={() => setMenu(true)} hitSlop={10}>
            <Text style={{ fontSize: 17, color: colors.textDim }}>⋯</Text>
          </Pressable>
        </View>
        <MonoText style={{ color: colors.textDim, marginBottom: 6 }}>
          {server.host}:{server.port}
        </MonoText>
        <Text style={{ fontSize: 12, color: colors.textFaint }}>
          {online
            ? `rocketd ${health.data.version} · up ${health.data.uptime.replace(/(\.\d+)?s.*/, 's')}`
            : health.isLoading || health.isFetching
              ? 'checking…'
              : 'offline — daemon unreachable'}
        </Text>
      </Card>
      <ActionSheet
        visible={menu}
        title={`${server.name} · ${server.host}:${server.port}`}
        onClose={() => setMenu(false)}
        actions={[
          {
            label: 'Connect',
            onPress: () => {
              setActive(server.id)
              router.replace('/(tabs)')
            },
          },
          { label: 'Check again', onPress: () => health.refetch() },
          { label: 'Remove server', destructive: true, onPress: confirmRemove },
        ]}
      />
    </Pressable>
  )
}

function AddServerForm({ onDone }: { onDone: () => void }) {
  const { addServer } = useServers()
  const [name, setName] = useState('')
  const [host, setHost] = useState('')
  const [port, setPort] = useState('4477')
  const valid = host.trim().length > 0 && /^\d+$/.test(port.trim())

  return (
    <Card style={{ padding: 16 }}>
      <Text style={styles.formLabel}>Name</Text>
      <TextInput
        style={styles.input}
        placeholder="Home desktop"
        placeholderTextColor={colors.textFaint}
        value={name}
        onChangeText={setName}
      />
      <View style={{ flexDirection: 'row', gap: 10 }}>
        <View style={{ flex: 2 }}>
          <Text style={styles.formLabel}>Host / IP</Text>
          <TextInput
            style={[styles.input, { fontFamily: mono }]}
            placeholder="192.168.1.10"
            placeholderTextColor={colors.textFaint}
            autoCapitalize="none"
            autoCorrect={false}
            keyboardType="numbers-and-punctuation"
            value={host}
            onChangeText={setHost}
          />
        </View>
        <View style={{ flex: 1 }}>
          <Text style={styles.formLabel}>Port</Text>
          <TextInput
            style={[styles.input, { fontFamily: mono }]}
            keyboardType="number-pad"
            value={port}
            onChangeText={setPort}
          />
        </View>
      </View>
      <View style={{ flexDirection: 'row', gap: 9, marginTop: 6 }}>
        <GhostButton label="Cancel" onPress={onDone} style={{ flex: 1 }} />
        <PrimaryButton
          label="Add server"
          disabled={!valid}
          onPress={() => {
            addServer({
              name: name.trim() || host.trim(),
              host: host.trim(),
              port: parseInt(port, 10),
            })
            onDone()
            router.replace('/(tabs)')
          }}
          style={{ flex: 1 }}
        />
      </View>
      <Text style={styles.hint}>
        The daemon must listen on your LAN: set `host: 0.0.0.0` in ~/.rocket/config.yaml and restart rocketd.
      </Text>
    </Card>
  )
}

export default function ServersScreen() {
  const { servers } = useServers()
  const [adding, setAdding] = useState(false)
  const qc = useQueryClient()

  return (
    <SafeAreaView style={{ flex: 1, backgroundColor: colors.page }} edges={['top', 'bottom']}>
      <View style={styles.header}>
        <View style={styles.logo}>
          <Text style={{ color: '#fff', fontFamily: mono, fontSize: 13, fontWeight: '700' }}>R</Text>
        </View>
        <Text style={styles.headerTitle}>Servers</Text>
      </View>
      <KeyboardAvoidingView style={{ flex: 1 }} behavior={Platform.OS === 'ios' ? 'padding' : undefined}>
        <ScrollView
          contentContainerStyle={{ padding: 16, gap: 12 }}
          refreshControl={
            <RefreshControl
              refreshing={false}
              onRefresh={() => qc.refetchQueries({ predicate: (q) => q.queryKey[1] === 'health' })}
            />
          }
        >
          <Text style={styles.lede}>
            Pick the machine running rocketd. Tap to connect, ⋯ for actions.
          </Text>
          {servers.map((s) => (
            <ServerCard key={s.id} server={s} />
          ))}
          {adding ? (
            <AddServerForm onDone={() => setAdding(false)} />
          ) : (
            <Pressable style={styles.addBtn} onPress={() => setAdding(true)}>
              <Text style={{ fontSize: 20, color: colors.textDim }}>＋</Text>
              <Text style={{ fontSize: 13.5, fontWeight: '600', color: colors.textDim }}>Add server</Text>
            </Pressable>
          )}
        </ScrollView>
      </KeyboardAvoidingView>
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
  headerTitle: { fontSize: 17, fontWeight: '700', color: colors.text, letterSpacing: -0.2 },
  lede: { fontSize: 13.5, lineHeight: 20, color: colors.textDim, marginBottom: 4 },
  cardHead: { flexDirection: 'row', alignItems: 'center', gap: 9, marginBottom: 8 },
  serverName: { fontSize: 16.5, fontWeight: '700', color: colors.text, flex: 1 },
  activePill: {
    backgroundColor: colors.indigoBg,
    paddingHorizontal: 8,
    paddingVertical: 3,
    borderRadius: radius.sm,
  },
  formLabel: { fontSize: 12.5, fontWeight: '600', color: colors.text, marginBottom: 6 },
  input: {
    height: 42,
    paddingHorizontal: 12,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: 10,
    fontSize: 14,
    color: colors.text,
    marginBottom: 12,
    backgroundColor: colors.card,
  },
  hint: { marginTop: 12, fontSize: 12, lineHeight: 17, color: colors.textFaint },
  addBtn: {
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
})
