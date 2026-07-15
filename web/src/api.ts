import type { CapabilitiesResponse, ExecutionLog, HealthResponse, ImportResult, ListTasksParams, ReadinessResponse, RunResponse, SearchResponse, Task, TaskListResponse, TaskStatsResponse } from './types'

export type ApiErrorKind = 'offline' | 'unavailable' | 'not-found' | 'validation' | 'conflict' | 'unknown'

export class ApiError extends Error {
  constructor(message: string, public status: number | null, public kind: ApiErrorKind) { super(message) }
}

const friendlyMessages: Record<number, string> = {
  400: '提交的内容不符合要求，请检查后重试。',
  404: '该功能当前不可用，或请求的内容不存在。',
  409: '当前状态不允许执行此操作，请刷新后重试。',
  502: 'AI 服务暂时无法完成请求，请稍后重试。',
  503: '依赖服务暂时不可用，请确认后端组件已启动。',
}

function classify(status: number): ApiErrorKind {
  if (status === 404) return 'not-found'
  if (status === 400) return 'validation'
  if (status === 409) return 'conflict'
  if (status === 502 || status === 503) return 'unavailable'
  return 'unknown'
}

async function request<T>(path: string, init?: RequestInit, acceptedStatuses: number[] = []): Promise<T> {
  try {
    const response = await fetch(path, { ...init, headers: { Accept: 'application/json', ...init?.headers } })
    if (!response.ok && !acceptedStatuses.includes(response.status)) {
      throw new ApiError(friendlyMessages[response.status] ?? '请求失败，请稍后重试。', response.status, classify(response.status))
    }
    return await response.json() as T
  } catch (error) {
    if (error instanceof ApiError) throw error
    throw new ApiError('无法连接 FlowPilot 后端，请确认服务已在 8080 端口启动。', null, 'offline')
  }
}

export const api = {
  checkBackend: () => request<HealthResponse>('/health'),
  checkReadiness: () => request<ReadinessResponse>('/ready', undefined, [503]),
  getCapabilities: () => request<CapabilitiesResponse>('/api/capabilities'),
  getTaskStats: () => request<TaskStatsResponse>('/api/tasks/stats'),
  listTasks: (params: ListTasksParams = {}) => {
    const query = new URLSearchParams()
    if (params.page) query.set('page', String(params.page))
    if (params.pageSize) query.set('page_size', String(params.pageSize))
    if (params.taskType) query.set('task_type', params.taskType)
    if (params.status) query.set('status', params.status)
    if (params.query?.trim()) query.set('query', params.query.trim())
    const suffix = query.size ? `?${query.toString()}` : ''
    return request<TaskListResponse>(`/api/tasks${suffix}`)
  },
  getTask: (id: number) => request<Task>(`/api/tasks/${id}`),
  getLogs: (id: number) => request<ExecutionLog[]>(`/api/tasks/${id}/logs`),
  createWorkflow: (input: { name: string; description: string; steps: Array<{ name: string; action_type: string; action_payload: Record<string, number> }> }) => request<Task>('/api/tasks', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(input),
  }),
  createAgent: (input: { goal: string; name?: string }) => request<Task>('/api/agent/tasks', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(input),
  }),
  runTask: (task: Pick<Task, 'id' | 'task_type'>) => request<RunResponse>(
    task.task_type === 'agent' ? `/api/agent/tasks/${task.id}/run` : `/api/tasks/${task.id}/run`, { method: 'POST' },
  ),
  importDocument: (file: File) => {
    const body = new FormData(); body.append('file', file)
    return request<ImportResult>('/api/knowledge/documents', { method: 'POST', body })
  },
  searchKnowledge: (query: string, topK: number) => request<SearchResponse>('/api/knowledge/search', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ query, top_k: topK }),
  }),
}
