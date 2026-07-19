import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './client'
import { useConnection } from './events'
import { useServers } from '../servers/ServerContext'
import type {
  GithubRepo,
  Health,
  Message,
  Project,
  Question,
  Repo,
  Session,
  Settings,
  SystemInfo,
  SystemCleanupResult,
  Task,
  TaskDetail,
  TaskDoc,
  TaskLogEntry,
  TaskStatus,
} from './types'

/** Base URL of the active server; components behind the servers screen can assume it exists. */
export function useBaseUrl(): string {
  const { baseUrl } = useServers()
  return baseUrl ?? 'http://127.0.0.1:4477'
}

/**
 * Polling interval: `fast` while the SSE stream is down, slow safety-net
 * when events already drive invalidation.
 */
function usePoll(fast: number, slow = 30000): number {
  const { sse } = useConnection()
  return sse ? Math.max(fast, slow) : fast
}

export function useHealth(baseUrl: string, enabled = true) {
  return useQuery({
    queryKey: [baseUrl, 'health'],
    queryFn: () => api.get<Health>(baseUrl, '/v1/health'),
    enabled,
    refetchInterval: 7000,
    retry: false,
  })
}

export function useProjects() {
  const baseUrl = useBaseUrl()
  const refetchInterval = usePoll(5000)
  return useQuery({
    queryKey: [baseUrl, 'projects'],
    queryFn: () => api.get<Project[]>(baseUrl, '/v1/projects'),
    refetchInterval,
  })
}

export function useRepos() {
  const baseUrl = useBaseUrl()
  const refetchInterval = usePoll(15000, 60000)
  return useQuery({
    queryKey: [baseUrl, 'repos'],
    queryFn: () => api.get<Repo[]>(baseUrl, '/v1/repos'),
    refetchInterval,
  })
}

export function useSessions(project?: string) {
  const baseUrl = useBaseUrl()
  const refetchInterval = usePoll(5000)
  const qs = project ? `?project=${encodeURIComponent(project)}` : ''
  return useQuery({
    queryKey: [baseUrl, 'sessions', project ?? 'all'],
    queryFn: () => api.get<Session[]>(baseUrl, `/v1/sessions${qs}`),
    refetchInterval,
  })
}

export function useTasks(project?: string) {
  const baseUrl = useBaseUrl()
  const refetchInterval = usePoll(5000)
  const qs = project ? `?project=${encodeURIComponent(project)}` : ''
  return useQuery({
    queryKey: [baseUrl, 'tasks', project ?? 'all'],
    queryFn: async () => (await api.get<{ tasks: Task[] }>(baseUrl, `/v1/tasks${qs}`)).tasks ?? [],
    refetchInterval,
  })
}

export function useTaskDetail(id: number) {
  const baseUrl = useBaseUrl()
  const refetchInterval = usePoll(3000)
  return useQuery({
    queryKey: [baseUrl, 'task', id],
    queryFn: () => api.get<TaskDetail>(baseUrl, `/v1/tasks/${id}`),
    refetchInterval,
  })
}

export function useTaskDocs(id: number, enabled: boolean) {
  const baseUrl = useBaseUrl()
  const refetchInterval = usePoll(10000, 60000)
  return useQuery({
    queryKey: [baseUrl, 'task', id, 'docs'],
    queryFn: async () => (await api.get<{ docs: TaskDoc[] }>(baseUrl, `/v1/tasks/${id}/docs`)).docs ?? [],
    enabled,
    refetchInterval,
  })
}

export function useTaskLog(id: number, enabled: boolean) {
  const baseUrl = useBaseUrl()
  const refetchInterval = usePoll(5000)
  return useQuery({
    queryKey: [baseUrl, 'task', id, 'log'],
    queryFn: async () => (await api.get<{ log: TaskLogEntry[] }>(baseUrl, `/v1/tasks/${id}/log`)).log ?? [],
    enabled,
    refetchInterval,
  })
}

