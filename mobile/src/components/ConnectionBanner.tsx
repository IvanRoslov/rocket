import { Text, View } from 'react-native'
import { useConnection } from '../api/events'
import { useServers } from '../servers/ServerContext'
import { colors } from '../theme'

/**
 * Thin amber strip shown under screen headers while the SSE stream is down
 * (the app then falls back to fast polling).
 */
export function ConnectionBanner() {
  const { sse } = useConnection()
  const { active } = useServers()
  if (sse || !active) return null
  return (
    <View
      style={{
        backgroundColor: colors.amberBgSoft,
        borderBottomWidth: 1,
        borderBottomColor: colors.amberBorder,
        paddingVertical: 5,
        alignItems: 'center',
      }}
    >
      <Text style={{ fontSize: 11.5, fontWeight: '600', color: colors.amberFg }}>
        live updates unavailable — polling…
      </Text>
    </View>
  )
}
