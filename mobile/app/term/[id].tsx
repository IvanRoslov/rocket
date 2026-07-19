import * as Clipboard from 'expo-clipboard'
import { router, useLocalSearchParams } from 'expo-router'
import { useEffect, useRef, useState } from 'react'
import {
  KeyboardAvoidingView,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  View,
} from 'react-native'
import { SafeAreaView } from 'react-native-safe-area-context'
import { WebView } from 'react-native-webview'
import { useBaseUrl, useSessionOutput, useSessions } from '../../src/api/queries'
import { Dot } from '../../src/components/ui'
import { sessionDot, stripAnsi } from '../../src/lib/format'
import { buildWsUrl, SPECIAL_KEYS } from '../../src/terminal/protocol'
import { TERMINAL_HTML } from '../../src/terminal/terminalHtml'
import { colors, mono } from '../../src/theme'

type WsStatus = 'connecting' | 'open' | 'closed' | 'error'

const STATUS_LABEL: Record<WsStatus, { text: string; color: string }> = {
  connecting: { text: 'connecting…', color: colors.amber },
  open: { text: 'live', color: colors.green },
  closed: { text: 'closed', color: colors.slate },
  error: { text: 'error', color: colors.red },
}

/** Read-only polling fallback when the WebSocket can't be established. */
function SnapshotView({ id }: { id: string }) {
  const output = useSessionOutput(id)
  const scrollRef = useRef<ScrollView>(null)
  const text = stripAnsi(output.data?.output ?? '').trimEnd()

  useEffect(() => {
    scrollRef.current?.scrollToEnd({ animated: false })
  }, [text])

  return (
    <ScrollView ref={scrollRef} contentContainerStyle={{ padding: 16 }} style={{ flex: 1 }}>
      <ScrollView horizontal showsHorizontalScrollIndicator={false}>
        <Text style={styles.snapshotText}>
          {output.isError ? 'failed to capture output — session may be gone' : text || 'no output yet…'}
        </Text>
      </ScrollView>
    </ScrollView>
  )
}

