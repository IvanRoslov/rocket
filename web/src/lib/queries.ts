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
  GithubIssue,
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
  TaskStatus,
} from './types'

export interface SessionFilter {
  project?: string
  kind?: string
  state?: string
  feature?: string
  /** Include non-live sessions (e.g. `done` workers whose PR merged) instead
   * of the default spawning/running-only view. Mirrors the daemon's
   * `?all=true` on `GET /v1/sessions`. */
  all?: boolean
}

function sessionsQueryString(filter?: SessionFilter): string {
  if (!filter) return ''
  const params = new URLSearchParams()
  if (filter.project) params.set('project', filter.project)
  if (filter.kind) params.set('kind', filter.kind)
  if (filter.state) params.set('state', filter.state)
  if (filter.feature) params.set('feature', filter.feature)
  if (filter.all) params.set('all', 'true')
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

/** Single session by id (`GET /v1/sessions/{id}`) — used by the dedicated
 * full-window terminal page, which only has the session id in its URL. */
export function useSession(id?: string): UseQueryResult<Session> {
  return useQuery({
    queryKey: ['session', id],
    queryFn: () => api.get<Session>(`/v1/sessions/${id}`),
    enabled: id !== undefined && id !== '',
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

/** Task board grouped for the kanban screen: `GET /v1/tasks?board=true`. */
export interface TaskBoard {
  backlog: Task[]
  in_progress: Task[]
  review: Task[]
  done: Task[]
  cancelled: Task[]
}

interface TaskBoardResponse {
  board: TaskBoard
}

export function useTasksBoard(projectId: string | undefined): UseQueryResult<TaskBoard> {
  return useQuery({
    queryKey: ['tasks', projectId, 'board'],
    queryFn: async () => {
      const res = await api.get<TaskBoardResponse>(
        `/v1/tasks?project=${encodeURIComponent(projectId ?? '')}&board=true`,
      )
      return res.board
    },
    enabled: projectId !== undefined,
  })
}

export interface TaskFilter {
  project?: string
  status?: TaskStatus
  /** Omit for root-only, 'all' for every task, or a parent task id for its children. */
  parent?: number | 'all'
}

function tasksQueryString(filter?: TaskFilter): string {
  if (!filter) return ''
  const params = new URLSearchParams()
  if (filter.project) params.set('project', filter.project)
  if (filter.status) params.set('status', filter.status)
  if (filter.parent !== undefined) params.set('parent', String(filter.parent))
  const qs = params.toString()
  return qs ? `?${qs}` : ''
}

export function useTasks(filter?: TaskFilter): UseQueryResult<Task[]> {
  return useQuery({
    queryKey: ['tasks', filter ?? {}],
    queryFn: async () => {
      const res = await api.get<{ tasks: Task[] }>(`/v1/tasks${tasksQueryString(filter)}`)
      return res.tasks
    },
  })
}

/**
 * Per-project task list, used by ProjectsScreen to derive status counts
 * client-side (the real `GET /v1/projects` has no task counters — see
 * .superpowers/sdd/phase3-contract.md). Root tasks only (default `parent`).
 */
export function useProjectTasks(projectId: string): UseQueryResult<Task[]> {
  return useQuery({
    queryKey: ['tasks', projectId, 'list'],
    queryFn: async () => {
      const res = await api.get<{ tasks: Task[] }>(`/v1/tasks?project=${encodeURIComponent(projectId)}`)
      return res.tasks
    },
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
    queryFn: async () => {
      const res = await api.get<{ docs: TaskDoc[] }>(`/v1/tasks/${id}/docs`)
      return res.docs
    },
    enabled: id !== undefined,
  })
}

export function useTaskLog(id: number | undefined): UseQueryResult<TaskLogEntry[]> {
  return useQuery({
    queryKey: ['task', id, 'log'],
    queryFn: async () => {
      const res = await api.get<{ log: TaskLogEntry[] }>(`/v1/tasks/${id}/log`)
      return res.log
    },
    enabled: id !== undefined,
  })
}

export function useTaskQuestions(id: number | undefined): UseQueryResult<Question[]> {
  return useQuery({
    queryKey: ['task', id, 'questions'],
    queryFn: async () => {
      const res = await api.get<{ questions: Question[] }>(`/v1/tasks/${id}/questions`)
      return res.questions
    },
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
 * `GET /v1/settings` (internal/api/settings.go): `{github_token}`, masked
 * (or "" when unset). Never carries `login` — see the `Settings` type doc.
 */
export function useSettings(): UseQueryResult<Settings> {
  return useQuery({
    queryKey: ['settings'],
    queryFn: () => api.get<Settings>('/v1/settings'),
    retry: false,
  })
}

/**
 * `GET /v1/github/repos?q=` (internal/api/github_catalog.go), unwrapped from
 * its `{"repos":[...]}` envelope. Only enabled when `enabled` is true (i.e.
 * the GitHub tab is active). With no token configured the daemon responds
 * `400 {code:"no_token"}` (NOT 404) — callers should branch on
 * `error.code === 'no_token'` to show the "Connect GitHub" placeholder,
 * treating any other error as a real failure.
 */
export function useGithubRepos(q: string, enabled: boolean): UseQueryResult<GithubRepo[]> {
  return useQuery({
    queryKey: ['github-repos', q],
    queryFn: async () => {
      const res = await api.get<{ repos: GithubRepo[] }>(`/v1/github/repos?q=${encodeURIComponent(q)}`)
      return res.repos
    },
    enabled,
    retry: false,
  })
}

/**
 * `GET /v1/github/issues?repo_id=&state=` (internal/api/github_issues.go),
 * unwrapped from its `{"issues":[...]}` envelope. Used by NewTaskModal's
 * "from GitHub issue" mode — `repoId` is a registered repo id (the daemon
 * resolves owner/name from that repo's git remote origin), not an
 * `owner/name` string. `state` defaults to `"open"`. Errors mirror
 * `useGithubRepos`: branch on `error.code` — `no_token` ("Connect GitHub"),
 * `not_a_github_repo` (repo has no GitHub origin), `github_unreachable`
 * (retryable). Only enabled when `enabled` is true and `repoId` is set.
 */
export function useGithubIssues(
  repoId: string | undefined,
  state: 'open' | 'closed' | 'all' = 'open',
  enabled: boolean,
): UseQueryResult<GithubIssue[]> {
  return useQuery({
    queryKey: ['github-issues', repoId, state],
    queryFn: async () => {
      const res = await api.get<{ issues: GithubIssue[] }>(
        `/v1/github/issues?repo_id=${encodeURIComponent(repoId ?? '')}&state=${state}`,
      )
      return res.issues
    },
    enabled: enabled && repoId !== undefined,
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
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (payload) => api.post('/v1/messages', payload),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['messages'] })
    },
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

/**
 * `POST /v1/sessions/{id}/restore` (phase 4): re-spawns an `errored` worker
 * session on its existing branch/worktree. Not in the phase-3 contract doc
 * (which predates it) but referenced by the Task screen brief for the
 * SessionRail's "restore" action on errored workers.
 */
export function useRestoreSession(): UseMutationResult<Session, Error, string> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id) => api.post<Session>(`/v1/sessions/${id}/restore`),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['sessions'] })
    },
  })
}

