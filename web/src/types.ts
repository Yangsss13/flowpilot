export type Status = 'Pending' | 'Running' | 'Success' | 'Failed'
export type TaskType = 'workflow' | 'agent'

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
  steps?: TaskStep[]
  created_at: string
  updated_at: string
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
export interface RunResponse { task_id: number; status: 'accepted' }
