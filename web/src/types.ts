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
export interface KnowledgeCapability {
  async_ingestion: boolean
  media_ingestion: boolean
  supported_formats: string[]
  max_bytes_by_format: Record<string, number>
  max_media_duration_seconds?: number
}
export interface CapabilitiesResponse {
  agent_enabled: boolean
  tools: CapabilityTool[]
  knowledge_enabled: boolean
  knowledge?: KnowledgeCapability
}

export interface ExecutionLog {
  id: number
  task_id: number
  step_id?: number
  level: 'INFO' | 'WARN' | 'ERROR'
  message: string
  created_at: string
}

export type DocumentStatus = 'Queued' | 'Processing' | 'Ready' | 'Failed' | 'Deleting'
export type IngestionJobStatus = 'Queued' | 'Running' | 'Success' | 'Failed' | 'Canceled'
export type IngestionStage = 'upload' | 'probe' | 'extract_audio' | 'transcribe' | 'keyframes' | 'ocr' | 'merge' | 'parse' | 'chunk' | 'embedding' | 'indexing'

export interface KnowledgeUploadResponse {
  document_id: number
  version_id: number
  job_id: number
  status: IngestionJobStatus
  deduplicated: boolean
}

export interface IngestionJob {
  id: number
  document_id: number
  version_id: number
  status: IngestionJobStatus
  stage: IngestionStage
  progress: number
  safe_error_code?: string
  safe_error_message?: string
  retry_count: number
  created_at: string
  updated_at: string
  started_at?: string
  finished_at?: string
  cancel_requested_at?: string
}

export interface KnowledgeDocument {
  id: number
  filename: string
  media_type: string
  size_bytes: number
  checksum: string
  status: DocumentStatus
  current_version: number
  created_at: string
  updated_at: string
}

export interface DocumentVersion {
  id: number
  document_id: number
  version: number
  filename: string
  media_type: string
  size_bytes: number
  checksum: string
  parser_version: string
  chunk_count: number
  created_at: string
}

export interface KnowledgeDocumentDetail {
  document: KnowledgeDocument
  current_version?: DocumentVersion
  latest_job?: IngestionJob
}

export interface KnowledgeDocumentListResponse {
  items: KnowledgeDocument[]
  total: number
  page: number
  page_size: number
}

export interface DeleteKnowledgeDocumentResponse { document_id: number; status: 'Deleting' }

export interface SearchResult {
  document_id: string
  version_id?: number
  source: string
  section?: string
  page?: number
  slide?: number
  start_ms?: number
  end_ms?: number
  start_time?: string
  end_time?: string
  chunk_index: number
  text: string
  score: number
}

export interface SearchResponse { results: SearchResult[] }
export interface RunResponse { task_id: number; status: 'queued' }
