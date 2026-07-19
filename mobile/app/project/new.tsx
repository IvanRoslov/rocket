import { router } from 'expo-router'
import { useState } from 'react'
import { Pressable, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native'
import { SafeAreaView } from 'react-native-safe-area-context'
import { useCreateProject } from '../../src/api/queries'
import { RepoPicker } from '../../src/components/RepoPicker'
import { BackButton, Card, GhostButton, MonoText, PrimaryButton } from '../../src/components/ui'
import { useServers } from '../../src/servers/ServerContext'
import { colors, radius } from '../../src/theme'

const STEPS = ['Name', 'Main repo', 'Linked repos', 'Review'] as const

export default function NewProjectScreen() {
  const { setActiveProjectId } = useServers()
  const create = useCreateProject()
  const [step, setStep] = useState(0)
  const [name, setName] = useState('')
  const [main, setMain] = useState<string | null>(null)
  const [linked, setLinked] = useState<string[]>([])
  const [picker, setPicker] = useState<'main' | 'linked' | null>(null)
  const [error, setError] = useState<string | null>(null)

  const canNext = [name.trim().length > 0, main !== null, true, !create.isPending][step]

  const submit = () => {
    if (!main) return
    setError(null)
    create.mutate(
      { name: name.trim(), main, linked: linked.length ? linked : undefined },
      {
        onSuccess: (p) => {
          setActiveProjectId(p.id)
          router.replace('/(tabs)/kanban')
        },
        onError: (e) => setError((e as Error).message),
      },
    )
  }

  return (
    <SafeAreaView style={{ flex: 1, backgroundColor: colors.page }} edges={['top', 'bottom']}>
      <View style={styles.header}>
        <BackButton onPress={() => router.back()} />
        <Text style={{ fontSize: 17, fontWeight: '700', letterSpacing: -0.2 }}>New project</Text>
        <View style={{ flex: 1 }} />
        <Text style={{ fontSize: 12, color: colors.textFaint }}>
          {step + 1} / {STEPS.length}
        </Text>
      </View>

      <View style={styles.dots}>
        {STEPS.map((s, i) => (
          <View key={s} style={[styles.dot, i <= step && { backgroundColor: colors.ink }]} />
        ))}
      </View>

      <ScrollView contentContainerStyle={{ padding: 16, gap: 14 }}>
        <Text style={styles.stepTitle}>{STEPS[step]}</Text>

        {step === 0 ? (
          <Card style={{ padding: 16 }}>
            <Text style={styles.label}>Project name</Text>
            <TextInput
              style={styles.input}
              placeholder="Billing"
              placeholderTextColor={colors.textFaint}
              value={name}
              onChangeText={setName}
              autoFocus
            />
            <Text style={styles.hint}>A project is one product: a main repo plus linked repos where workers run.</Text>
          </Card>
        ) : null}

        {step === 1 ? (
          <Card style={{ padding: 16 }}>
            <Text style={styles.label}>Main repository</Text>
            {main ? (
              <View style={styles.repoChipRow}>
                <View style={[styles.repoChip, { backgroundColor: colors.indigoBg }]}>
                  <MonoText style={{ fontSize: 13, color: colors.indigoFg }}>⌂ {main}</MonoText>
                </View>
                <Pressable onPress={() => setMain(null)}>
                  <Text style={{ fontSize: 13, color: colors.textDim }}>change</Text>
                </Pressable>
              </View>
            ) : (
              <GhostButton label="＋ Choose repo" onPress={() => setPicker('main')} />
            )}
            <Text style={styles.hint}>The orchestrator runs in the main repo; its docs live there too.</Text>
          </Card>
        ) : null}

        {step === 2 ? (
          <Card style={{ padding: 16 }}>
            <Text style={styles.label}>Linked repositories (optional)</Text>
            <View style={[styles.repoChipRow, { flexWrap: 'wrap', marginBottom: 10 }]}>
              {linked.map((id) => (
                <Pressable key={id} onPress={() => setLinked((l) => l.filter((x) => x !== id))}>
                  <View style={styles.repoChip}>
                    <MonoText style={{ fontSize: 13 }}>
                      {id} <Text style={{ color: colors.textFaint }}>✕</Text>
                    </MonoText>
                  </View>
                </Pressable>
              ))}
            </View>
            <GhostButton label="＋ Add linked repo" onPress={() => setPicker('linked')} />
            <Text style={styles.hint}>Workers can also run in linked repos (web, infra, sdk…).</Text>
          </Card>
        ) : null}

        {step === 3 ? (
          <Card style={{ padding: 16, gap: 10 }}>
            <Row k="name" v={name.trim()} />
            <Row k="main" v={main ?? '—'} />
            <Row k="linked" v={linked.length ? linked.join(', ') : '—'} />
            {error ? (
              <View style={styles.errorBox}>
                <Text style={{ fontSize: 12.5, color: colors.redFg }}>{error}</Text>
              </View>
            ) : null}
          </Card>
        ) : null}
      </ScrollView>

      <View style={styles.footer}>
        {step > 0 ? <GhostButton label="Back" onPress={() => setStep((s) => s - 1)} style={{ flex: 1 }} /> : null}
        <PrimaryButton
          label={step < 3 ? 'Continue' : create.isPending ? 'Creating…' : 'Create project'}
          disabled={!canNext}
          onPress={() => (step < 3 ? setStep((s) => s + 1) : submit())}
          style={{ flex: 2 }}
        />
      </View>

      <RepoPicker
        visible={picker !== null}
        title={picker === 'main' ? 'Main repository' : 'Add linked repo'}
        exclude={picker === 'linked' ? [...linked, ...(main ? [main] : [])] : []}
        onClose={() => setPicker(null)}
        onPick={(id) => {
          if (picker === 'main') setMain(id)
          else setLinked((l) => (l.includes(id) ? l : [...l, id]))
        }}
      />
    </SafeAreaView>
  )
}

function Row({ k, v }: { k: string; v: string }) {
  return (
    <View style={{ flexDirection: 'row', justifyContent: 'space-between', gap: 12 }}>
      <Text style={{ fontSize: 13, color: colors.textDim }}>{k}</Text>
      <MonoText style={{ fontSize: 12.5, fontWeight: '600', color: colors.text, flex: 1, textAlign: 'right' }}>
        {v}
      </MonoText>
    </View>
  )
}

const styles = StyleSheet.create({
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 11,
    height: 54,
    paddingHorizontal: 16,
    backgroundColor: colors.card,
    borderBottomWidth: 1,
    borderBottomColor: colors.border,
  },
  dots: { flexDirection: 'row', gap: 6, padding: 12, paddingHorizontal: 16, backgroundColor: colors.card },
  dot: { flex: 1, height: 4, borderRadius: 2, backgroundColor: colors.border },
  stepTitle: { fontSize: 20, fontWeight: '700', letterSpacing: -0.3, color: colors.text },
  label: { fontSize: 12.5, fontWeight: '600', color: colors.text, marginBottom: 8 },
  input: {
    height: 42,
    paddingHorizontal: 12,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: 10,
    fontSize: 14,
    color: colors.text,
    backgroundColor: colors.card,
  },
  hint: { marginTop: 10, fontSize: 12, lineHeight: 17, color: colors.textFaint },
  repoChipRow: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  repoChip: {
    backgroundColor: '#f4f4f2',
    paddingHorizontal: 10,
    paddingVertical: 6,
    borderRadius: radius.md - 1,
  },
  errorBox: {
    backgroundColor: colors.redBgSoft,
    borderWidth: 1,
    borderColor: colors.redBorder,
    borderRadius: radius.md,
    padding: 10,
  },
  footer: {
    flexDirection: 'row',
    gap: 9,
    padding: 16,
    backgroundColor: colors.card,
    borderTopWidth: 1,
    borderTopColor: colors.border,
  },
})
