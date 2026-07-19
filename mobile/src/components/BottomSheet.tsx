import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Animated, Easing, Modal, Pressable, StyleSheet, View } from 'react-native'
import { useSafeAreaInsets } from 'react-native-safe-area-context'

/**
 * Bottom sheet with the backdrop and the panel animated separately: the
 * backdrop fades in place while only the panel slides up. (A plain
 * `Modal animationType="slide"` slides the semi-transparent backdrop
 * together with the panel, which looks broken.)
 *
 * Parents keep the usual `visible` / `onClose` contract; the sheet stays
 * mounted while the exit animation plays.
 */
export function BottomSheet({
  visible,
  onClose,
  children,
}: {
  visible: boolean
  onClose: () => void
  children: ReactNode
}) {
  const [mounted, setMounted] = useState(visible)
  const progress = useRef(new Animated.Value(0)).current
  const insets = useSafeAreaInsets()

  useEffect(() => {
    if (visible) {
      setMounted(true)
      Animated.timing(progress, {
        toValue: 1,
        duration: 240,
        easing: Easing.out(Easing.cubic),
        useNativeDriver: true,
      }).start()
    } else {
      Animated.timing(progress, {
        toValue: 0,
        duration: 180,
        easing: Easing.in(Easing.cubic),
        useNativeDriver: true,
      }).start(() => setMounted(false))
    }
  }, [visible, progress])

  if (!mounted) return null

  return (
    <Modal transparent animationType="none" visible onRequestClose={onClose}>
      <Animated.View style={[StyleSheet.absoluteFill, styles.backdrop, { opacity: progress }]}>
        <Pressable style={{ flex: 1 }} onPress={onClose} />
      </Animated.View>
      <View style={styles.host} pointerEvents="box-none">
        <Animated.View
          style={[
            styles.sheet,
            {
              // breathing room above the home indicator / Android nav buttons
              paddingBottom: Math.max(28, insets.bottom + 16),
              transform: [
                { translateY: progress.interpolate({ inputRange: [0, 1], outputRange: [640, 0] }) },
              ],
            },
          ]}
        >
          <View style={styles.grabber} />
          {children}
        </Animated.View>
      </View>
    </Modal>
  )
}

const styles = StyleSheet.create({
  backdrop: { backgroundColor: 'rgba(15,15,17,.4)' },
  host: { position: 'absolute', top: 0, left: 0, right: 0, bottom: 0, justifyContent: 'flex-end' },
  sheet: {
    backgroundColor: '#fbfbfa',
    borderTopLeftRadius: 18,
    borderTopRightRadius: 18,
    padding: 16,
    maxHeight: '85%',
  },
  grabber: {
    alignSelf: 'center',
    width: 38,
    height: 5,
    borderRadius: 3,
    backgroundColor: '#d4d4d1',
    marginBottom: 14,
  },
})
