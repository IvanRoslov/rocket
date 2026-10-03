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
import type { Device, PairingCode } from './auth'
import type {
  Agent,
  AgentDelivery,
  AgentInboxMessage,
  AgentCatalog,
  AgentKinds,
  AgentQuestion,
  BrainstormOutcome,
  BrainstormStats,
  BrainstormStorm,
  GithubIssue,
  GithubRepo,
  GlobalQuestion,
  ImportCatalogResult,
  Message,
  ModelPrice,
  ModelPriceInput,
  ModelProfile,
  ModelProfileInput,
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
  TaskGate,
  TaskLogEntry,
  TaskStatus,
  TaskUsage,
  ThreadInboxEntry,
  UsageStats,
  ThreadType,
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

export function useDevices(): UseQueryResult<Device[]> {
  return useQuery({
    queryKey: ['devices'],
    queryFn: () => api.get<Device[]>('/v1/auth/devices'),
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
  brainstorm: Task[]
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

/**
 * Milestones board: `GET /v1/tasks?milestones=true&board=true` (task #1023,
 * spec v2). Milestones live outside every project, so unlike `useTasksBoard`
 * this one takes no project and is never scoped by the project switcher.
 */
export function useMilestonesBoard(): UseQueryResult<TaskBoard> {
  return useQuery({
    queryKey: ['tasks', 'milestones', 'board'],
    queryFn: async () => {
      const res = await api.get<TaskBoardResponse>('/v1/tasks?milestones=true&board=true')
      return res.board
    },
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

/**
 * `GET /v1/tasks/{id}/docs?history=true` — every version of every doc. The
 * plain list carries only the newest version of each (kind, title), so a
 * gate's pinned "Spec v1" is only resolvable from here.
 */
export function useTaskDocHistory(id: number | undefined): UseQueryResult<TaskDoc[]> {
  return useQuery({
    queryKey: ['task', id, 'docs', 'history'],
    queryFn: async () => {
      const res = await api.get<{ docs: TaskDoc[] }>(`/v1/tasks/${id}/docs?history=true`)
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
 * `POST /v1/tasks` with `milestone: true` -> a root task outside every
 * project. `--milestone` and a project are mutually exclusive, so this hook
 * deliberately has no project parameter.
 */
export function useCreateMilestone(): UseMutationResult<
  Task,
  Error,
  { title: string; description?: string }
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (payload) => api.post<Task>('/v1/tasks', { ...payload, milestone: true }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tasks'] })
    },
  })
}

/**
 * `POST /v1/tasks/{id}/assign` — the human's half of milestone ownership:
 * `{agent_id}` hands it over, `{none: true}` takes it back. The agent's own
 * half (`take`) is a CLI verb from inside its session and has no UI.
 */
export function useAssignMilestone(): UseMutationResult<
  Task,
  Error,
  { id: number; agentId: string | null }
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, agentId }) =>
      api.post<Task>(`/v1/tasks/${id}/assign`, agentId ? { agent_id: agentId } : { none: true }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tasks'] })
      queryClient.invalidateQueries({ queryKey: ['task'] })
      // The agent card lists its milestones — it moved too.
      queryClient.invalidateQueries({ queryKey: ['agents'] })
      queryClient.invalidateQueries({ queryKey: ['agent'] })
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
 * `PATCH /v1/tasks/{id}` `{title?, description?, allowed_profiles?}` -> bare
 * taskResponse (200). `allowed_profiles: []` lets workers use every enabled
 * profile; agent sessions get 403 human_only for it (task #5026).
 * Used by the Overview tab's inline title/description editor. The daemon
 * does not itself reject an empty title on this path (see
 * internal/api/tasks.go handlePatchTask) — callers must validate that
 * client-side before calling mutate.
 */
export function useUpdateTask(): UseMutationResult<
  Task,
  Error,
  { id: number; title?: string; description?: string; allowed_profiles?: string[] }
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

/** `GET /v1/agent-kinds` -> the agent implementations the daemon can launch,
 * for the orchestrator picker on Start ▸. */
export function useAgentKinds(): UseQueryResult<AgentKinds> {
  return useQuery({
    queryKey: ['agent-kinds'],
    queryFn: () => api.get<AgentKinds>('/v1/agent-kinds'),
    retry: false,
  })
}

/** `POST /v1/tasks/{id}/start` `{agent?, profile?, allowed_profiles?}` ->
 * `{task_id,feature_slug,session_id}` (201). Root tasks only. Empty `agent` /
 * `profile` stay off the wire (the daemon uses its defaults, task #5026);
 * `allowed_profiles` is sent whenever given, `[]` included — it replaces the
 * task's allowlist. With nothing set there is no body at all. */
export function useStartTask(): UseMutationResult<
  { task_id: number; feature_slug: string; session_id: string },
  Error,
  { id: number; agent?: string; profile?: string; allowed_profiles?: string[] }
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, agent, profile, allowed_profiles }) => {
      const body: { agent?: string; profile?: string; allowed_profiles?: string[] } = {}
      if (agent) body.agent = agent
      if (profile) body.profile = profile
      // [] is meaningful (clear the allowlist), so only absence leaves it off.
      if (allowed_profiles !== undefined) body.allowed_profiles = allowed_profiles
      return api.post<{ task_id: number; feature_slug: string; session_id: string }>(
        `/v1/tasks/${id}/start`,
        Object.keys(body).length > 0 ? body : undefined,
      )
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tasks'] })
      queryClient.invalidateQueries({ queryKey: ['task'] })
      queryClient.invalidateQueries({ queryKey: ['sessions'] })
    },
  })
}

/** `GET /v1/model-profiles` -> the whole registry, in registry order (task #5026). */
export function useModelProfiles(): UseQueryResult<ModelProfile[]> {
  return useQuery({
    queryKey: ['model-profiles'],
    queryFn: () => api.get<{ profiles: ModelProfile[] }>('/v1/model-profiles').then((r) => r.profiles),
    retry: false,
  })
}

/** `POST /v1/model-profiles` -> the new profile (201). Human-only. */
export function useCreateModelProfile(): UseMutationResult<ModelProfile, Error, ModelProfileInput> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (body) => api.post<ModelProfile>('/v1/model-profiles', body),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['model-profiles'] }),
  })
}

