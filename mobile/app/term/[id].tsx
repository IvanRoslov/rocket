import * as Clipboard from 'expo-clipboard'
import { router, useLocalSearchParams } from 'expo-router'
import { useEffect, useRef, useState } from 'react'
import { Pressable, ScrollView, StyleSheet, Text, View } from 'react-native'
import { SafeAreaView } from 'react-native-safe-area-context'
import { useSessionOutput, useSessions } from '../../src/api/queries'
import { Dot } from '../../src/components/ui'
import { sessionDot, stripAnsi } from '../../src/lib/format'
import { colors, mono } from '../../src/theme'

// Read-only terminal: polls capture-pane snapshots every 2s. Live input
// goes through `rocket attach` on a real terminal (copy button below).
export default function TerminalScreen() {
  const { id } = useLocalSearchParams<{ id: string }>()
  const output = useSessionOutput(id)
  const { data: sessions } = useSessions()
  const session = sessions?.find((s) => s.id === id)
  const scrollRef = useRef<ScrollView>(null)
  const [copied, setCopied] = useState(false)

  const text = stripAnsi(output.data?.output ?? '').trimEnd()

  useEffect(() => {
    scrollRef.current?.scrollToEnd({ animated: false })
  }, [text])

  return (
    <SafeAreaView style={{ flex: 1, backgroundColor: colors.termBg }} edges={['top', 'bottom']}>
      <View style={styles.header}>
        <Dot color={session ? sessionDot(session.state, session.activity) : colors.slate} size={9} />
        <Text style={styles.title} numberOfLines={1}>
          {session?.tmux_name ?? id}
        </Text>
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
      <ScrollView ref={scrollRef} contentContainerStyle={{ padding: 16 }}>
        <ScrollView horizontal showsHorizontalScrollIndicator={false}>
          <Text style={styles.term}>
            {output.isError ? 'failed to capture output — session may be gone' : text || 'no output yet…'}
            <Text style={{ color: colors.green }}>▊</Text>
          </Text>
        </ScrollView>
      </ScrollView>
    </SafeAreaView>
  )
}

const styles = StyleSheet.create({
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 9,
    padding: 14,
    backgroundColor: colors.termHeader,
    borderBottomWidth: 1,
    borderBottomColor: '#000',
  },
  title: { fontFamily: mono, fontSize: 13, fontWeight: '600', color: '#e4e4e7', flex: 1 },
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
  term: { fontFamily: mono, fontSize: 12, lineHeight: 20, color: colors.termText },
})
