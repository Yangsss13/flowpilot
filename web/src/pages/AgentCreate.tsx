import { ArrowRight, Bot, Check, CircleDashed, LoaderCircle, Play, RotateCcw, Sparkles } from 'lucide-react'
import { FormEvent, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { ApiError, api } from '../api'
import { PageHeader, Panel, StatusBadge } from '../components'
import { parseArray, pretty } from '../hooks'
import type { Task } from '../types'

export default function AgentCreate() {
  const [goal, setGoal] = useState(''); const [name, setName] = useState(''); const [task, setTask] = useState<Task | null>(null)
  const [creating, setCreating] = useState(false); const [running, setRunning] = useState(false); const [error, setError] = useState<string | null>(null)
  const navigate = useNavigate()
  async function create(event: FormEvent) { event.preventDefault(); setCreating(true); setError(null)
    try { setTask(await api.createAgent({ goal: goal.trim(), ...(name.trim() ? { name: name.trim() } : {}) })) }
    catch (err) { const apiError = err as ApiError; setError(apiError.kind === 'not-found' ? 'Agent 能力尚未在后端启用，请配置后端 AI 模型后重启服务。' : apiError.message) }
    finally { setCreating(false) }
  }
  async function run() { if (!task) return; setRunning(true); setError(null); try { await api.runTask(task); navigate(`/tasks/${task.id}`) } catch (err) { setError((err as ApiError).message); setRunning(false) } }
  return <>
    <PageHeader eyebrow="AGENT STUDIO" title="创建一个执行 Agent" description="描述目标，后端模型会生成受工具白名单与依赖校验约束的真实计划。" />
    <div className="create-layout"><Panel className="composer-panel"><div className="panel-heading"><div><span className="step-label">01</span><h2>定义目标</h2></div><Sparkles size={19} /></div>
      <form onSubmit={create}><label>任务名称 <span>可选</span><input value={name} maxLength={100} onChange={e => setName(e.target.value)} placeholder="例如：梳理退款政策" disabled={creating || !!task} /></label>
        <label>Agent 目标 <span>{[...goal].length} / 500</span><textarea value={goal} maxLength={500} rows={8} onChange={e => setGoal(e.target.value)} placeholder="例如：根据已导入的知识资料，总结退款期限、适用条件和处理流程，并给出简明结论。" disabled={creating || !!task} required /></label>
        {error && <div className="inline-error">{error}</div>}
        {!task ? <button className="button button-primary button-wide" disabled={!goal.trim() || creating}>{creating ? <><LoaderCircle className="spin" size={17} />正在生成计划…</> : <><Bot size={17} />生成 Agent 计划</>}</button> : <button type="button" className="button button-secondary button-wide" onClick={() => { setTask(null); setError(null) }}><RotateCcw size={16} />重新定义</button>}
      </form><div className="process-note"><CircleDashed size={16} /><span>计划最多 5 步，只允许后端注册的 <code>rag_query</code> 与白名单 <code>http_request</code>。</span></div>
    </Panel>
    <Panel className="plan-panel"><div className="panel-heading"><div><span className="step-label">02</span><h2>检查并运行</h2></div>{task && <StatusBadge status={task.status} />}</div>
      {!task ? <div className="plan-placeholder"><div className="orbit"><Bot size={27} /></div><strong>{creating ? '正在规划步骤' : '计划将在这里生成'}</strong><p>{creating ? 'AI 正在将目标转换为可验证的工具调用与依赖关系。' : '提交目标后，你可以先检查每个步骤，再决定是否运行。'}</p></div> : <>
        <div className="plan-summary"><div><Check size={17} /><span>计划已通过后端校验</span></div><strong>{task.steps?.length ?? 0} 个步骤</strong></div>
        <div className="compact-timeline">{task.steps?.map((step, index) => <article key={step.id}><span className="timeline-index">{String(index + 1).padStart(2, '0')}</span><div><div className="step-title"><strong>{step.name}</strong><code>{step.action_type}</code></div><pre>{pretty(step.action_payload)}</pre>{parseArray(step.depends_on).length > 0 && <small>依赖：{parseArray(step.depends_on).join('、')}</small>}</div></article>)}</div>
        <button className="button button-primary button-wide" onClick={run} disabled={running}>{running ? <><LoaderCircle className="spin" size={17} />正在提交…</> : <><Play size={17} />运行此 Agent</>}</button><Link className="text-link" to={`/tasks/${task.id}`}>先查看任务详情 <ArrowRight size={14} /></Link>
      </>}
    </Panel></div>
  </>
}