/** `PATCH /v1/model-profiles/{name}` (partial; a profile cannot be renamed). Human-only. */
export function useUpdateModelProfile(): UseMutationResult<
  ModelProfile,
  Error,
  { name: string } & Partial<Omit<ModelProfileInput, 'name'>> & { enabled?: boolean }
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ name, ...body }) => api.patch<ModelProfile>(`/v1/model-profiles/${name}`, body),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['model-profiles'] }),
  })
}

/** `GET /v1/model-catalog` -> every agent's model catalog (choose-model v2).
 * The daemon caches it for 10 minutes, so there is nothing to poll. */
export function useModelCatalog(): UseQueryResult<AgentCatalog[]> {
  return useQuery({
    queryKey: ['model-catalog'],
    queryFn: () => api.get<{ agents: AgentCatalog[] }>('/v1/model-catalog').then((r) => r.agents),
    retry: false,
    staleTime: 10 * 60 * 1000,
  })
}

/** `GET /v1/model-catalog?refresh=1` -> the catalogs refetched past the
 * daemon's 10-minute cache; the result replaces `useModelCatalog`'s data. */
export function useRefreshModelCatalog(): UseMutationResult<AgentCatalog[], Error, void> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: () => api.get<{ agents: AgentCatalog[] }>('/v1/model-catalog?refresh=1').then((r) => r.agents),
    onSuccess: (agents) => queryClient.setQueryData(['model-catalog'], agents),
  })
}

/** `POST /v1/model-profiles/import-catalog` -> a disabled profile for every
 * catalog model no profile uses yet (main only unless `include_legacy`). Human-only. */
