import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider } from 'react-router-dom'
import { router } from './routes'
import { useEventStream } from './lib/sse'
import { wireInvalidation } from './lib/queries'

const queryClient = new QueryClient()
const invalidate = wireInvalidation(queryClient)

function EventStreamBridge() {
  useEventStream(invalidate)
  return null
}

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <EventStreamBridge />
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}
