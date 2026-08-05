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
  Agent,
  AgentDelivery,
  AgentInboxMessage,
  AgentQuestion,
  GithubIssue,
  GithubRepo,
  GlobalQuestion,
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
  ThreadInboxEntry,
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

/** `GET /v1/questions` — all open questions across all projects, for the
 * global Questions page and the AppShell nav counter. */
export function useOpenQuestions(): UseQueryResult<GlobalQuestion[]> {
  return useQuery({
    queryKey: ['questions', 'open'],
    queryFn: async () => {
      const res = await api.get<{ questions: GlobalQuestion[] }>('/v1/questions')
      return res.questions
    },
  })
}

/**
 * `GET /v1/threads` (internal/api/thread_inbox.go): the unified inbox — every
 * thread the caller may read, task and role alike, in one listing. This is the
 * answer to "what is open and on whom", the question that previously required
 * walking every task and every role (task #1023 spec v1 §«Единый инбокс»).
 *
 * A row carries the question only, never the conversation: expanding one
 * fetches the real thread from its per-subject endpoint.
 *
 * `all` includes resolved threads — history, fyi notes included.
 */
export function useThreads(opts?: { all?: boolean }): UseQueryResult<ThreadInboxEntry[]> {
  const all = opts?.all ?? false
  return useQuery({
    queryKey: ['threads', { all }],
    queryFn: async () => {
      const res = await api.get<{ threads: ThreadInboxEntry[] }>(
        all ? '/v1/threads?all=true' : '/v1/threads',
      )
      return res.threads
    },
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
  { id: number; status: string; body: string },
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

/**
 * Attaches the addressee list to a thread payload. `to` decides who must
 * RESPOND (`waiting_on`), never who gets NOTIFIED — every participant but the
 * author is notified regardless. An empty pick must leave the key off the wire
 * entirely: the API reads an absent `to` as "everyone except the author"
 * (waitingOn in internal/api/threads.go).
 */
function withTo<T extends object>(payload: T, to?: string[]): T & { to?: string[] } {
  return to && to.length > 0 ? { ...payload, to } : payload
}

/** `POST /v1/questions/{id}/reply` `{body}` -> bare questionResponse (201). Open questions only. */
export function useReplyQuestion(): UseMutationResult<
  Question,
  Error,
  { id: number; body: string; taskId: number; to?: string[] }
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, body, to }) =>
      api.post<Question>(`/v1/questions/${id}/reply`, withTo({ body }, to)),
    onSuccess: (_data, { taskId }) => {
      queryClient.invalidateQueries({ queryKey: ['task', taskId, 'questions'] })
      queryClient.invalidateQueries({ queryKey: ['task', taskId] })
      queryClient.invalidateQueries({ queryKey: ['questions'] })
      queryClient.invalidateQueries({ queryKey: ['threads'] })
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
  { id: number; taskId: number; to?: string[] } & (
    | { body: string; dismiss?: never; choose?: never }
    | { dismiss: true; body?: never; choose?: never }
    | { choose: number; body?: never; dismiss?: never }
  )
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, body, dismiss, choose, to }) =>
      api.post<Question>(
        `/v1/questions/${id}/answer`,
        // Both a dismiss and a picked option resolve the thread outright, so
        // nobody is left to respond and an addressee list would be
        // meaningless. `choose` is a 1-based index into `options`; the daemon
        // substitutes the option's own text (chooseOptionBody in
        // internal/api/threads.go), so no body travels with it.
        dismiss ? { dismiss: true } : choose ? { choose } : withTo({ body }, to),
      ),
    onSuccess: (_data, { taskId }) => {
      queryClient.invalidateQueries({ queryKey: ['task', taskId, 'questions'] })
      queryClient.invalidateQueries({ queryKey: ['task', taskId] })
      queryClient.invalidateQueries({ queryKey: ['questions'] })
      queryClient.invalidateQueries({ queryKey: ['threads'] })
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
// Agents (docs/10-agents.md «Постоянные агенты»)
// ---------------------------------------------------------------------------

/** `GET /v1/agents[?project=]` — bare array of agents. */
export function useAgents(projectId?: string): UseQueryResult<Agent[]> {
  return useQuery({
    queryKey: ['agents', projectId ?? 'all'],
    queryFn: () =>
      api.get<Agent[]>(`/v1/agents${projectId ? `?project=${encodeURIComponent(projectId)}` : ''}`),
  })
}

/** `GET /v1/agents/{id}` — the same shape the list returns. */
export function useAgent(id?: string): UseQueryResult<Agent> {
  return useQuery({
    queryKey: ['agent', id],
    queryFn: () => api.get<Agent>(`/v1/agents/${id}`),
    enabled: !!id,
  })
}

/** `GET /v1/agents/{id}/inbox[?status=unread|read]` — messages, oldest first. */
export function useAgentInbox(id?: string, status?: string): UseQueryResult<AgentInboxMessage[]> {
  return useQuery({
    queryKey: ['agent', id, 'inbox', status ?? 'all'],
    queryFn: () =>
      api.get<AgentInboxMessage[]>(
        `/v1/agents/${id}/inbox${status ? `?status=${encodeURIComponent(status)}` : ''}`,
      ),
    enabled: !!id,
  })
}

/** `GET /v1/agents/{id}/questions` — agent Q&A threads (open and resolved). */
export function useAgentQuestions(id?: string): UseQueryResult<AgentQuestion[]> {
  return useQuery({
    queryKey: ['agent', id, 'questions'],
    queryFn: async () => {
      const res = await api.get<{ questions: AgentQuestion[] }>(`/v1/agents/${id}/questions`)
      return res.questions
    },
    enabled: !!id,
  })
}

export interface AgentFormValues {
  id: string
  project: string
  description: string
  dir: string
  command: string
}

/** `POST /v1/agents` -> bare agentResponse (201). A duplicate id comes back as
 * `409 {code:"agent_exists"}`. */
export function useCreateAgent(): UseMutationResult<Agent, Error, AgentFormValues> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (payload) => api.post<Agent>('/v1/agents', payload),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['agents'] })
    },
  })
}