export function useImportCatalog(): UseMutationResult<ImportCatalogResult, Error, { include_legacy: boolean }> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (body) => api.post<ImportCatalogResult>('/v1/model-profiles/import-catalog', body),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['model-profiles'] }),
  })
}

/** `DELETE /v1/model-profiles/{name}` (204). 409 profile_in_use while it is a default. */
export function useDeleteModelProfile(): UseMutationResult<void, Error, string> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (name) => api.del<void>(`/v1/model-profiles/${name}`),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['model-profiles'] }),
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

/**
 * The three ways to close a thread: a written answer (which may carry
 * addressees), a `dismiss`, or a 1-based `choose` into `options`. A storm
 * thread (task #4901) may send the human's comment as `body` next to `choose`.
 */
type AnswerShape =
  | { body: string; dismiss?: never; choose?: never }
  | { dismiss: true; body?: never; choose?: never }
  | { choose: number; body?: string; dismiss?: never }

/**
 * Builds the answer body. Both a dismiss and a picked option resolve the
 * thread outright, so nobody is left to respond and an addressee list would
 * be meaningless. `choose` is a 1-based index into `options`; the daemon
 * substitutes the option's own text (chooseOptionBody in
 * internal/api/threads.go) and records a body sent with it as the storm
 * comment — so a blank comment stays off the wire.
 */
function answerPayload({
  body,
  dismiss,
  choose,
  to,
}: {
  body?: string
  dismiss?: boolean
  choose?: number
  to?: string[]
}): object {
  if (dismiss) return { dismiss: true }
  if (choose) return body && body.trim() ? { choose, body } : { choose }
  return withTo({ body }, to)
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
  { id: number; taskId: number; to?: string[] } & AnswerShape
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, body, dismiss, choose, to }) =>
      api.post<Question>(`/v1/questions/${id}/answer`, answerPayload({ body, dismiss, choose, to })),
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
): UseMutationResult<Question, Error, { body: string; title?: string }> {
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
export function useUpdateSettings(): UseMutationResult<
  Settings,
  Error,
  {
    github_token?: string
    orchestrator_brainstorm_custom?: boolean
    default_orchestrator_profile?: string
    default_worker_profile?: string
  }
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (payload) => api.put<Settings>('/v1/settings', payload),
    // The PUT answers with the full settings, so it IS the new cache entry —
    // no refetch, no flicker. `login` is dropped: GET never carries it.
    onSuccess: ({ login: _login, ...settings }, payload) => {
      queryClient.setQueryData(['settings'], settings)
      // Only a new token changes which GitHub repos are visible.
      if (payload.github_token !== undefined) {
        queryClient.invalidateQueries({ queryKey: ['github-repos'] })
      }
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
// Unified thread actions (the Questions v3 screen)
// ---------------------------------------------------------------------------

/**
 * Which subject a thread hangs off. `GET /v1/threads` mixes task threads and
 * role threads in one list, but the write endpoints stay separate
 * (`/v1/questions/{id}/…` vs `/v1/agent-questions/{id}/…`), so anything that
 * acts on an inbox row has to carry the kind with it.
 */
export type ThreadRef =
  | { id: number; kind: 'task'; taskId: number }
  | { id: number; kind: 'role'; roleId: string }

/** Invalidates everything a write to `ref` can have changed. */
function invalidateThread(queryClient: QueryClient, ref: ThreadRef): void {
  if (ref.kind === 'task') {
    queryClient.invalidateQueries({ queryKey: ['task', ref.taskId, 'questions'] })
    queryClient.invalidateQueries({ queryKey: ['task', ref.taskId] })
    queryClient.invalidateQueries({ queryKey: ['questions'] })
  } else {
    queryClient.invalidateQueries({ queryKey: ['agent', ref.roleId, 'questions'] })
    queryClient.invalidateQueries({ queryKey: ['agent', ref.roleId] })
  }
  queryClient.invalidateQueries({ queryKey: ['threads'] })
}

/**
 * Resolves a thread of either kind. Same three shapes as `useAnswerQuestion`:
 * a written answer (which may carry addressees), a `dismiss`, or a 1-based
 * `choose` into `options`.
 */
export function useAnswerThread(): UseMutationResult<
  unknown,
  Error,
  { ref: ThreadRef; to?: string[] } & AnswerShape
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ ref, body, dismiss, choose, to }) => {
      const path =
        ref.kind === 'task' ? `/v1/questions/${ref.id}/answer` : `/v1/agent-questions/${ref.id}/answer`
      return api.post(path, answerPayload({ body, dismiss, choose, to }))
    },
    onSuccess: (_data, { ref }) => invalidateThread(queryClient, ref),
  })
}

