import { StyleSheet, Text, View } from 'react-native'
import { questionTitle } from '../lib/questions'
import { Markdown } from './Markdown'

/**
 * A question's heading and markdown body (task #1264: the body is markdown
 * with the context folded in). The body repeats the heading when the daemon
 * derived no title, so it is shown only when it adds something.
 */
export function QuestionText({ q }: { q: { title?: string; body: string } }) {
  const title = questionTitle(q)
  const bodyAddsSomething = q.body.trim() !== title
  return (
    <View style={{ marginBottom: 14 }}>
      <Text style={styles.title}>{title}</Text>
      {bodyAddsSomething ? <Markdown>{q.body}</Markdown> : null}
    </View>
  )
}

const styles = StyleSheet.create({
  title: { fontSize: 17, lineHeight: 24, fontWeight: '700', letterSpacing: -0.2, marginBottom: 6 },
})