/** `PATCH /v1/agents/{id}` — every field optional; the id itself is immutable. */
export function useUpdateAgent(): UseMutationResult<
  Agent,
  Error,
  { id: string } & Partial<Omit<AgentFormValues, 'id'>> & { enabled?: boolean }
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, ...body }) => api.patch<Agent>(`/v1/agents/${id}`, body),
    onSuccess: (_data, { id }) => {
      queryClient.invalidateQueries({ queryKey: ['agents'] })
      queryClient.invalidateQueries({ queryKey: ['agent', id] })
    },
  })
}

/** `DELETE /v1/agents/{id}` — drops the registration with its inbox; whatever
 * runs inside the agent's own directory is untouched. */
export function useDeleteAgent(): UseMutationResult<{ status: string }, Error, string> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id) => api.del<{ status: string }>(`/v1/agents/${id}`),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['agents'] })
    },
  })
}

/** `POST /v1/agents/{id}/enable|disable` -> bare agentResponse. */
export function useSetAgentEnabled(): UseMutationResult<
  Agent,
  Error,
  { id: string; enabled: boolean }
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, enabled }) =>
      api.post<Agent>(`/v1/agents/${id}/${enabled ? 'enable' : 'disable'}`),
    onSuccess: (_data, { id }) => {
      queryClient.invalidateQueries({ queryKey: ['agents'] })
      queryClient.invalidateQueries({ queryKey: ['agent', id] })
    },
  })
}

/**
 * `POST /v1/agents/{id}/messages` `{body}` -> 202. One delivery path: a live
 * session gets the text through the message queue, a dead one gets an inbox
 * row — the response says which happened.
 */
export function useSendAgentMessage(): UseMutationResult<
  AgentDelivery,
  Error,
  { id: string; body: string }
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, body }) => api.post<AgentDelivery>(`/v1/agents/${id}/messages`, { body }),
    onSuccess: (_data, { id }) => {
      queryClient.invalidateQueries({ queryKey: ['agent', id] })
      queryClient.invalidateQueries({ queryKey: ['agents'] })
    },
  })
}

/**
 * `POST /v1/agents/{id}/start` — the thin launcher: a tmux session named after
 * the agent, running its `command` in its `dir`. An agent without a `dir`
 * answers `400 {code:"agent_no_dir"}`; one already up, `agent_live`.
 */
