import { createContext, useCallback, useContext, useRef, useState, type ReactNode } from 'react'
import { Animated, StyleSheet, Text } from 'react-native'
import { useSafeAreaInsets } from 'react-native-safe-area-context'
import { colors } from '../theme'

export type ToastKind = 'ok' | 'error'

interface ToastValue {
  show: (message: string, kind?: ToastKind) => void
}

const ToastContext = createContext<ToastValue>({ show: () => {} })

export function useToast(): ToastValue {
  return useContext(ToastContext)
}

export function ToastProvider({ children }: { children: ReactNode }) {
  const insets = useSafeAreaInsets()
  const [toast, setToast] = useState<{ message: string; kind: ToastKind } | null>(null)
  const opacity = useRef(new Animated.Value(0)).current
  const hideTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  const show = useCallback(
    (message: string, kind: ToastKind = 'error') => {
      if (hideTimer.current) clearTimeout(hideTimer.current)
      setToast({ message, kind })
      Animated.timing(opacity, { toValue: 1, duration: 180, useNativeDriver: true }).start()
      hideTimer.current = setTimeout(() => {
        Animated.timing(opacity, { toValue: 0, duration: 220, useNativeDriver: true }).start(() => setToast(null))
      }, 3500)
    },
    [opacity],
  )

  return (
    <ToastContext.Provider value={{ show }}>
      {children}
      {toast ? (
        <Animated.View
          pointerEvents="none"
          style={[
            styles.toast,
            { top: insets.top + 8, opacity },
            toast.kind === 'error' ? styles.error : styles.ok,
          ]}
        >
          <Text style={{ fontSize: 13, fontWeight: '600', color: toast.kind === 'error' ? colors.redFg : colors.greenFg }}>
            {toast.message}
          </Text>
        </Animated.View>
      ) : null}
    </ToastContext.Provider>
  )
}

const styles = StyleSheet.create({
  toast: {
    position: 'absolute',
    left: 16,
    right: 16,
    padding: 12,
    paddingHorizontal: 14,
    borderRadius: 12,
    borderWidth: 1,
    shadowColor: '#000',
    shadowOpacity: 0.12,
    shadowRadius: 12,
    shadowOffset: { width: 0, height: 4 },
    elevation: 5,
    zIndex: 100,
  },
  error: { backgroundColor: colors.redBgSoft, borderColor: colors.redBorder },
  ok: { backgroundColor: colors.greenBgSoft, borderColor: colors.greenBorder },
})