/** Replies into a thread of either kind, leaving it open. */
export function useReplyThread(): UseMutationResult<
  unknown,
  Error,
  { ref: ThreadRef; body: string; to?: string[] }
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ ref, body, to }) => {
      const path =
        ref.kind === 'task' ? `/v1/questions/${ref.id}/reply` : `/v1/agent-questions/${ref.id}/reply`
      return api.post(path, withTo({ body }, to))
    },
    onSuccess: (_data, { ref }) => invalidateThread(queryClient, ref),
  })
}

/** Who a new thread can be opened on: a task's orchestrator, or an agent. */
export type AskTarget = { kind: 'task'; id: number } | { kind: 'role'; id: string }

/**
 * Opens a thread FROM the dashboard user. `type: 'fyi'` posts a note instead
 * of a question: the daemon is born it resolved, so it waits on nobody and
 * never enters the queue or the counters. An absent `type` means `decision`.
 */
export function useAskThread(): UseMutationResult<
  unknown,
  Error,
  { target: AskTarget; body: string; title?: string; type?: ThreadType }
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ target, body, title, type }) => {
      const path =
        target.kind === 'task' ? `/v1/tasks/${target.id}/questions` : `/v1/agents/${target.id}/questions`
      return api.post(path, { body, ...(title ? { title } : {}), ...(type ? { type } : {}) })
    },
    onSuccess: (_data, { target }) => {
      if (target.kind === 'task') {
        queryClient.invalidateQueries({ queryKey: ['task', target.id, 'questions'] })
        queryClient.invalidateQueries({ queryKey: ['task', target.id] })
      } else {
        queryClient.invalidateQueries({ queryKey: ['agent', target.id, 'questions'] })
        queryClient.invalidateQueries({ queryKey: ['agent', target.id] })
      }
      queryClient.invalidateQueries({ queryKey: ['threads'] })
    },
  })
}

// ---------------------------------------------------------------------------
// Storm: gate, outcome, metric (task #4901)
// ---------------------------------------------------------------------------

/** `GET /v1/tasks/{id}/gates` — the task's storm exit gates, newest first. */
export function useTaskGates(id: number | undefined): UseQueryResult<TaskGate[]> {
  return useQuery({
    queryKey: ['task', id, 'gates'],
    queryFn: async () => {
      const res = await api.get<{ gates: TaskGate[] }>(`/v1/tasks/${id}/gates`)
      return res.gates
    },
    enabled: id !== undefined,
  })
}

/** `GET /v1/tasks/{id}/brainstorm/stats` — one `storms[]` row for the Brainstorm tab. */
export function useTaskBrainstormStats(id: number | undefined): UseQueryResult<BrainstormStorm> {
  return useQuery({
    queryKey: ['task', id, 'brainstorm-stats'],
    queryFn: () => api.get<BrainstormStorm>(`/v1/tasks/${id}/brainstorm/stats`),
    enabled: id !== undefined,
    retry: false,
  })
}