export default function TerminalScreen() {
  const { id } = useLocalSearchParams<{ id: string }>()
  const baseUrl = useBaseUrl()
  const { data: sessions } = useSessions()
  const session = sessions?.find((s) => s.id === id)
  const webRef = useRef<WebView>(null)
  const [status, setStatus] = useState<WsStatus>('connecting')
  const [copied, setCopied] = useState(false)
  const [fallback, setFallback] = useState(false)
  const errorCount = useRef(0)

  const wsUrl = buildWsUrl(baseUrl, id)

  const connect = () => {
    setStatus('connecting')
    webRef.current?.injectJavaScript(`window.rocketTerm && window.rocketTerm.connect(${JSON.stringify(wsUrl)}); true;`)
  }

  const sendKey = (seq: string) => {
    webRef.current?.injectJavaScript(`window.rocketTerm && window.rocketTerm.sendKey(${JSON.stringify(seq)}); true;`)
  }

  const onMessage = (raw: string) => {
    try {
      const msg = JSON.parse(raw) as { type: string; value?: string }
      if (msg.type === 'ready') connect()
      if (msg.type === 'status') {
        const v = msg.value as WsStatus
        setStatus(v)
        if (v === 'error') {
          errorCount.current += 1
          if (errorCount.current >= 2) setFallback(true)
        }
        if (v === 'open') errorCount.current = 0
      }
    } catch {
      // non-JSON message — ignore
    }
  }

  const st = STATUS_LABEL[status]

  return (
    <SafeAreaView style={{ flex: 1, backgroundColor: colors.termBg }} edges={['top', 'bottom']}>
      <View style={styles.header}>
        <Dot color={session ? sessionDot(session.state, session.activity) : colors.slate} size={9} />
        <Text style={styles.title} numberOfLines={1}>
          {session?.tmux_name ?? id}
        </Text>
        {!fallback ? (
          <View style={[styles.statusPill, { borderColor: st.color }]}>
            <Text style={{ fontSize: 10.5, fontWeight: '600', color: st.color }}>{st.text}</Text>
          </View>
        ) : (
          <View style={[styles.statusPill, { borderColor: colors.amber }]}>
            <Text style={{ fontSize: 10.5, fontWeight: '600', color: colors.amber }}>snapshot</Text>
          </View>
        )}
        <Pressable
          style={styles.headBtn}
          onPress={() => {
            Clipboard.setStringAsync(`rocket attach ${session?.tmux_name ?? id}`)
            setCopied(true)
            setTimeout(() => setCopied(false), 2000)
          }}
        >
          <Text style={styles.headBtnText}>{copied ? 'copied ✓' : 'attach ⧉'}</Text>
        </Pressable>
        <Pressable style={styles.headBtn} onPress={() => router.back()}>
          <Text style={[styles.headBtnText, { color: colors.textFaint }]}>✕</Text>
        </Pressable>
      </View>

      <KeyboardAvoidingView style={{ flex: 1 }} behavior={Platform.OS === 'ios' ? 'padding' : undefined}>
        {fallback ? (
          <SnapshotView id={id} />
        ) : (
          <WebView
            ref={webRef}
            source={{ html: TERMINAL_HTML }}
            originWhitelist={['*']}
            style={{ flex: 1, backgroundColor: colors.termBg }}
            onMessage={(e) => onMessage(e.nativeEvent.data)}
            keyboardDisplayRequiresUserAction={false}
            hideKeyboardAccessoryView
            webviewDebuggingEnabled={__DEV__}
          />
        )}

        <View style={styles.keyBar}>
          <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={{ gap: 6 }}>
            {SPECIAL_KEYS.map((k) => (
              <Pressable key={k.label} style={styles.key} onPress={() => sendKey(k.seq)} disabled={fallback}>
                <Text style={[styles.keyText, fallback && { opacity: 0.35 }]}>{k.label}</Text>
              </Pressable>
            ))}
            {status === 'closed' || status === 'error' ? (
              <Pressable
                style={[styles.key, { borderColor: colors.green }]}
                onPress={() => {
                  errorCount.current = 0
                  setFallback(false)
                  connect()
                }}
              >
                <Text style={[styles.keyText, { color: colors.green }]}>reconnect ⟳</Text>
              </Pressable>
            ) : null}
          </ScrollView>
        </View>
      </KeyboardAvoidingView>
    </SafeAreaView>
  )
}

const styles = StyleSheet.create({
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 9,
    padding: 12,
    paddingHorizontal: 14,
    backgroundColor: colors.termHeader,
    borderBottomWidth: 1,
    borderBottomColor: '#000',
  },
  title: { fontFamily: mono, fontSize: 13, fontWeight: '600', color: '#e4e4e7', flex: 1 },
  statusPill: {
    borderWidth: 1,
    borderRadius: 7,
    paddingHorizontal: 8,
    paddingVertical: 3,
  },
  headBtn: {
    height: 30,
    paddingHorizontal: 11,
    backgroundColor: '#2a2a2e',
    borderWidth: 1,
    borderColor: '#3a3a3f',
    borderRadius: 7,
    alignItems: 'center',
    justifyContent: 'center',
  },
  headBtnText: { fontSize: 11.5, fontWeight: '600', color: colors.termText },
  keyBar: {
    padding: 8,
    paddingHorizontal: 10,
    backgroundColor: colors.termHeader,
    borderTopWidth: 1,
    borderTopColor: '#000',
  },
  key: {
    minWidth: 44,
    height: 36,
    paddingHorizontal: 12,
    backgroundColor: '#2a2a2e',
    borderWidth: 1,
    borderColor: '#3a3a3f',
    borderRadius: 8,
    alignItems: 'center',
    justifyContent: 'center',
  },
  keyText: { fontFamily: mono, fontSize: 13, color: colors.termText },
  snapshotText: { fontFamily: mono, fontSize: 12, lineHeight: 20, color: colors.termText },
})
