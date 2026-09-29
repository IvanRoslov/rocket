import { router } from 'expo-router'
import { Pressable, Text, View } from 'react-native'
import { colors } from '../theme'

/** Shown when the active server has no valid device token; leads to re-pairing. */
export function AuthLostBanner() {
  return (
    <View
      style={{
        backgroundColor: colors.amberBgSoft,
        borderBottomWidth: 1,
        borderBottomColor: colors.amberBorder,
        paddingVertical: 6,
        paddingHorizontal: 12,
        flexDirection: 'row',
        alignItems: 'center',
        justifyContent: 'center',
        gap: 10,
      }}
    >
      <Text style={{ flexShrink: 1, fontSize: 11.5, fontWeight: '600', color: colors.amberFg }}>
        Доступ к серверу отозван или не настроен — подключи заново
      </Text>
      <Pressable accessibilityRole="button" onPress={() => router.push('/servers?pair=1')} hitSlop={8}>
        <Text style={{ fontSize: 11.5, fontWeight: '700', color: colors.amberFg, textDecorationLine: 'underline' }}>
          Подключить
        </Text>
      </Pressable>
    </View>
  )
}
