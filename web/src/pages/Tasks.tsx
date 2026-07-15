import { Filter, Plus, RefreshCw, Search } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { EmptyState, ErrorState, LoadingState, PageHeader, Panel, TaskTable } from '../components'
import { useTasks } from '../hooks'
import type { Status, TaskType } from '../types'

export default function Tasks() {
  const { tasks, total, loading, error, refresh } = useTasks(); const [query, setQuery] = useState(''); const [type, setType] = useState<TaskType | 'all'>('all'); const [status, setStatus] = useState<Status | 'all'>('all')
  const filtered = useMemo(() => tasks.filter(task => (type === 'all' || task.task_type === type) && (status === 'all' || task.status === status) && `${task.name} ${task.description} ${task.id}`.toLowerCase().includes(query.toLowerCase())), [tasks, query, type, status])
  return <><PageHeader eyebrow="EXECUTION HUB" title="任务中心" description="统一查看 Workflow 与 Agent，按类型、状态快速定位每一次执行。" action={<Link className="button button-primary" to="/agent/new"><Plus size={17} />创建 Agent</Link>} />
    <Panel><div className="toolbar"><label className="search-field"><Search size={16} /><input value={query} onChange={e => setQuery(e.target.value)} placeholder="搜索名称、目标或 ID" /></label><div className="filter-group"><Filter size={15} /><select value={type} onChange={e => setType(e.target.value as TaskType | 'all')}><option value="all">全部类型</option><option value="agent">Agent</option><option value="workflow">Workflow</option></select><select value={status} onChange={e => setStatus(e.target.value as Status | 'all')}><option value="all">全部状态</option><option>Pending</option><option>Queued</option><option>Running</option><option>Success</option><option>Failed</option></select><button className="icon-button" title="刷新" onClick={refresh}><RefreshCw size={16} /></button></div></div>
      <div className="result-meta"><span>{query || type !== 'all' || status !== 'all' ? `当前匹配 ${filtered.length} 项` : `显示 ${tasks.length} / 共 ${total} 项`}</span>{(query || type !== 'all' || status !== 'all') && <button onClick={() => { setQuery(''); setType('all'); setStatus('all') }}>清除筛选</button>}</div>
      {loading ? <LoadingState /> : error ? <ErrorState message={error.message} onRetry={refresh} /> : filtered.length ? <TaskTable tasks={filtered} /> : <EmptyState title="没有匹配的任务" description={tasks.length ? '尝试调整筛选条件或搜索词。' : '创建第一个 Agent 后，任务会出现在这里。'} />}
    </Panel></>
}