export function useStartAgent(): UseMutationResult<
  { id: string; status: string; dir: string },
  Error,
  string
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id) => api.post<{ id: string; status: string; dir: string }>(`/v1/agents/${id}/start`),
    onSuccess: (_data, id) => {
      queryClient.invalidateQueries({ queryKey: ['agent', id] })
      queryClient.invalidateQueries({ queryKey: ['agents'] })
      queryClient.invalidateQueries({ queryKey: ['sessions'] })
    },
  })
}

/** `POST /v1/agents/{id}/stop` — kills the tmux session; the registration
 * (and the inbox) stays. */
export function useStopAgent(): UseMutationResult<{ id: string; status: string }, Error, string> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id) => api.post<{ id: string; status: string }>(`/v1/agents/${id}/stop`),
    onSuccess: (_data, id) => {
      queryClient.invalidateQueries({ queryKey: ['agent', id] })
      queryClient.invalidateQueries({ queryKey: ['agents'] })
      queryClient.invalidateQueries({ queryKey: ['sessions'] })
    },
  })
}

/**
 * `POST /v1/agents/{id}/questions` `{body, context?}` -> bare
 * agentQuestionResponse (201). Opens a thread FROM you TO the agent (the api
 * client never sends `X-Rocket-Session`, so the daemon treats the caller as
 * the human); the text reaches the agent the same live-or-inbox way a plain
 * message does.
 */
export function useAskAgent(
  roleId: string | undefined,
): UseMutationResult<AgentQuestion, Error, { body: string; context?: string }> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (payload) => api.post<AgentQuestion>(`/v1/agents/${roleId}/questions`, payload),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['agent', roleId, 'questions'] })
      queryClient.invalidateQueries({ queryKey: ['agent', roleId] })
      queryClient.invalidateQueries({ queryKey: ['threads'] })
    },
  })
}

/** `POST /v1/agent-questions/{id}/reply` `{body}` -> the thread (201). */
export function useReplyAgentQuestion(): UseMutationResult<
  AgentQuestion,
  Error,
  { id: number; body: string; roleId: string; to?: string[] }
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, body, to }) =>
      api.post<AgentQuestion>(`/v1/agent-questions/${id}/reply`, withTo({ body }, to)),
    onSuccess: (_data, { roleId }) => {
      queryClient.invalidateQueries({ queryKey: ['agent', roleId, 'questions'] })
      queryClient.invalidateQueries({ queryKey: ['agent', roleId] })
      queryClient.invalidateQueries({ queryKey: ['threads'] })
    },
  })
}

/** `POST /v1/agent-questions/{id}/answer` `{body}` | `{dismiss:true}` — human
 * only; resolves the thread. */
export function useAnswerAgentQuestion(): UseMutationResult<
  AgentQuestion,
  Error,
  { id: number; roleId: string; to?: string[] } & (
    | { body: string; dismiss?: never; choose?: never }
    | { dismiss: true; body?: never; choose?: never }
    | { choose: number; body?: never; dismiss?: never }
  )
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, body, dismiss, choose, to }) =>
      api.post<AgentQuestion>(
        `/v1/agent-questions/${id}/answer`,
        // See useAnswerQuestion: dismiss and choose both close the thread, so
        // neither carries addressees, and choose is a 1-based option index.
        dismiss ? { dismiss: true } : choose ? { choose } : withTo({ body }, to),
      ),
    onSuccess: (_data, { roleId }) => {
      queryClient.invalidateQueries({ queryKey: ['agent', roleId, 'questions'] })
      queryClient.invalidateQueries({ queryKey: ['agent', roleId] })
      queryClient.invalidateQueries({ queryKey: ['threads'] })
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
      queryClient.invalidateQueries({ queryKey: ['questions'] })
      queryClient.invalidateQueries({ queryKey: ['threads'] })
    } else if (event.type === 'orchestrator.heartbeat_sent') {
      // High-frequency event; keep invalidation minimal — only the task
      // detail view (which shows session/heartbeat state) needs to refresh.
      queryClient.invalidateQueries({ queryKey: ['task'] })
    } else if (event.type.startsWith('agent.')) {
      // Agent registration, session start/stop and Q&A threads all move the
      // agents list and the open agent card; the session rows behind
      // `session_alive` move with them.
      queryClient.invalidateQueries({ queryKey: ['agents'] })
      queryClient.invalidateQueries({ queryKey: ['agent'] })
      queryClient.invalidateQueries({ queryKey: ['sessions'] })
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
