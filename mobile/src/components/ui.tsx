import { Pressable, ScrollView, StyleSheet, Text, View, type ViewStyle } from 'react-native'
import { colors, mono, radius } from '../theme'

/** Header back arrow with a finger-sized touch target. */
export function BackButton({ onPress }: { onPress: () => void }) {
  return (
    <Pressable onPress={onPress} hitSlop={10} style={styles.backBtn}>
      <Text style={{ fontSize: 24, lineHeight: 26, color: colors.textDim }}>‹</Text>
    </Pressable>
  )
}

export function Dot({ color, size = 8 }: { color: string; size?: number }) {
  return <View style={{ width: size, height: size, borderRadius: size / 2, backgroundColor: color }} />
}

export function Badge({ label, fg, bg }: { label: string; fg: string; bg: string }) {
  return (
    <View style={{ backgroundColor: bg, paddingHorizontal: 8, paddingVertical: 3, borderRadius: radius.sm }}>
      <Text style={{ color: fg, fontSize: 11.5, fontWeight: '600' }}>{label}</Text>
    </View>
  )
}

export function Card({ children, style }: { children: React.ReactNode; style?: ViewStyle }) {
  return <View style={[styles.card, style]}>{children}</View>
}

export function SectionTitle({ children, right }: { children: string; right?: React.ReactNode }) {
  return (
    <View style={styles.sectionRow}>
      <Text style={styles.sectionTitle}>{children}</Text>
      {right}
    </View>
  )
}

export function MonoText({
  children,
  style,
  ...rest
}: { children: React.ReactNode; style?: object } & import('react-native').TextProps) {
  return (
    <Text style={[{ fontFamily: mono, fontSize: 12, color: colors.textMid }, style]} {...rest}>
      {children}
    </Text>
  )
}

export interface ChipDef {
  key: string
  label: string
  count?: number
  dot?: string
  warn?: boolean
}

/** Horizontal pill-tab row used for kanban columns, task tabs and settings sections. */
export function ChipTabs({
  chips,
  active,
  onChange,
}: {
  chips: ChipDef[]
  active: string
  onChange: (key: string) => void
}) {
  return (
    <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.chipRow}>
      {chips.map((c) => {
        const on = c.key === active
        return (
          <Pressable
            key={c.key}
            onPress={() => onChange(c.key)}
            style={[
              styles.chip,
              {
                backgroundColor: on ? colors.ink : colors.card,
                borderColor: on ? colors.ink : c.warn ? colors.amberBorder : colors.border,
              },
            ]}
          >
            {c.dot ? <Dot color={c.dot} size={7} /> : null}
            <Text
              style={{
                fontSize: 13,
                fontWeight: '600',
                color: on ? '#fff' : c.warn ? colors.amberFg : colors.textMid,
              }}
            >
              {c.label}
            </Text>
            {c.count !== undefined ? (
              <Text style={{ fontSize: 11.5, fontFamily: mono, color: on ? '#d4d4d8' : colors.textFaint }}>
                {c.count}
              </Text>
            ) : null}
          </Pressable>
        )
      })}
    </ScrollView>
  )
}

export function PrimaryButton({
  label,
  onPress,
  disabled,
  style,
}: {
  label: string
  onPress: () => void
  disabled?: boolean
  style?: ViewStyle
}) {
  return (
    <Pressable
      onPress={onPress}
      disabled={disabled}
      style={[styles.primaryBtn, disabled && { opacity: 0.4 }, style]}
    >
      <Text style={{ color: '#fff', fontSize: 13, fontWeight: '600' }}>{label}</Text>
    </Pressable>
  )
}

export function GhostButton({
  label,
  onPress,
  color = colors.textMid,
  style,
}: {
  label: string
  onPress: () => void
  color?: string
  style?: ViewStyle
}) {
  return (
    <Pressable onPress={onPress} style={[styles.ghostBtn, style]}>
      <Text style={{ color, fontSize: 13, fontWeight: '600' }}>{label}</Text>
    </Pressable>
  )
}

export function EmptyState({ text }: { text: string }) {
  return (
    <View style={{ padding: 40, alignItems: 'center' }}>
      <Text style={{ color: colors.textFaint, fontSize: 14 }}>{text}</Text>
    </View>
  )
}

const styles = StyleSheet.create({
  backBtn: {
    width: 44,
    height: 44,
    marginLeft: -12,
    alignItems: 'center',
    justifyContent: 'center',
  },
  card: {
    backgroundColor: colors.card,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: radius.xl,
    padding: 14,
  },
  sectionRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    marginHorizontal: 2,
    marginBottom: 10,
    marginTop: 6,
  },
  sectionTitle: { fontSize: 15, fontWeight: '700', color: colors.text },
  chipRow: { gap: 6, paddingVertical: 2 },
  chip: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 7,
    height: 34,
    paddingHorizontal: 13,
    borderWidth: 1,
    borderRadius: radius.chip,
  },
  primaryBtn: {
    height: 44,
    backgroundColor: colors.ink,
    borderRadius: 10,
    alignItems: 'center',
    justifyContent: 'center',
  },
  ghostBtn: {
    height: 44,
    backgroundColor: colors.card,
    borderWidth: 1,
    borderColor: '#d4d4d1',
    borderRadius: 10,
    alignItems: 'center',
    justifyContent: 'center',
  },
})
