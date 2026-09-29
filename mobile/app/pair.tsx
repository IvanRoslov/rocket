import { router, useLocalSearchParams } from 'expo-router'
import { useState } from 'react'
import { StyleSheet, Text, TextInput, View } from 'react-native'
import { SafeAreaView } from 'react-native-safe-area-context'
import { Card, GhostButton, MonoText, PrimaryButton } from '../src/components/ui'
import { DEFAULT_DEVICE_NAME, parsePairLink, usePairing } from '../src/servers/pairing'
import { colors } from '../src/theme'

/** Deep-link target: rocketmobile://pair?url=…&code=…. Never pairs without a tap. */
export default function PairScreen() {
  const params = useLocalSearchParams<{ url?: string; code?: string }>()
  const link = parsePairLink(
    `rocketmobile://pair?url=${encodeURIComponent(params.url ?? '')}&code=${encodeURIComponent(params.code ?? '')}`,
  )
  const { pair, busy, error } = usePairing()
  const [name, setName] = useState(DEFAULT_DEVICE_NAME)

  return (
    <SafeAreaView style={{ flex: 1, backgroundColor: colors.page, padding: 16 }} edges={['top', 'bottom']}>
      <Card style={{ padding: 16 }}>
        {link ? (
          <>
            <Text style={styles.title}>Подключить к серверу?</Text>
            <MonoText style={{ color: colors.textDim, marginBottom: 14 }}>{link.baseUrl}</MonoText>
            <Text style={styles.label}>Имя устройства</Text>
            <TextInput
              style={styles.input}
              accessibilityLabel="Имя"
              placeholder={DEFAULT_DEVICE_NAME}
              placeholderTextColor={colors.textFaint}
              value={name}
              onChangeText={setName}
            />
            <View style={{ flexDirection: 'row', gap: 9 }}>
              <GhostButton label="Отмена" onPress={() => router.replace('/servers')} style={{ flex: 1 }} />
              <PrimaryButton
                label={busy ? 'Подключаю…' : 'Подключить'}
                disabled={busy}
                onPress={async () => {
                  if (await pair({ url: link.baseUrl, code: link.code, name })) router.replace('/(tabs)')
                }}
                style={{ flex: 1 }}
              />
            </View>
            {error ? <Text style={styles.error}>{error}</Text> : null}
          </>
        ) : (
          <>
            <Text style={styles.title}>Ссылка сопряжения повреждена</Text>
            <Text style={styles.hint}>Получи новый QR командой rocket pair.</Text>
            <GhostButton label="К серверам" onPress={() => router.replace('/servers')} />
          </>
        )}
      </Card>
    </SafeAreaView>
  )
}

const styles = StyleSheet.create({
  title: { fontSize: 17, fontWeight: '700', color: colors.text, marginBottom: 6 },
  label: { fontSize: 12.5, fontWeight: '600', color: colors.text, marginBottom: 6 },
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
  error: { marginTop: 10, fontSize: 12.5, color: colors.redFg },
  hint: { fontSize: 12.5, color: colors.textDim, marginBottom: 12 },
})