export function useSystemCleanup(): UseMutationResult<SystemCleanupResult, Error, void> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: () => api.post<SystemCleanupResult>('/v1/system/cleanup'),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['system'] })
    },
  })
}

/** `POST /v1/tasks`: `{title, description?, project, parent_id?}` -> bare taskResponse (201). */
export function useCreateTask(): UseMutationResult<
  Task,
  Error,
  { title: string; description?: string; project: string; parent_id?: number }
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (payload) => api.post<Task>('/v1/tasks', payload),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tasks'] })
    },
  })
}

/**
 * `PATCH /v1/tasks/{id}` `{status}` -> bare taskResponse (200). Moving a
 * task to `cancelled` via PATCH is rejected by the daemon (400 `use_cancel`)
 * — redirect that case to `POST /v1/tasks/{id}/cancel` instead.
 */
export function useMoveTask(): UseMutationResult<Task, Error, { id: number; status: TaskStatus }> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, status }) => {
      if (status === 'cancelled') {
        return api.post<Task>(`/v1/tasks/${id}/cancel`)
      }
      return api.patch<Task>(`/v1/tasks/${id}`, { status })
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tasks'] })
      queryClient.invalidateQueries({ queryKey: ['task'] })
    },
  })
}

/**
 * `PATCH /v1/tasks/{id}` `{title?, description?}` -> bare taskResponse (200).
 * Used by the Overview tab's inline title/description editor. The daemon
 * does not itself reject an empty title on this path (see
 * internal/api/tasks.go handlePatchTask) — callers must validate that
 * client-side before calling mutate.
 */
