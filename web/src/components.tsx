import type { ReactNode } from 'react'
import { AlertTriangle, Bot, Boxes, CheckCircle2, CircleDashed, LoaderCircle, RotateCw, Workflow } from 'lucide-react'
import { Link } from 'react-router-dom'
import type { Status, Task, TaskType } from './types'
import { formatDate } from './hooks'

export function StatusBadge({ status }: { status: Status }) {
  const icons = { Pending: CircleDashed, Running: LoaderCircle, Success: CheckCircle2, Failed: AlertTriangle }
  const Icon = icons[status]
  return <span className={`status status-${status.toLowerCase()}`}><Icon size={13} className={status === 'Running' ? 'spin' : ''} />{status}</span>
}

export function TypeBadge({ type }: { type: TaskType }) {
  return <span className={`type type-${type}`}>{type === 'agent' ? <Bot size={13} /> : <Workflow size={13} />}{type === 'agent' ? 'Agent' : 'Workflow'}</span>
}

export function PageHeader({ eyebrow, title, description, action }: { eyebrow: string; title: string; description: string; action?: ReactNode }) {
  return <header className="page-header"><div><span className="eyebrow">{eyebrow}</span><h1>{title}</h1><p>{description}</p></div>{action}</header>
}

export function Panel({ children, className = '' }: { children: ReactNode; className?: string }) {
  return <section className={`panel ${className}`}>{children}</section>
}

export function LoadingState({ label = '正在同步数据' }: { label?: string }) {
  return <div className="state-box"><LoaderCircle className="spin" size={24} /><strong>{label}</strong><span>FlowPilot 正在读取最新状态</span></div>
}

export function ErrorState({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return <div className="state-box error-state"><AlertTriangle size={25} /><strong>暂时无法加载</strong><span>{message}</span>{onRetry && <button className="button button-secondary" onClick={onRetry}><RotateCw size={15} />重新连接</button>}</div>
}

export function EmptyState({ title, description, action }: { title: string; description: string; action?: ReactNode }) {
  return <div className="state-box"><Boxes size={26} /><strong>{title}</strong><span>{description}</span>{action}</div>
}

export function TaskTable({ tasks }: { tasks: Task[] }) {
  return <div className="table-wrap"><table><thead><tr><th>任务</th><th>类型</th><th>状态</th><th>步骤</th><th>创建时间</th><th></th></tr></thead><tbody>{tasks.map(task => <tr key={task.id}>
    <td><Link className="task-name" to={`/tasks/${task.id}`}>{task.name}<small>#{task.id} · {task.description || '暂无目标描述'}</small></Link></td>
    <td><TypeBadge type={task.task_type} /></td><td><StatusBadge status={task.status} /></td><td>{task.steps?.length ?? '—'}</td><td>{formatDate(task.created_at)}</td><td><Link className="row-link" to={`/tasks/${task.id}`}>查看 →</Link></td>
  </tr>)}</tbody></table></div>
}
