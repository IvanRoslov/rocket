// react-query hooks over the `api` client, plus `wireInvalidation`, which
// maps live SSE event types (see sse.ts) onto the query keys they should
// invalidate.

import {
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
  type UseMutationResult,
  type UseQueryResult,
} from '@tanstack/react-query'
import { api } from './api'
import type {
  Message,
  Project,
  Question,
  Repo,
  RocketEvent,
  Session,
  Task,
  TaskDetail,
  TaskDoc,
  TaskLogEntry,
} from './types'

export interface SessionFilter {
  project?: string
  kind?: string
  state?: string
  feature?: string
}

function sessionsQueryString(filter?: SessionFilter): string {
  if (!filter) return ''
  const params = new URLSearchParams()
  if (filter.project) params.set('project', filter.project)
  if (filter.kind) params.set('kind', filter.kind)
  if (filter.state) params.set('state', filter.state)
  if (filter.feature) params.set('feature', filter.feature)
  const qs = params.toString()
  return qs ? `?${qs}` : ''
}

// ---------------------------------------------------------------------------
// Queries
// ---------------------------------------------------------------------------

export function useProjects(): UseQueryResult<Project[]> {
  return useQuery({
    queryKey: ['projects'],
    queryFn: () => api.get<Project[]>('/v1/projects'),
  })
}

export function useSessions(filter?: SessionFilter): UseQueryResult<Session[]> {
  return useQuery({
    queryKey: ['sessions', filter ?? {}],
    queryFn: () => api.get<Session[]>(`/v1/sessions${sessionsQueryString(filter)}`),
  })
}

export function useRepos(): UseQueryResult<Repo[]> {
  return useQuery({
    queryKey: ['repos'],
    queryFn: () => api.get<Repo[]>('/v1/repos'),
  })
}

export function useMessages(sessionId: string | undefined): UseQueryResult<Message[]> {
  return useQuery({
    queryKey: ['messages', sessionId],
    queryFn: async () => {
      const res = await api.get<{ messages: Message[] }>(
        `/v1/messages?session=${encodeURIComponent(sessionId ?? '')}`,
      )
      return res.messages
    },
    enabled: sessionId !== undefined,
  })
}

/** Contract type (phase 3): task board grouped for the kanban screen. */
export interface TaskBoard {
  backlog: Task[]
  in_progress: Task[]
  review: Task[]
  done: Task[]
}

/**
 * Contract type (phase 3): the raw `{columns:{...}}` shape returned by
 * `GET /v1/tasks?project=&board=true` (docs/03-daemon-api.md). This is the
 * ONE adapter over that response — reconcile it here at phase-3 integration
 * if the real shape differs.
 */
interface TaskBoardResponse {
  columns: {
    backlog: Task[]
    in_progress: Task[]
    review: Task[]
    done: Task[]
    cancelled: Task[]
  }
}

export function useTasksBoard(projectId: string | undefined): UseQueryResult<TaskBoard> {
  return useQuery({
    queryKey: ['tasks', projectId],
    queryFn: async () => {
      const res = await api.get<TaskBoardResponse>(
        `/v1/tasks?project=${encodeURIComponent(projectId ?? '')}&board=true`,
      )
      const { backlog, in_progress, review, done } = res.columns
      return { backlog, in_progress, review, done }
    },
    enabled: projectId !== undefined,
  })
}

export function useTask(id: number | undefined): UseQueryResult<TaskDetail> {
  return useQuery({
    queryKey: ['task', id],
    queryFn: () => api.get<TaskDetail>(`/v1/tasks/${id}`),
    enabled: id !== undefined,
  })
}

export function useTaskDocs(id: number | undefined): UseQueryResult<TaskDoc[]> {
  return useQuery({
    queryKey: ['task', id, 'docs'],
    queryFn: () => api.get<TaskDoc[]>(`/v1/tasks/${id}/docs`),
    enabled: id !== undefined,
  })
}

export function useTaskLog(id: number | undefined): UseQueryResult<TaskLogEntry[]> {
  return useQuery({
    queryKey: ['task', id, 'log'],
    queryFn: () => api.get<TaskLogEntry[]>(`/v1/tasks/${id}/log`),
    enabled: id !== undefined,
  })
}

export function useTaskQuestions(id: number | undefined): UseQueryResult<Question[]> {
  return useQuery({
    queryKey: ['task', id, 'questions'],
    queryFn: () => api.get<Question[]>(`/v1/tasks/${id}/questions`),
    enabled: id !== undefined,
  })
}

export interface SystemHealth {
  status: string
  version: string
  uptime: number
}

export function useSystem(): UseQueryResult<SystemHealth> {
  return useQuery({
    queryKey: ['system'],
    queryFn: () => api.get<SystemHealth>('/v1/health'),
  })
}

// ---------------------------------------------------------------------------
// Mutations
// ---------------------------------------------------------------------------

export function useSendMessage(): UseMutationResult<
  { id: number; status: string },
  Error,
  { to: string; body: string; from?: string }
> {
  return useMutation({
    mutationFn: (payload) => api.post('/v1/messages', payload),
  })
}

export function useKillSession(): UseMutationResult<
  void,
  Error,
  { id: string; cleanup?: boolean }
> {
  return useMutation({
    mutationFn: ({ id, cleanup }) =>
      api.post(`/v1/sessions/${id}/kill${cleanup ? '?cleanup=true' : ''}`),
  })
}

export function useUpdateTaskStatus(): UseMutationResult<
  Task,
  Error,
  { id: number; status: Task['status'] }
> {
  return useMutation({
    mutationFn: ({ id, status }) => api.patch<Task>(`/v1/tasks/${id}`, { status }),
  })
}

// ---------------------------------------------------------------------------
// Live invalidation
// ---------------------------------------------------------------------------

/**
 * Maps SSE event types onto the query keys they invalidate. Prefix-matched:
 * `session.*` -> sessions + projects (live_sessions counters), `message.*`
 * -> messages, `task.*` -> tasks, `repo.clone_*` -> repos.
 */
export function wireInvalidation(queryClient: QueryClient) {
  return (event: RocketEvent) => {
    if (event.type.startsWith('session.')) {
      queryClient.invalidateQueries({ queryKey: ['sessions'] })
      queryClient.invalidateQueries({ queryKey: ['projects'] })
    } else if (event.type.startsWith('message.')) {
      queryClient.invalidateQueries({ queryKey: ['messages'] })
    } else if (event.type.startsWith('task.')) {
      queryClient.invalidateQueries({ queryKey: ['tasks'] })
      queryClient.invalidateQueries({ queryKey: ['task'] })
    } else if (event.type.startsWith('repo.clone_')) {
      queryClient.invalidateQueries({ queryKey: ['repos'] })
    }
  }
}

export function useQueryClientInvalidation(): (event: RocketEvent) => void {
  const queryClient = useQueryClient()
  return wireInvalidation(queryClient)
}
