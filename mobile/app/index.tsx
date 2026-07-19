import { Redirect } from 'expo-router'
import { useServers } from '../src/servers/ServerContext'

// Entry: with an active server go straight to the dashboard, otherwise
// land on the server picker.
export default function Index() {
  const { active, loaded } = useServers()
  if (!loaded) return null
  return <Redirect href={active ? '/(tabs)' : '/servers'} />
}