export function useTaskQuestions(id: number) {
  const baseUrl = useBaseUrl()
  const refetchInterval = usePoll(3000)
  return useQuery({
    queryKey: [baseUrl, 'task', id, 'questions'],
    queryFn: async () =>
      (await api.get<{ questions: Question[] }>(baseUrl, `/v1/tasks/${id}/questions`)).questions ?? [],
    refetchInterval,
  })
}

export function useMessages(sessionId: string | undefined) {
  const baseUrl = useBaseUrl()
  const refetchInterval = usePoll(4000)
  return useQuery({
    queryKey: [baseUrl, 'messages', sessionId],
    queryFn: async () =>
      (await api.get<{ messages: Message[] }>(baseUrl, `/v1/messages?session=${sessionId}&limit=50`)).messages ?? [],
    enabled: !!sessionId,
    refetchInterval,
  })
}

export function useSystem() {
  const baseUrl = useBaseUrl()
  const refetchInterval = usePoll(5000)
  return useQuery({
    queryKey: [baseUrl, 'system'],
    queryFn: () => api.get<SystemInfo>(baseUrl, '/v1/system'),
    refetchInterval,
  })
}

export function useSettings() {
  const baseUrl = useBaseUrl()
  return useQuery({
    queryKey: [baseUrl, 'settings'],
    queryFn: () => api.get<Settings>(baseUrl, '/v1/settings'),
  })
}

export function useSessionOutput(id: string, lines = 200) {
  const baseUrl = useBaseUrl()
  return useQuery({
    queryKey: [baseUrl, 'output', id],
    queryFn: () => api.get<{ output: string }>(baseUrl, `/v1/sessions/${id}/output?lines=${lines}`),
    refetchInterval: 2000,
  })
}

// ---------------------------------------------------------------------------
// Mutations
// ---------------------------------------------------------------------------

export function useSendMessage() {
  const baseUrl = useBaseUrl()
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (p: { to: string; body: string }) => api.post<Message>(baseUrl, '/v1/messages', p),
    onSuccess: () => qc.invalidateQueries({ queryKey: [baseUrl, 'messages'] }),
  })
}

export function useCreateTask() {
  const baseUrl = useBaseUrl()
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (p: { title: string; description?: string; project: string }) =>
      api.post<Task>(baseUrl, '/v1/tasks', p),
    onSuccess: () => qc.invalidateQueries({ queryKey: [baseUrl, 'tasks'] }),
  })
}

export function useStartTask() {
  const baseUrl = useBaseUrl()
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => api.post(baseUrl, `/v1/tasks/${id}/start`, {}),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: [baseUrl, 'tasks'] })
      qc.invalidateQueries({ queryKey: [baseUrl, 'task'] })
    },
  })
}

export function useQuestionReply() {
  const baseUrl = useBaseUrl()
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (p: { id: number; body: string }) =>
      api.post(baseUrl, `/v1/questions/${p.id}/reply`, { body: p.body }),
    onSuccess: () => qc.invalidateQueries({ queryKey: [baseUrl, 'task'] }),
  })
}

export function useQuestionAnswer() {
  const baseUrl = useBaseUrl()
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (p: { id: number; body: string }) =>
      api.post(baseUrl, `/v1/questions/${p.id}/answer`, { body: p.body }),
    onSuccess: () => qc.invalidateQueries({ queryKey: [baseUrl, 'task'] }),
  })
}

export function useKillSession() {
  const baseUrl = useBaseUrl()
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (p: { id: string; cleanup?: boolean }) =>
      api.post(baseUrl, `/v1/sessions/${p.id}/kill${p.cleanup ? '?cleanup=true' : ''}`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: [baseUrl, 'sessions'] })
      qc.invalidateQueries({ queryKey: [baseUrl, 'system'] })
      qc.invalidateQueries({ queryKey: [baseUrl, 'task'] })
    },
  })
}

export function useRestoreSession() {
  const baseUrl = useBaseUrl()
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.post<Session>(baseUrl, `/v1/sessions/${id}/restore`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: [baseUrl, 'sessions'] })
      qc.invalidateQueries({ queryKey: [baseUrl, 'system'] })
    },
  })
}

