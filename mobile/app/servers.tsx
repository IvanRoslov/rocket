import { useCameraPermissions, CameraView } from 'expo-camera'
import { router, useLocalSearchParams } from 'expo-router'
import { useEffect, useRef, useState } from 'react'
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
import { DEFAULT_DEVICE_NAME, parsePairLink, usePairing } from '../src/servers/pairing'
import { colors, mono, radius } from '../src/theme'

function ServerCard({ server }: { server: ServerEntry }) {
  const { activeId, setActive, removeServer } = useServers()
  const health = useHealth(server.baseUrl)
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
          {server.baseUrl}
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
        title={`${server.name} · ${server.baseUrl}`}
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

function Scanner({ onScan, onClose }: { onScan: (data: string) => void; onClose: () => void }) {
  const [perm, requestPerm] = useCameraPermissions()
  useEffect(() => {
    if (perm && !perm.granted) requestPerm()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [perm?.granted])

  return (
    <View style={{ gap: 10 }}>
      {perm?.granted ? (
        <CameraView
          style={styles.camera}
          facing="back"
          barcodeScannerSettings={{ barcodeTypes: ['qr'] }}
          onBarcodeScanned={({ data }) => onScan(data)}
        />
      ) : (
        <Text style={styles.hint}>Разреши доступ к камере, чтобы отсканировать QR, или введи адрес и код вручную.</Text>
      )}
      <GhostButton label="Ввести вручную" onPress={onClose} />
    </View>
  )
}

function AddServerForm({ onDone, startScanning }: { onDone: () => void; startScanning: boolean }) {
  const { pair, busy, error, setError } = usePairing()
  const [name, setName] = useState(DEFAULT_DEVICE_NAME)
  const [url, setUrl] = useState('')
  const [code, setCode] = useState('')
  const [scanning, setScanning] = useState(startScanning)
  const scanned = useRef(false)

  const submit = async (u: string, c: string) => {
    if (await pair({ url: u, code: c, name })) {
      onDone()
      router.replace('/(tabs)')
    }
  }

  // A scanned QR never pairs on its own: hand it to the confirm screen (same as a deep link).
  const onScan = (data: string) => {
    if (scanned.current) return
    const link = parsePairLink(data)
    if (!link) return
    scanned.current = true
    setScanning(false)
    router.push({ pathname: '/pair', params: { url: link.baseUrl, code: link.code } })
    scanned.current = false
  }

  return (
    <Card style={{ padding: 16 }}>
      {scanning ? (
        <Scanner onScan={onScan} onClose={() => setScanning(false)} />
      ) : (
        <>
          <PrimaryButton
            label="Сканировать QR"
            onPress={() => {
              setError(null)
              setScanning(true)
            }}
            style={{ marginBottom: 14 }}
          />
          <Text style={styles.formLabel}>Имя устройства</Text>
          <TextInput
            style={styles.input}
            accessibilityLabel="Имя"
            placeholder={DEFAULT_DEVICE_NAME}
            placeholderTextColor={colors.textFaint}
            value={name}
            onChangeText={setName}
          />
          <Text style={styles.formLabel}>Адрес</Text>
          <TextInput
            style={[styles.input, { fontFamily: mono }]}
            accessibilityLabel="Адрес"
            placeholder="https://mac.tailnet.ts.net"
            placeholderTextColor={colors.textFaint}
            autoCapitalize="none"
            autoCorrect={false}
            keyboardType="url"
            value={url}
            onChangeText={setUrl}
          />
          <Text style={styles.formLabel}>Код</Text>
          <TextInput
            style={[styles.input, { fontFamily: mono }]}
            accessibilityLabel="Код"
            placeholder="XXXX-XXXX"
            placeholderTextColor={colors.textFaint}
            autoCapitalize="characters"
            autoCorrect={false}
            value={code}
            onChangeText={setCode}
          />
          <View style={{ flexDirection: 'row', gap: 9, marginTop: 6 }}>
            <GhostButton label="Отмена" onPress={onDone} style={{ flex: 1 }} />
            <PrimaryButton
              label={busy ? 'Подключаю…' : 'Подключить'}
              disabled={busy}
              onPress={() => submit(url, code)}
              style={{ flex: 1 }}
            />
          </View>
        </>
      )}
      {error ? <Text style={styles.error}>{error}</Text> : null}
      <Text style={styles.hint}>
        На компьютере выполни `rocket pair` — он покажет QR и одноразовый код. Телефон должен быть в твоём Tailscale.
      </Text>
    </Card>
  )
}

export default function ServersScreen() {
  const { servers } = useServers()
  const { pair: pairParam } = useLocalSearchParams<{ pair?: string }>()
  const [adding, setAdding] = useState(pairParam === '1')
  const qc = useQueryClient()

  return (
    <SafeAreaView style={{ flex: 1, backgroundColor: colors.page }} edges={['top', 'bottom']}>
      <View style={styles.header}>
        <View style={styles.logo}>
          <Text style={{ color: '#fff', fontFamily: mono, fontSize: 13, fontWeight: '700' }}>R</Text>
        </View>
        <Text style={styles.headerTitle}>Servers</Text>
      </View>
      <KeyboardAvoidingView style={{ flex: 1 }} behavior="padding">
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
            <AddServerForm onDone={() => setAdding(false)} startScanning={pairParam === '1'} />
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
  camera: { height: 280, borderRadius: 12, overflow: 'hidden' },
  error: { marginTop: 10, fontSize: 12.5, color: colors.redFg },
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