/** `GET /v1/stats/brainstorm?weeks=N` — the storm metric screen. */
export function useBrainstormStats(weeks?: number): UseQueryResult<BrainstormStats> {
  return useQuery({
    queryKey: ['stats', 'brainstorm', weeks ?? null],
    queryFn: () => api.get<BrainstormStats>(`/v1/stats/brainstorm${weeks ? `?weeks=${weeks}` : ''}`),
    retry: false,
  })
}

/**
 * `POST /v1/gates/{id}/decide` `{decision, comment}` — the human's Go or
 * "needs changes". 409 when the gate was superseded by a newer spec or is
 * already decided; callers show that and refetch.
 */
export function useDecideGate(): UseMutationResult<
  TaskGate,
  Error,
  { gateId: number; taskId: number; decision: 'go' | 'changes'; comment?: string }
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ gateId, decision, comment }) =>
      api.post<TaskGate>(`/v1/gates/${gateId}/decide`, { decision, comment: comment ?? '' }),
    // Settled, not success: a 409 means the gate list we showed is stale.
    onSettled: (_data, _err, { taskId }) => {
      queryClient.invalidateQueries({ queryKey: ['task', taskId] })
      queryClient.invalidateQueries({ queryKey: ['tasks'] })
      queryClient.invalidateQueries({ queryKey: ['stats'] })
    },
  })
}

/** `PATCH /v1/questions/{id}/outcome` `{outcome}` — the human corrects a storm answer's outcome. */
export function useSetOutcome(): UseMutationResult<
  Question,
  Error,
  { id: number; taskId: number; outcome: BrainstormOutcome }
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, outcome }) => api.patch<Question>(`/v1/questions/${id}/outcome`, { outcome }),
    onSuccess: (_data, { id, taskId }) => {
      invalidateThread(queryClient, { id, kind: 'task', taskId })
      queryClient.invalidateQueries({ queryKey: ['stats'] })
    },
  })
}

// ---------------------------------------------------------------------------
// Agent usage (task #5138 spec §3)
// ---------------------------------------------------------------------------

export interface UsageFilter {
  /** Local `YYYY-MM-DD`, inclusive. */
  from: string
  to: string
  /** Project id; empty for all projects. */
  project?: string
}

/** `GET /v1/stats/usage` — tokens and ≈ $ by model and by task for a period. */
export function useUsageStats(filter: UsageFilter, enabled = true): UseQueryResult<UsageStats> {
  const params = new URLSearchParams({ from: filter.from, to: filter.to })
  if (filter.project) params.set('project', filter.project)
  return useQuery({
    queryKey: ['usage', 'stats', filter.from, filter.to, filter.project ?? ''],
    queryFn: () => api.get<UsageStats>(`/v1/stats/usage?${params}`),
    enabled,
  })
}

/** `GET /v1/tasks/{id}/usage` — every session of a feature; root tasks only. */
export function useTaskUsage(id: number | undefined): UseQueryResult<TaskUsage> {
  return useQuery({
    queryKey: ['taskUsage', id],
    queryFn: () => api.get<TaskUsage>(`/v1/tasks/${id}/usage`),
    enabled: id !== undefined,
  })
}

/** `GET /v1/stats/prices` — every model seen in usage, unpriced ones with null prices. */
export function usePrices(): UseQueryResult<ModelPrice[]> {
  return useQuery({
    queryKey: ['usage', 'prices'],
    queryFn: () => api.get<{ prices: ModelPrice[] }>('/v1/stats/prices').then((r) => r.prices),
  })
}

// Cost is computed at read time from the current prices, so a price change
// moves every usage view.
function invalidateUsage(queryClient: QueryClient) {
  queryClient.invalidateQueries({ queryKey: ['usage'] })
  queryClient.invalidateQueries({ queryKey: ['taskUsage'] })
}

export function useSetPrice(): UseMutationResult<ModelPrice, Error, { model: string } & ModelPriceInput> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ model, ...body }) => api.put<ModelPrice>(`/v1/stats/prices/${encodeURIComponent(model)}`, body),
    onSuccess: () => invalidateUsage(queryClient),
  })
}

