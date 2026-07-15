import { useCallback, useEffect, useState } from 'react'
import { ApiError, api } from './api'
import type { ListTasksParams, Task, TaskStatsResponse } from './types'

export function useTasks(params: ListTasksParams = {}) {
  const { page = 1, pageSize = 100, taskType, status, query } = params
  const [tasks, setTasks] = useState<Task[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<ApiError | null>(null)
  const refresh = useCallback(async () => {
    setLoading(true); setError(null)
    try { const response = await api.listTasks({ page, pageSize, taskType, status, query }); setTasks(response.items); setTotal(response.total) }
    catch (err) { setError(err as ApiError) }
    finally { setLoading(false) }
  }, [page, pageSize, taskType, status, query])
  useEffect(() => { const timer = window.setTimeout(() => void refresh(), 0); return () => window.clearTimeout(timer) }, [refresh])
  return { tasks, total, loading, error, refresh }
}

export function useTaskStats() {
  const [stats, setStats] = useState<TaskStatsResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<ApiError | null>(null)
  const refresh = useCallback(async () => {
    setLoading(true); setError(null)
    try { setStats(await api.getTaskStats()) }
    catch (err) { setError(err as ApiError) }
    finally { setLoading(false) }
  }, [])
  useEffect(() => { const timer = window.setTimeout(() => void refresh(), 0); return () => window.clearTimeout(timer) }, [refresh])
  return { stats, loading, error, refresh }
}

export function formatDate(value: string) {
  return new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false }).format(new Date(value))
}

export function parseArray(value: unknown): string[] {
  if (Array.isArray(value)) return value.map(String)
  if (typeof value === 'string') {
    try { const parsed: unknown = JSON.parse(value); return Array.isArray(parsed) ? parsed.map(String) : [] } catch { return [] }
  }
  return []
}

export function pretty(value: unknown) {
  if (value == null || value === '') return '—'
  if (typeof value === 'string') {
    try { return JSON.stringify(JSON.parse(value), null, 2) } catch { return value }
  }
  return JSON.stringify(value, null, 2)
}
