import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, renderHook, waitFor } from '@testing-library/react-native'
import { ServerProvider, useServers } from '../servers/ServerContext'
import {
  useAgentQuestionAnswer,
  useAgentQuestionDismiss,
  useAgentQuestionReply,
  useAgents,
  useCreateAgentQuestion,
  useSetAgentEnabled,
  useWakeAgent,
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

/** Renders a hook with an active server configured. */
async function setup<T>(useHook: () => T) {
  globalThis.fetch = jest.fn(async () => ({ ok: true, json: async () => [] })) as unknown as typeof fetch
  const { result } = await renderHook(() => ({ servers: useServers(), hook: useHook() }), { wrapper })
  await waitFor(() => expect(result.current.servers.loaded).toBe(true))
  await act(async () => result.current.servers.addServer({ name: 'A', host: '192.168.1.10', port: 4477 }))
  return result
}

function lastCall(): { url: string; init: RequestInit } {
  const calls = (fetch as jest.Mock).mock.calls
  const [url, init] = calls[calls.length - 1]
  return { url, init }
}

describe('agent role hooks', () => {
  afterEach(() => jest.restoreAllMocks())

  it('lists the roles of a project', async () => {
    await setup(() => useAgents('platform'))
    await waitFor(() => expect(lastCall().url).toBe(`${BASE}/v1/agents?project=platform`))
  })

  it('lists all roles when no project is given', async () => {
    await setup(() => useAgents())
    await waitFor(() => expect(lastCall().url).toBe(`${BASE}/v1/agents`))
  })

  it('wakes a role with text', async () => {
    const r = await setup(useWakeAgent)
    await act(async () => {
      await r.current.hook.mutateAsync({ id: 'sre', text: 'ping' })
    })
    const { url, init } = lastCall()
    expect(url).toBe(`${BASE}/v1/agents/sre/wake`)
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body as string)).toEqual({ kind: 'message', text: 'ping' })
  })

  it('disables a role', async () => {
    const r = await setup(useSetAgentEnabled)
    await act(async () => {
      await r.current.hook.mutateAsync({ id: 'sre', enabled: false })
    })
    expect(lastCall().url).toBe(`${BASE}/v1/agents/sre/disable`)
  })

  it('enables a role', async () => {
    const r = await setup(useSetAgentEnabled)
    await act(async () => {
      await r.current.hook.mutateAsync({ id: 'sre', enabled: true })
    })
    expect(lastCall().url).toBe(`${BASE}/v1/agents/sre/enable`)
  })

  it('opens a thread on a role', async () => {
    const r = await setup(useCreateAgentQuestion)
    await act(async () => {
      await r.current.hook.mutateAsync({ roleId: 'sre', body: 'status?' })
    })
    const { url, init } = lastCall()
    expect(url).toBe(`${BASE}/v1/agents/sre/questions`)
    expect(JSON.parse(init.body as string)).toEqual({ body: 'status?' })
  })

  it('replies in a role thread', async () => {
    const r = await setup(useAgentQuestionReply)
    await act(async () => {
      await r.current.hook.mutateAsync({ id: 5, body: 'more' })
    })
    expect(lastCall().url).toBe(`${BASE}/v1/agent-questions/5/reply`)
  })

  it('answers a role thread', async () => {
    const r = await setup(useAgentQuestionAnswer)
    await act(async () => {
      await r.current.hook.mutateAsync({ id: 5, body: 'yes' })
    })
    const { url, init } = lastCall()
    expect(url).toBe(`${BASE}/v1/agent-questions/5/answer`)
    expect(JSON.parse(init.body as string)).toEqual({ body: 'yes' })
  })

  it('dismisses a role thread', async () => {
    const r = await setup(useAgentQuestionDismiss)
    await act(async () => {
      await r.current.hook.mutateAsync(5)
    })
    const { url, init } = lastCall()
    expect(url).toBe(`${BASE}/v1/agent-questions/5/answer`)
    expect(JSON.parse(init.body as string)).toEqual({ dismiss: true })
  })
})
