import { Tabs } from 'expo-router'
import { View } from 'react-native'
import { useAgents } from '../../src/api/queries'
import { awaitingUser } from '../../src/lib/agents'
import { colors } from '../../src/theme'

// Tab icons rebuilt from the mockup SVGs with plain Views (no svg dep).

function ProjectsIcon({ color }: { color: import("react-native").ColorValue }) {
  return (
    <View style={{ width: 20, height: 20, flexDirection: 'row', flexWrap: 'wrap', gap: 2.5 }}>
      {[0, 1, 2, 3].map((i) => (
        <View key={i} style={{ width: 8.5, height: 8.5, borderRadius: 2.5, backgroundColor: color }} />
      ))}
    </View>
  )
}

function KanbanIcon({ color }: { color: import("react-native").ColorValue }) {
  return (
    <View style={{ width: 20, height: 20, flexDirection: 'row', gap: 2.5 }}>
      <View style={{ width: 5, height: 18, borderRadius: 2.5, backgroundColor: color }} />
      <View style={{ width: 5, height: 12, borderRadius: 2.5, backgroundColor: color }} />
      <View style={{ width: 5, height: 15, borderRadius: 2.5, backgroundColor: color }} />
    </View>
  )
}

function SystemIcon({ color }: { color: import("react-native").ColorValue }) {
  return (
    <View
      style={{
        width: 19,
        height: 19,
        borderRadius: 10,
        borderWidth: 1.6,
        borderColor: color,
        opacity: 0.9,
        alignItems: 'center',
        justifyContent: 'center',
      }}
    >
      <View style={{ width: 8, height: 8, borderRadius: 4, backgroundColor: color }} />
    </View>
  )
}

function SettingsIcon({ color }: { color: import("react-native").ColorValue }) {
  return (
    <View style={{ width: 20, height: 18, justifyContent: 'space-between', paddingVertical: 1 }}>
      {[12, 3, 13].map((knob, i) => (
        <View key={i} style={{ height: 4, justifyContent: 'center' }}>
          <View style={{ height: 1.8, borderRadius: 1, backgroundColor: color }} />
          <View
            style={{
              position: 'absolute',
              left: knob,
              width: 5,
              height: 5,
              borderRadius: 2.5,
              backgroundColor: colors.card,
              borderWidth: 1.6,
              borderColor: color,
            }}
          />
        </View>
      ))}
    </View>
  )
}

/** A milestone: a flag on a pole — work an agent plants its name on. */
function MilestonesIcon({ color }: { color: import('react-native').ColorValue }) {
  return (
    <View style={{ width: 20, height: 20, flexDirection: 'row', gap: 2 }}>
      <View style={{ width: 1.8, height: 18, borderRadius: 1, backgroundColor: color }} />
      <View
        style={{
          width: 12,
          height: 9,
          marginTop: 1,
          borderWidth: 1.7,
          borderColor: color,
          borderRadius: 2,
        }}
      />
    </View>
  )
}

/** An agent: a head over a body, echoing the agent cards' avatar. */
function AgentsIcon({ color }: { color: import('react-native').ColorValue }) {
  return (
    <View style={{ width: 20, height: 20, alignItems: 'center', justifyContent: 'center' }}>
      <View style={{ width: 7.5, height: 7.5, borderRadius: 4, borderWidth: 1.7, borderColor: color }} />
      <View
        style={{
          width: 15,
          height: 8,
          marginTop: 2,
          borderTopLeftRadius: 7.5,
          borderTopRightRadius: 7.5,
          borderWidth: 1.7,
          borderBottomWidth: 0,
          borderColor: color,
        }}
      />
    </View>
  )
}

export default function TabsLayout() {
  // Agents that owe the user nothing still matter, but a thread waiting on the
  // user is the one thing worth a badge — same rule as task questions.
  const agents = useAgents()
  const awaiting = awaitingUser(agents.data ?? [])

  return (
    <Tabs
      screenOptions={{
        headerShown: false,
        tabBarActiveTintColor: colors.ink,
        tabBarInactiveTintColor: colors.textFaint,
        tabBarLabelStyle: { fontSize: 10.5, fontWeight: '600' },
        tabBarStyle: {
          backgroundColor: colors.card,
          borderTopColor: colors.border,
        },
        sceneStyle: { backgroundColor: colors.page },
      }}
    >
      <Tabs.Screen
        name="index"
        options={{ title: 'Projects', tabBarIcon: ({ color }) => <ProjectsIcon color={color} /> }}
      />
      <Tabs.Screen
        name="kanban"
        options={{ title: 'Kanban', tabBarIcon: ({ color }) => <KanbanIcon color={color} /> }}
      />
      <Tabs.Screen
        name="milestones"
        options={{ title: 'Milestones', tabBarIcon: ({ color }) => <MilestonesIcon color={color} /> }}
      />
      <Tabs.Screen
        name="agents"
        options={{
          title: 'Agents',
          tabBarIcon: ({ color }) => <AgentsIcon color={color} />,
          ...(awaiting > 0 ? { tabBarBadge: awaiting } : {}),
        }}
      />
      <Tabs.Screen
        name="system"
        options={{ title: 'System', tabBarIcon: ({ color }) => <SystemIcon color={color} /> }}
      />
      <Tabs.Screen
        name="settings"
        options={{ title: 'Settings', tabBarIcon: ({ color }) => <SettingsIcon color={color} /> }}
      />
    </Tabs>
  )
}
