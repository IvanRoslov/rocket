import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, renderHook, waitFor } from '@testing-library/react-native'
import { ServerProvider, useServers } from '../servers/ServerContext'
import {
  useCancelTask,
  useCreateQuestion,
  useKillSession,
  useMoveTask,
  useQuestionAnswer,
  useQuestionDismiss,
  useQuestionReply,
  useRestoreSession,
  useSystemCleanup,
} from './queries'

const BASE = 'http://192.168.1.10:4477'

function wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: Infinity },
      mutations: { retry: false, gcTime: Infinity },
    },
  })
  return (
    <QueryClientProvider client={qc}>
      <ServerProvider>{children}</ServerProvider>
    </QueryClientProvider>
  )
}

/** Renders a mutation hook with an active server configured. */
async function setup<T>(useHook: () => T) {
  globalThis.fetch = jest.fn(async () => ({ ok: true, json: async () => ({}) })) as unknown as typeof fetch
  const { result } = await renderHook(
    () => ({ servers: useServers(), hook: useHook() }),
    { wrapper },
  )
  await waitFor(() => expect(result.current.servers.loaded).toBe(true))
  await act(async () => result.current.servers.addServer({ name: 'A', host: '192.168.1.10', port: 4477 }))
  return result
}

function lastCall(): { url: string; init: RequestInit } {
  const calls = (fetch as jest.Mock).mock.calls
  const [url, init] = calls[calls.length - 1]
  return { url, init }
}

describe('mutation hooks', () => {
  afterEach(() => jest.restoreAllMocks())

  it('kill session without cleanup', async () => {
    const r = await setup(useKillSession)
    await act(async () => {
      await r.current.hook.mutateAsync({ id: 's1' })
    })
    expect(lastCall().url).toBe(`${BASE}/v1/sessions/s1/kill`)
    expect(lastCall().init.method).toBe('POST')
  })

  it('kill session with cleanup', async () => {
    const r = await setup(useKillSession)
    await act(async () => {
      await r.current.hook.mutateAsync({ id: 's1', cleanup: true })
    })
    expect(lastCall().url).toBe(`${BASE}/v1/sessions/s1/kill?cleanup=true`)
  })

  it('restore session', async () => {
    const r = await setup(useRestoreSession)
    await act(async () => {
      await r.current.hook.mutateAsync('s2')
    })
    expect(lastCall().url).toBe(`${BASE}/v1/sessions/s2/restore`)
  })

  it('cancel task', async () => {
    const r = await setup(useCancelTask)
    await act(async () => {
      await r.current.hook.mutateAsync(12)
    })
    expect(lastCall().url).toBe(`${BASE}/v1/tasks/12/cancel`)
  })

  it('move task PATCHes status', async () => {
    const r = await setup(useMoveTask)
    await act(async () => {
      await r.current.hook.mutateAsync({ id: 12, status: 'review' })
    })
    const { url, init } = lastCall()
    expect(url).toBe(`${BASE}/v1/tasks/12`)
    expect(init.method).toBe('PATCH')
    expect(JSON.parse(init.body as string)).toEqual({ status: 'review' })
  })

  it('system cleanup', async () => {
    const r = await setup(useSystemCleanup)
    await act(async () => {
      await r.current.hook.mutateAsync()
    })
    expect(lastCall().url).toBe(`${BASE}/v1/system/cleanup`)
  })

  it('dismiss question posts dismiss flag', async () => {
    const r = await setup(useQuestionDismiss)
    await act(async () => {
      await r.current.hook.mutateAsync(7)
    })
    const { url, init } = lastCall()
    expect(url).toBe(`${BASE}/v1/questions/7/answer`)
    expect(JSON.parse(init.body as string)).toEqual({ dismiss: true })
  })

  it('reply sends the picked addressees', async () => {
    const r = await setup(useQuestionReply)
    await act(async () => {
      await r.current.hook.mutateAsync({ id: 7, body: 'here you go', to: ['cto'] })
    })
    const { url, init } = lastCall()
    expect(url).toBe(`${BASE}/v1/questions/7/reply`)
    expect(JSON.parse(init.body as string)).toEqual({ body: 'here you go', to: ['cto'] })
  })

  it('reply omits the to key when nobody is picked', async () => {
    const r = await setup(useQuestionReply)
    await act(async () => {
      await r.current.hook.mutateAsync({ id: 7, body: 'here you go' })
    })
    expect(JSON.parse(lastCall().init.body as string)).toEqual({ body: 'here you go' })
  })

  it('answer sends the picked addressees', async () => {
    const r = await setup(useQuestionAnswer)
    await act(async () => {
      await r.current.hook.mutateAsync({ id: 7, body: 'final', to: ['cto', 'reply-answer-orch'] })
    })
    const { url, init } = lastCall()
    expect(url).toBe(`${BASE}/v1/questions/7/answer`)
    expect(JSON.parse(init.body as string)).toEqual({
      body: 'final',
      to: ['cto', 'reply-answer-orch'],
    })
  })

  it('creating a thread sends the picked addressees', async () => {
    const r = await setup(useCreateQuestion)
    await act(async () => {
      await r.current.hook.mutateAsync({ taskId: 12, body: 'who owns this?', to: ['cto'] })
    })
    const { url, init } = lastCall()
    expect(url).toBe(`${BASE}/v1/tasks/12/questions`)
    expect(JSON.parse(init.body as string)).toEqual({ body: 'who owns this?', to: ['cto'] })
  })
})
