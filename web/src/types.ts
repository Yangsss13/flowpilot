export type Status = 'Pending' | 'Queued' | 'Running' | 'Success' | 'Failed'
export type TaskType = 'workflow' | 'agent'
export type BackendConnection = 'checking' | 'online' | 'degraded' | 'offline'

export interface TaskStep {
  id: number
  task_id: number
  name: string
  step_order: number
  action_type: string
  action_payload: unknown
  depends_on?: unknown
  observation?: unknown
  status: Status
  created_at: string
  updated_at: string
}

export interface Task {
  id: number
  name: string
  description: string
  task_type: TaskType
  status: Status
  result?: string
  replan_count?: number
  step_count?: number
  steps?: TaskStep[]
  created_at: string
  updated_at: string
}

export interface TaskListResponse {
  items: Task[]
  total: number
  page: number
  page_size: number
}

export interface TaskStatsResponse {
  total: number
  by_status: Partial<Record<Status, number>>
  by_type: Partial<Record<TaskType, number>>
}

export interface ListTasksParams {
  page?: number
  pageSize?: number
  taskType?: TaskType
  status?: Status
  query?: string
}

export interface HealthResponse { status: 'ok' }
export interface ReadinessResponse {
  status: 'ready' | 'not_ready'
  checks: Record<string, 'ok' | 'unavailable'>
}

export interface CapabilityTool { name: 'rag_query' | 'http_request'; description: string }
export interface CapabilitiesResponse {
  agent_enabled: boolean
  tools: CapabilityTool[]
  knowledge_enabled: boolean
}

export interface ExecutionLog {
  id: number
  task_id: number
  step_id?: number
  level: 'INFO' | 'WARN' | 'ERROR'
  message: string
  created_at: string
}

export interface ImportResult {
  document_id: string
  source: string
  chunk_count: number
}

export interface SearchResult {
  document_id: string
  source: string
  chunk_index: number
  text: string
  score: number
}

export interface SearchResponse { results: SearchResult[] }
export interface RunResponse { task_id: number; status: 'queued' }
