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
  GithubRepo,
  Message,
  Project,
  Question,
  Repo,
  RocketEvent,
  Session,
  Settings,
  SystemCleanupResult,
  SystemInfo,
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

/**
 * `GET /v1/system` (internal/api/system.go): daemon status, message queue
 * depth, reconciled tmux sessions/worktrees (with orphan/state info) and a
 * tail of rocketd.log. Polled every 5s for the System screen.
 */
export function useSystem(): UseQueryResult<SystemInfo> {
  return useQuery({
    queryKey: ['system'],
    queryFn: () => api.get<SystemInfo>('/v1/system'),
    refetchInterval: 5000,
  })
}

/**
 * `GET /v1/settings` (contract, phase 4): secrets masked. Used to decide
 * whether the GitHub tab shows the repo list or the "Connect GitHub"
 * placeholder.
 */
export function useSettings(): UseQueryResult<Settings> {
  return useQuery({
    queryKey: ['settings'],
    queryFn: () => api.get<Settings>('/v1/settings'),
    retry: false,
  })
}

/**
 * `GET /v1/github/repos?q=` (contract, phase 4). Only enabled when `enabled`
 * is true (i.e. the GitHub tab is active) — the daemon 404s / returns
 * `github_token_missing` when no token is configured, which callers should
 * treat as "show the Connect GitHub placeholder", not a hard error.
 */
export function useGithubRepos(q: string, enabled: boolean): UseQueryResult<GithubRepo[]> {
  return useQuery({
    queryKey: ['github-repos', q],
    queryFn: () => api.get<GithubRepo[]>(`/v1/github/repos?q=${encodeURIComponent(q)}`),
    enabled,
    retry: false,
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

export function useSystemCleanup(): UseMutationResult<SystemCleanupResult, Error, void> {
  return useMutation({
    mutationFn: () => api.post<SystemCleanupResult>('/v1/system/cleanup'),
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

/** `POST /v1/repos`: `{path}` for a local checkout, or `{github:"owner/name"}` to clone. */
export function useRegisterRepo(): UseMutationResult<
  Repo,
  Error,
  { path?: string; github?: string; id?: string }
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (payload) => api.post<Repo>('/v1/repos', payload),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['repos'] })
    },
  })
}

/** `POST /v1/projects`: `{id?, name, main, linked?}` — main/linked are repo ids. */
export function useCreateProject(): UseMutationResult<
  Project,
  Error,
  { id?: string; name: string; main: string; linked?: string[] }
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (payload) => api.post<Project>('/v1/projects', payload),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projects'] })
    },
  })
}

/** `PUT /v1/settings` (contract, phase 4): `{github_token}`. */
export function useUpdateSettings(): UseMutationResult<Settings, Error, { github_token: string }> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (payload) => api.put<Settings>('/v1/settings', payload),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['settings'] })
      queryClient.invalidateQueries({ queryKey: ['github-repos'] })
    },
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
