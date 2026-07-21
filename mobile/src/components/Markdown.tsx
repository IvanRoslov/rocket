import MarkdownDisplay from 'react-native-markdown-display'
import { colors, mono } from '../theme'

// Themed GFM renderer: tables, fenced code, lists, quotes, links. Wide
// tables scroll horizontally instead of squashing the columns.
const mdStyles = {
  body: { color: colors.text, fontSize: 13.5, lineHeight: 20 },
  paragraph: { marginTop: 0, marginBottom: 8 },
  heading1: { fontSize: 19, fontWeight: '700' as const, letterSpacing: -0.3, marginBottom: 8, marginTop: 6 },
  heading2: { fontSize: 17, fontWeight: '700' as const, letterSpacing: -0.2, marginBottom: 7, marginTop: 6 },
  heading3: { fontSize: 15, fontWeight: '700' as const, marginBottom: 6, marginTop: 4 },
  heading4: { fontSize: 13.5, fontWeight: '700' as const, marginBottom: 5 },
  heading5: { fontSize: 13, fontWeight: '700' as const },
  heading6: { fontSize: 12.5, fontWeight: '700' as const, color: colors.textDim },
  strong: { fontWeight: '700' as const },
  em: { fontStyle: 'italic' as const },
  link: { color: colors.accent, textDecorationLine: 'underline' as const },
  blockquote: {
    backgroundColor: colors.cardAlt,
    borderLeftWidth: 3,
    borderLeftColor: colors.border,
    paddingLeft: 10,
    paddingVertical: 4,
    marginBottom: 8,
    marginLeft: 0,
  },
  code_inline: {
    fontFamily: mono,
    fontSize: 12,
    backgroundColor: colors.slateBg,
    color: colors.text,
    paddingHorizontal: 4,
    borderRadius: 4,
  },
  code_block: {
    fontFamily: mono,
    fontSize: 11.5,
    lineHeight: 17,
    backgroundColor: colors.termBg,
    color: colors.termText,
    padding: 10,
    borderRadius: 8,
    borderWidth: 0,
    marginBottom: 8,
  },
  fence: {
    fontFamily: mono,
    fontSize: 11.5,
    lineHeight: 17,
    backgroundColor: colors.termBg,
    color: colors.termText,
    padding: 10,
    borderRadius: 8,
    borderWidth: 0,
    marginBottom: 8,
  },
  bullet_list: { marginBottom: 8 },
  ordered_list: { marginBottom: 8 },
  list_item: { flexDirection: 'row' as const, marginBottom: 3 },
  bullet_list_icon: { color: colors.textDim, marginRight: 6 },
  ordered_list_icon: { color: colors.textDim, marginRight: 6 },
  hr: { backgroundColor: colors.border, height: 1, marginVertical: 10 },
  table: {
    width: '100%' as const,
    alignSelf: 'stretch' as const,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: 8,
    marginBottom: 8,
    overflow: 'hidden' as const,
  },
  thead: { backgroundColor: colors.cardAlt },
  th: {
    padding: 6,
    paddingHorizontal: 9,
    fontWeight: '700' as const,
    fontSize: 12,
    borderColor: colors.borderSoft,
  },
  tr: { borderBottomWidth: 1, borderColor: colors.borderSoft, flexDirection: 'row' as const },
  td: { padding: 6, paddingHorizontal: 9, fontSize: 12.5 },
}

export function Markdown({ children }: { children: string }) {
  return <MarkdownDisplay style={mdStyles}>{children}</MarkdownDisplay>
}
