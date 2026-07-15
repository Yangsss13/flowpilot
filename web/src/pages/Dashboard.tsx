import { ArrowRight, Bot, BookOpen, CheckCircle2, CircleDashed, ListTodo, LoaderCircle, TriangleAlert } from 'lucide-react'
import { Link } from 'react-router-dom'
import { EmptyState, ErrorState, LoadingState, PageHeader, Panel, TaskTable } from '../components'
import { useTasks } from '../hooks'
import type { Status } from '../types'

const cards: { status?: Status; label: string; icon: typeof ListTodo }[] = [
  { label: '全部任务', icon: ListTodo }, { status: 'Running', label: '运行中', icon: LoaderCircle },
  { status: 'Success', label: '已成功', icon: CheckCircle2 }, { status: 'Failed', label: '失败', icon: TriangleAlert },
  { status: 'Pending', label: '待运行', icon: CircleDashed },
]

export default function Dashboard() {
  const { tasks, loading, error, refresh } = useTasks()
  const recent = [...tasks].sort((a, b) => +new Date(b.created_at) - +new Date(a.created_at)).slice(0, 6)
  return <>
    <PageHeader eyebrow="CONTROL PLANE" title="工作流总览" description="从一个清晰的控制面，掌握 Agent 计划、任务执行与知识检索。" action={<Link className="button button-primary" to="/agent/new"><Bot size={17} />创建 Agent</Link>} />
    {loading ? <LoadingState /> : error ? <ErrorState message={error.message} onRetry={refresh} /> : <>
      <div className="metric-grid">{cards.map(({ status, label, icon: Icon }) => <Panel key={label} className={`metric-card ${status ? `metric-${status.toLowerCase()}` : ''}`}><span className="metric-icon"><Icon size={19} /></span><div><strong>{status ? tasks.filter(task => task.status === status).length : tasks.length}</strong><span>{label}</span></div></Panel>)}</div>
      <div className="dashboard-grid">
        <Panel className="recent-panel"><div className="panel-heading"><div><span className="eyebrow">RECENT RUNS</span><h2>最近任务</h2></div><Link to="/tasks">查看全部 <ArrowRight size={14} /></Link></div>
          {recent.length ? <TaskTable tasks={recent} /> : <EmptyState title="还没有任务" description="创建第一个 Agent，让 FlowPilot 为目标生成执行计划。" action={<Link className="button button-primary" to="/agent/new">开始创建</Link>} />}
        </Panel>
        <Panel className="quick-panel"><span className="eyebrow">QUICK START</span><h2>下一步操作</h2><p>从目标规划或知识导入开始，快速跑通一次完整演示。</p>
          <Link className="quick-action" to="/agent/new"><span><Bot size={20} /></span><div><strong>创建 Agent</strong><small>生成受约束的工具计划</small></div><ArrowRight size={16} /></Link>
          <Link className="quick-action" to="/knowledge"><span><BookOpen size={20} /></span><div><strong>导入知识</strong><small>上传 .txt / .md 资料</small></div><ArrowRight size={16} /></Link>
          <div className="guardrail"><span>安全边界</span><p>模型凭据只由后端环境变量读取，不会进入浏览器。</p></div>
        </Panel>
      </div>
    </>}
  </>
}