export function useDeletePrice(): UseMutationResult<void, Error, string> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (model) => api.del<void>(`/v1/stats/prices/${encodeURIComponent(model)}`),
    onSuccess: () => invalidateUsage(queryClient),
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
/** How long SSE invalidations are collected before they are applied at once. */
export const INVALIDATION_WINDOW_MS = 1000

/** The query keys an SSE event makes stale; empty when it moves nothing. */
function eventQueryKeys(type: string): string[][] {
  if (type === 'session.chat_updated') {
    // Fires on every activity tick of a talking agent; the chat screen polls
    // its own cursor. Invalidating on it refetched sessions and projects many
    // times a second per open tab.
    return []
  }
  if (type.startsWith('session.')) return [['sessions'], ['projects']]
  if (type.startsWith('message.')) return [['messages']]
  if (type.startsWith('task.')) {
    // Answers, outcome overrides and gate decisions all move the storm metric.
    return [['tasks'], ['task'], ['questions'], ['threads'], ['stats']]
  }
  if (type.startsWith('milestone.')) {
    // `milestone.quiet` (subtask #1032) flips the quiet flag the milestone
    // cards render, on both the board and the holder's agent card.
    return [['tasks'], ['agents'], ['agent']]
  }
  if (type === 'orchestrator.heartbeat_sent') {
    // High-frequency event; keep invalidation minimal — only the task
    // detail view (which shows session/heartbeat state) needs to refresh.
    return [['task']]
  }
  if (type.startsWith('agent.')) {
    // Agent registration, session start/stop and Q&A threads all move the
    // agents list and the open agent card; the session rows behind
    // `session_alive` move with them.
    return [['agents'], ['agent'], ['sessions']]
  }
  if (type === 'usage.collected') return [['usage'], ['taskUsage']]
  if (type.startsWith('repo.clone_')) return [['repos']]
  if (type.startsWith('pr.')) {
    // PR state changes (phase 4): re-fetch the sessions carrying pr_*
    // fields plus the task/board views that surface PR badges.
    return [['sessions'], ['tasks'], ['task']]
  }
  return []
}

/**
 * Turns SSE events into query invalidations, coalesced: the keys a burst of
 * events touches are collected for INVALIDATION_WINDOW_MS and invalidated once,
 * and a refetch already in flight is reused instead of cancelled
 * (`cancelRefetch: false`). Invalidating on every event, with React Query's
 * default of cancelling the running fetch, turned an event burst into a
 * request burst — and the daemon, which does not notice a client hanging up,
 * kept executing every abandoned request until it drowned.
 */
export function wireInvalidation(queryClient: QueryClient) {
  const pending = new Map<string, string[]>()
  let timer: ReturnType<typeof setTimeout> | undefined

  const flush = () => {
    timer = undefined
    const keys = [...pending.values()]
    pending.clear()
    for (const queryKey of keys) {
      queryClient.invalidateQueries({ queryKey }, { cancelRefetch: false })
    }
  }

  return (event: RocketEvent) => {
    const keys = eventQueryKeys(event.type)
    if (keys.length === 0) return
    for (const key of keys) pending.set(JSON.stringify(key), key)
    timer ??= setTimeout(flush, INVALIDATION_WINDOW_MS)
  }
}

export function useQueryClientInvalidation(): (event: RocketEvent) => void {
  const queryClient = useQueryClient()
  return wireInvalidation(queryClient)
}

export function useRevokeDevice(): UseMutationResult<void, Error, number> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id) => api.del<void>(`/v1/auth/devices/${id}`),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['devices'] })
    },
  })
}

export function useCreatePairingCode(): UseMutationResult<PairingCode, Error, void> {
  return useMutation({
    mutationFn: () => api.post<PairingCode>('/v1/auth/pairing-codes'),
  })
}

export function useLogout(): UseMutationResult<void, Error, void> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: () => api.post<void>('/v1/auth/logout'),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['devices'] })
      window.location.assign('/login')
    },
  })
}
