import { AlertTriangle, ArrowLeft, Braces, Clock3, FileText, LoaderCircle, Play, RefreshCw, RotateCcw, TerminalSquare, Trash2, Wrench } from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'
import { ApiError, api } from '../api'
import { ErrorState, LoadingState, Panel, StatusBadge, TypeBadge } from '../components'
import { formatDate, parseArray, pretty } from '../hooks'
import type { ExecutionLog, Task } from '../types'

export default function TaskDetail() {
  const location = useLocation()
  const navigate = useNavigate()
  const id = Number(useParams().id); const [task, setTask] = useState<Task | null>(null); const [logs, setLogs] = useState<ExecutionLog[]>([]); const [loading, setLoading] = useState(true); const [running, setRunning] = useState(false); const [submitted, setSubmitted] = useState(Boolean((location.state as { submitted?: boolean } | null)?.submitted)); const [error, setError] = useState<string | null>(null); const [updated, setUpdated] = useState<Date | null>(null)
  const [deleteConfirm, setDeleteConfirm] = useState(false); const [deleting, setDeleting] = useState(false)
  const load = useCallback(async (quiet = false) => { if (!Number.isInteger(id) || id < 1) { setError('任务 ID 无效。'); setLoading(false); return }
    if (!quiet) setLoading(true)
    try { const [nextTask, nextLogs] = await Promise.all([api.getTask(id), api.getLogs(id)]); setTask(nextTask); setLogs(nextLogs); setError(null); setUpdated(new Date()) }
    catch (err) { const apiError = err as ApiError; setError(apiError.kind === 'not-found' ? '没有找到这个任务，它可能已被删除或 ID 不正确。' : apiError.message) }
    finally { if (!quiet) setLoading(false) }
  }, [id])
  useEffect(() => { const timer = window.setTimeout(() => void load(), 0); return () => window.clearTimeout(timer) }, [load])
  const activelyExecuting = task?.status === 'Queued' || task?.status === 'Running' || (submitted && task?.status === 'Pending')
  useEffect(() => { if (!activelyExecuting) return; const timer = window.setInterval(() => void load(true), 2000); return () => window.clearInterval(timer) }, [activelyExecuting, load])
  async function run() { if (!task) return; setRunning(true); setError(null); try { await api.runTask(task); setSubmitted(true); await load(true) } catch (err) { setError((err as ApiError).message) } finally { setRunning(false) } }
  async function deleteTask() { if (!task) return; setDeleting(true); setError(null); try { await api.deleteTask(task.id); navigate('/tasks', { replace: true }) } catch (err) { const apiError = err as ApiError; setError(apiError.kind === 'conflict' ? '排队或运行中的任务不能删除，请等待任务结束后重试。' : apiError.message); setDeleteConfirm(false) } finally { setDeleting(false) } }
  if (loading) return <LoadingState label="正在恢复任务现场" />
  if (error && !task) return <><Link className="back-link" to="/tasks"><ArrowLeft size={15} />返回任务中心</Link><ErrorState message={error} onRetry={() => void load()} /></>
  if (!task) return null
  return <><div className="detail-heading"><div><Link className="back-link" to="/tasks"><ArrowLeft size={15} />任务中心</Link><div className="title-row"><h1>{task.name}</h1><StatusBadge status={task.status} /></div><p>{task.description || '这个任务没有目标描述。'}</p><div className="detail-meta"><TypeBadge type={task.task_type} /><span>#{task.id}</span><span><Clock3 size={13} />{formatDate(task.created_at)}</span>{activelyExecuting && <span className="polling"><span className="live-dot" />{task.status === 'Queued' ? '已进入队列，自动更新' : '每 2 秒自动更新'}</span>}</div></div>
    <div className="detail-actions"><button className="button button-secondary" onClick={() => void load()}><RefreshCw size={16} />刷新</button>{!activelyExecuting && (deleteConfirm ? <button className="button button-danger" onClick={() => void deleteTask()} disabled={deleting}>{deleting ? <LoaderCircle className="spin" size={16} /> : <Trash2 size={16} />}确认删除</button> : <button className="button button-secondary button-delete" onClick={() => setDeleteConfirm(true)}><Trash2 size={16} />删除</button>)}{(task.status === 'Pending' || task.status === 'Failed') && <button className="button button-primary" onClick={run} disabled={running}>{running ? <LoaderCircle className="spin" size={16} /> : task.status === 'Failed' ? <RotateCcw size={16} /> : <Play size={16} />}{task.status === 'Failed' ? '重新运行' : '运行任务'}</button>}</div></div>
    {error && <div className="inline-error"><AlertTriangle size={16} />{error}</div>}
    <div className="summary-strip"><div><span>当前状态</span><strong>{task.status}</strong></div><div><span>执行步骤</span><strong>{task.steps?.length ?? 0}</strong></div><div><span>Replan 次数</span><strong>{task.replan_count ?? 0}<small> / 2</small></strong></div><div><span>最后同步</span><strong>{updated?.toLocaleTimeString('zh-CN', { hour12: false }) ?? '—'}</strong></div></div>
    <div className="detail-grid"><div className="detail-main"><Panel><div className="panel-heading"><div><span className="eyebrow">EXECUTION PLAN</span><h2>步骤时间线</h2></div><span className="muted">按计划顺序</span></div>
      <div className="step-timeline">{task.steps?.map((step, index) => { const dependencies = parseArray(step.depends_on); return <article key={step.id} className={`step-card step-${step.status.toLowerCase()}`}><div className="step-rail"><span>{index + 1}</span></div><div className="step-body"><div className="step-card-head"><div><strong>{step.name}</strong><code><Wrench size={12} />{step.action_type}</code></div><StatusBadge status={step.status} /></div>
        <div className="step-data"><div><span><Braces size={13} />输入</span><pre>{pretty(step.action_payload)}</pre></div>{dependencies.length > 0 && <div><span>依赖</span><div className="dependency-list">{dependencies.map(dep => <code key={dep}>{dep}</code>)}</div></div>}<div><span><FileText size={13} />Observation</span><pre>{pretty(step.observation)}</pre></div></div>
      </div></article> })}</div>
    </Panel></div><aside className="detail-side">{task.result && <Panel className="answer-panel"><span className="eyebrow">FINAL ANSWER</span><h2>最终答案</h2><div className="answer-content">{task.result}</div></Panel>}
      <Panel className="logs-panel"><div className="panel-heading"><div><span className="eyebrow">LIVE TRACE</span><h2>执行日志</h2></div><TerminalSquare size={18} /></div><div className="log-list">{logs.length ? logs.map(log => <div className={`log-line log-${log.level.toLowerCase()}`} key={log.id}><span>{new Date(log.created_at).toLocaleTimeString('zh-CN', { hour12: false })}</span><i>{log.level}</i><p>{log.message}</p></div>) : <div className="empty-logs">运行任务后，执行日志会显示在这里。</div>}</div></Panel>
    </aside></div>
  </>
}
