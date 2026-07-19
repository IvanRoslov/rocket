import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Stack } from 'expo-router'
import { StatusBar } from 'expo-status-bar'
import { useState } from 'react'
import { ServerProvider } from '../src/servers/ServerContext'
import { colors } from '../src/theme'

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
        <StatusBar style="dark" />
        <Stack
          screenOptions={{
            headerShown: false,
            contentStyle: { backgroundColor: colors.page },
          }}
        />
      </ServerProvider>
    </QueryClientProvider>
  )
}