export function useUpdateTask(): UseMutationResult<
  Task,
  Error,
  { id: number; title?: string; description?: string }
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, ...body }) => api.patch<Task>(`/v1/tasks/${id}`, body),
    onSuccess: (_data, { id }) => {
      queryClient.invalidateQueries({ queryKey: ['task', id] })
      queryClient.invalidateQueries({ queryKey: ['tasks'] })
    },
  })
}

/** `POST /v1/tasks/{id}/cancel` (no body) -> bare taskResponse (200); cascades to kill sessions. */
export function useCancelTask(): UseMutationResult<Task, Error, number> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id) => api.post<Task>(`/v1/tasks/${id}/cancel`),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tasks'] })
      queryClient.invalidateQueries({ queryKey: ['task'] })
      queryClient.invalidateQueries({ queryKey: ['sessions'] })
    },
  })
}

/** `POST /v1/tasks/{id}/start` `{agent?}` -> `{task_id,feature_slug,session_id}` (201). Root tasks only. */
export function useStartTask(): UseMutationResult<
  { task_id: number; feature_slug: string; session_id: string },
  Error,
  { id: number; agent?: string }
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, agent }) =>
      api.post<{ task_id: number; feature_slug: string; session_id: string }>(
        `/v1/tasks/${id}/start`,
        agent ? { agent } : undefined,
      ),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tasks'] })
      queryClient.invalidateQueries({ queryKey: ['task'] })
      queryClient.invalidateQueries({ queryKey: ['sessions'] })
    },
  })
}

/** `POST /v1/questions/{id}/reply` `{body}` -> bare questionResponse (201). Open questions only. */
export function useReplyQuestion(): UseMutationResult<
  Question,
  Error,
  { id: number; body: string; taskId: number }
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, body }) => api.post<Question>(`/v1/questions/${id}/reply`, { body }),
    onSuccess: (_data, { taskId }) => {
      queryClient.invalidateQueries({ queryKey: ['task', taskId, 'questions'] })
      queryClient.invalidateQueries({ queryKey: ['task', taskId] })
    },
  })
}

/**
 * `POST /v1/questions/{id}/answer` `{body}` | `{dismiss:true}` -> bare
 * questionResponse (200). User only.
 */
export function useAnswerQuestion(): UseMutationResult<
  Question,
  Error,
  { id: number; taskId: number } & ({ body: string; dismiss?: never } | { dismiss: true; body?: never })
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, body, dismiss }) =>
      api.post<Question>(`/v1/questions/${id}/answer`, dismiss ? { dismiss: true } : { body }),
    onSuccess: (_data, { taskId }) => {
      queryClient.invalidateQueries({ queryKey: ['task', taskId, 'questions'] })
      queryClient.invalidateQueries({ queryKey: ['task', taskId] })
    },
  })
}

