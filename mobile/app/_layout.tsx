import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Stack } from 'expo-router'
import { StatusBar } from 'expo-status-bar'
import { useState, type ReactNode } from 'react'
import { ConnectionContext, useEventStream } from '../src/api/events'
import { ServerProvider } from '../src/servers/ServerContext'
import { colors } from '../src/theme'

function EventBridge({ children }: { children: ReactNode }) {
  const { connected } = useEventStream()
  return <ConnectionContext.Provider value={{ sse: connected }}>{children}</ConnectionContext.Provider>
}

export default function RootLayout() {
  const [client] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: { retry: 1, staleTime: 1000 },
        },
      }),
  )

  return (
    <QueryClientProvider client={client}>
      <ServerProvider>
        <EventBridge>
          <StatusBar style="dark" />
          <Stack
            screenOptions={{
              headerShown: false,
              contentStyle: { backgroundColor: colors.page },
            }}
          />
        </EventBridge>
      </ServerProvider>
    </QueryClientProvider>
  )
}
