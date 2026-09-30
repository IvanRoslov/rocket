import { useState } from 'react'
import { Pressable, StyleSheet, Text, View } from 'react-native'
import { questionBrief, questionTitle } from '../lib/questions'
import { colors } from '../theme'
import { Markdown } from './Markdown'

/**
 * A question's heading and markdown body (task #1264: the body is markdown
 * with the context folded in). The body repeats the heading when the daemon
 * derived no title, so it is shown only when it adds something.
 *
 * When the agent wrote a brief — the plain-language version: problem,
 * options, recommendation — it is shown in the body's place and the full
 * body waits behind a collapsed «Подробности» toggle. No brief: as before.
 */
export function QuestionText({ q }: { q: { title?: string; brief?: string; body: string } }) {
  const [detailsOpen, setDetailsOpen] = useState(false)
  const title = questionTitle(q)
  const brief = questionBrief(q)
  const bodyAddsSomething = q.body.trim() !== title
  if (!brief) {
    return (
      <View style={{ marginBottom: 14 }}>
        <Text style={styles.title}>{title}</Text>
        {bodyAddsSomething ? <Markdown>{q.body}</Markdown> : null}
      </View>
    )
  }
  return (
    <View style={{ marginBottom: 14 }}>
      <Text style={styles.title}>{title}</Text>
      <Markdown>{brief}</Markdown>
      {bodyAddsSomething ? (
        <>
          <Pressable
            onPress={() => setDetailsOpen((v) => !v)}
            hitSlop={6}
            style={styles.detailsToggle}
            accessibilityRole="button"
            accessibilityState={{ expanded: detailsOpen }}
          >
            <Text style={styles.detailsText}>{detailsOpen ? '▾' : '▸'} Подробности</Text>
          </Pressable>
          {detailsOpen ? (
            <View style={styles.details}>
              <Markdown>{q.body}</Markdown>
            </View>
          ) : null}
        </>
      ) : null}
    </View>
  )
}

const styles = StyleSheet.create({
  title: { fontSize: 17, lineHeight: 24, fontWeight: '700', letterSpacing: -0.2, marginBottom: 6 },
  detailsToggle: { alignSelf: 'flex-start', paddingVertical: 4 },
  detailsText: { fontSize: 12.5, fontWeight: '600', color: colors.accent },
  details: { marginTop: 8, paddingLeft: 10, borderLeftWidth: 2, borderLeftColor: colors.border },
})