/**
 * `POST /v1/tasks/{id}/questions` `{body, context?}` -> bare questionResponse
 * (201). Opens a question thread FROM the dashboard user TO the task's
 * orchestrator (no `X-Rocket-Session` header — the api client never sends
 * one, so the daemon treats the caller as the human). The response carries
 * `asked_by: ""` and `whose_turn: "orchestrator"`.
 */
export function useAskOrchestrator(
  taskId: number | undefined,
): UseMutationResult<Question, Error, { body: string; context?: string }> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (payload) => api.post<Question>(`/v1/tasks/${taskId}/questions`, payload),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['task', taskId, 'questions'] })
      queryClient.invalidateQueries({ queryKey: ['task', taskId] })
    },
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

/**
 * `PUT /v1/settings` (internal/api/settings.go): `{github_token}` -> the
 * masked token plus `login` (present only when a non-empty token was
 * accepted and validated against GitHub).
 */
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

/** `PATCH /v1/repos/{id}`: env/symlinks/post_create. Used by the repo Edit modal (Settings screen). */
export function useUpdateRepo(): UseMutationResult<
  Repo,
  Error,
  { id: string; env?: Record<string, string>; symlinks?: string[]; post_create?: string[] }
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, ...body }) => api.patch<Repo>(`/v1/repos/${id}`, body),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['repos'] })
    },
  })
}

/**
 * `DELETE /v1/repos/{id}`. The daemon rejects this if the repo is still
 * referenced by a project's `main`/`linked` — callers should also disable
 * the Remove button client-side using the same check (Settings > Repositories).
 */
export function useDeleteRepo(): UseMutationResult<void, Error, string> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id) => api.del<void>(`/v1/repos/${id}`),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['repos'] })
    },
  })
}

/** `PATCH /v1/projects/{id}`: name/main/linked. Used by the Settings > Project section. */
export function useUpdateProject(): UseMutationResult<
  Project,
  Error,
  { id: string; name?: string; main?: string; linked?: string[] }
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, ...body }) => api.patch<Project>(`/v1/projects/${id}`, body),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projects'] })
    },
  })
}

/**
 * `DELETE /v1/projects/{id}`. The daemon blocks this only when
 * `live_sessions>0` (409 `project_busy`) — it does not check tasks.
 */
export function useDeleteProject(): UseMutationResult<void, Error, string> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id) => api.del<void>(`/v1/projects/${id}`),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projects'] })
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
 *
 * `session.quiz_asked` / `session.quiz_resolved` / `session.quiz_answer_
 * unconfirmed` (docs/13-chat.md «Квизы») fall under the `session.*` prefix
 * above, so a react-query-backed session list (e.g. the SessionRail "quiz"
 * badge) refreshes automatically. The chat feed itself is NOT react-query
 * backed (see useSessionChat.ts) — it listens to these same three types
 * directly to refetch `pending_quiz` promptly.
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
    } else if (event.type === 'orchestrator.heartbeat_sent') {
      // High-frequency event; keep invalidation minimal — only the task
      // detail view (which shows session/heartbeat state) needs to refresh.
      queryClient.invalidateQueries({ queryKey: ['task'] })
    } else if (event.type.startsWith('repo.clone_')) {
      queryClient.invalidateQueries({ queryKey: ['repos'] })
    } else if (event.type.startsWith('pr.')) {
      // PR state changes (phase 4): re-fetch the sessions carrying pr_*
      // fields plus the task/board views that surface PR badges.
      queryClient.invalidateQueries({ queryKey: ['sessions'] })
      queryClient.invalidateQueries({ queryKey: ['tasks'] })
      queryClient.invalidateQueries({ queryKey: ['task'] })
    }
  }
}

export function useQueryClientInvalidation(): (event: RocketEvent) => void {
  const queryClient = useQueryClient()
  return wireInvalidation(queryClient)
}