export function useCancelTask() {
  const baseUrl = useBaseUrl()
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => api.post<Task>(baseUrl, `/v1/tasks/${id}/cancel`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: [baseUrl, 'tasks'] })
      qc.invalidateQueries({ queryKey: [baseUrl, 'task'] })
      qc.invalidateQueries({ queryKey: [baseUrl, 'sessions'] })
    },
  })
}

export function useMoveTask() {
  const baseUrl = useBaseUrl()
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (p: { id: number; status: TaskStatus }) =>
      api.patch<Task>(baseUrl, `/v1/tasks/${p.id}`, { status: p.status }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: [baseUrl, 'tasks'] })
      qc.invalidateQueries({ queryKey: [baseUrl, 'task'] })
    },
  })
}

export function useUpdateTask() {
  const baseUrl = useBaseUrl()
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (p: { id: number; title?: string; description?: string }) =>
      api.patch<Task>(baseUrl, `/v1/tasks/${p.id}`, { title: p.title, description: p.description }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: [baseUrl, 'tasks'] })
      qc.invalidateQueries({ queryKey: [baseUrl, 'task'] })
    },
  })
}

export function useSystemCleanup() {
  const baseUrl = useBaseUrl()
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api.post<SystemCleanupResult>(baseUrl, '/v1/system/cleanup'),
    onSuccess: () => qc.invalidateQueries({ queryKey: [baseUrl, 'system'] }),
  })
}

export function useQuestionDismiss() {
  const baseUrl = useBaseUrl()
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => api.post(baseUrl, `/v1/questions/${id}/answer`, { dismiss: true }),
    onSuccess: () => qc.invalidateQueries({ queryKey: [baseUrl, 'task'] }),
  })
}

export function useGithubRepos(q: string, enabled = true) {
  const baseUrl = useBaseUrl()
  return useQuery({
    queryKey: [baseUrl, 'github-repos', q],
    queryFn: async () =>
      (await api.get<{ repos: GithubRepo[] }>(baseUrl, `/v1/github/repos?q=${encodeURIComponent(q)}`)).repos ?? [],
    enabled,
    staleTime: 60000,
  })
}

export function useRegisterRepo() {
  const baseUrl = useBaseUrl()
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (p: { github?: string; path?: string; id?: string }) => api.post<Repo>(baseUrl, '/v1/repos', p),
    onSuccess: () => qc.invalidateQueries({ queryKey: [baseUrl, 'repos'] }),
  })
}

export function useDeleteRepo() {
  const baseUrl = useBaseUrl()
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del(baseUrl, `/v1/repos/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: [baseUrl, 'repos'] }),
  })
}

export function useCreateProject() {
  const baseUrl = useBaseUrl()
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (p: { id?: string; name: string; main: string; linked?: string[] }) =>
      api.post<Project>(baseUrl, '/v1/projects', p),
    onSuccess: () => qc.invalidateQueries({ queryKey: [baseUrl, 'projects'] }),
  })
}

export function useUpdateProject() {
  const baseUrl = useBaseUrl()
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (p: { id: string; name?: string; main?: string; linked?: string[] }) =>
      api.patch<Project>(baseUrl, `/v1/projects/${p.id}`, { name: p.name, main: p.main, linked: p.linked }),
    onSuccess: () => qc.invalidateQueries({ queryKey: [baseUrl, 'projects'] }),
  })
}

export function useDeleteProject() {
  const baseUrl = useBaseUrl()
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del(baseUrl, `/v1/projects/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: [baseUrl, 'projects'] }),
  })
}

export function useSaveSettings() {
  const baseUrl = useBaseUrl()
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (p: { github_token: string }) => api.put<Settings>(baseUrl, '/v1/settings', p),
    onSuccess: () => qc.invalidateQueries({ queryKey: [baseUrl, 'settings'] }),
  })
}
