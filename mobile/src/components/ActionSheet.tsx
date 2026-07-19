import { Modal, Pressable, StyleSheet, Text, View } from 'react-native'
import { colors } from '../theme'

export interface ActionDef {
  label: string
  destructive?: boolean
  disabled?: boolean
  onPress: () => void
}

/** Bottom action sheet styled like the sessions sheet in the mockups. */
export function ActionSheet({
  visible,
  title,
  actions,
  onClose,
}: {
  visible: boolean
  title: string
  actions: ActionDef[]
  onClose: () => void
}) {
  if (!visible) return null
  return (
    <Modal transparent animationType="slide" onRequestClose={onClose}>
      <Pressable style={styles.backdrop} onPress={onClose}>
        <Pressable style={styles.sheet} onPress={() => {}}>
          <View style={styles.grabber} />
          <Text style={styles.title}>{title}</Text>
          <View style={{ gap: 8 }}>
            {actions.map((a) => (
              <Pressable
                key={a.label}
                disabled={a.disabled}
                onPress={() => {
                  onClose()
                  a.onPress()
                }}
                style={[styles.action, a.disabled && { opacity: 0.4 }]}
              >
                <Text
                  style={{
                    fontSize: 14,
                    fontWeight: '600',
                    color: a.destructive ? colors.redFg : colors.text,
                  }}
                >
                  {a.label}
                </Text>
              </Pressable>
            ))}
          </View>
          <Pressable style={styles.cancel} onPress={onClose}>
            <Text style={{ fontSize: 14, fontWeight: '600', color: colors.textDim }}>Cancel</Text>
          </Pressable>
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
  title: { fontSize: 15, fontWeight: '700', color: colors.text, marginBottom: 14 },
  action: {
    height: 48,
    backgroundColor: colors.card,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: 11,
    alignItems: 'center',
    justifyContent: 'center',
  },
  cancel: { height: 44, alignItems: 'center', justifyContent: 'center', marginTop: 10 },
})
