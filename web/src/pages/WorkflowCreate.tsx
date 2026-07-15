import { ArrowDown, GitBranch, LoaderCircle, Play, Plus, Trash2 } from 'lucide-react'
import { FormEvent, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { ApiError, api } from '../api'
import { PageHeader, Panel, StatusBadge } from '../components'
import type { Task } from '../types'

type ActionType = 'sleep' | 'http_mock' | 'shell_mock'
interface DraftStep { id: number; name: string; actionType: ActionType; value: number }

const actionOptions: Record<ActionType, { label: string; field: string; hint: string; min: number; max: number }> = {
  sleep: { label: '等待', field: '持续时间 (ms)', hint: '1-30000 毫秒', min: 1, max: 30000 },
  http_mock: { label: '模拟 HTTP', field: '响应状态码', hint: '100-599', min: 100, max: 599 },
  shell_mock: { label: '模拟 Shell', field: '退出码', hint: '0 表示成功，非 0 表示失败', min: -255, max: 255 },
}

function defaultValue(actionType: ActionType) {
  return actionType === 'sleep' ? 1000 : actionType === 'http_mock' ? 200 : 0
}

export default function WorkflowCreate() {
  const [name, setName] = useState(''); const [description, setDescription] = useState('')
  const [steps, setSteps] = useState<DraftStep[]>([{ id: 1, name: '', actionType: 'sleep', value: 1000 }])
  const [created, setCreated] = useState<Task | null>(null); const [submitting, setSubmitting] = useState(false); const [running, setRunning] = useState(false); const [error, setError] = useState<string | null>(null)
  const navigate = useNavigate()

  function updateStep(id: number, update: Partial<DraftStep>) { setSteps(current => current.map(step => step.id === id ? { ...step, ...update } : step)) }
  function changeAction(step: DraftStep, actionType: ActionType) { updateStep(step.id, { actionType, value: defaultValue(actionType) }) }
  function addStep() { setSteps(current => [...current, { id: Math.max(...current.map(step => step.id)) + 1, name: '', actionType: 'sleep', value: 1000 }]) }
  function removeStep(id: number) { setSteps(current => current.filter(step => step.id !== id)) }
  function payload(step: DraftStep): Record<string, number> {
    if (step.actionType === 'sleep') return { duration_ms: step.value }
    if (step.actionType === 'http_mock') return { status: step.value }
    return { exit_code: step.value }
  }
  async function submit(event: FormEvent) { event.preventDefault(); setSubmitting(true); setError(null)
    try { setCreated(await api.createWorkflow({ name: name.trim(), description: description.trim(), steps: steps.map(step => ({ name: step.name.trim(), action_type: step.actionType, action_payload: payload(step) })) })) }
    catch (err) { setError((err as ApiError).message) } finally { setSubmitting(false) }
  }
  async function run() { if (!created) return; setRunning(true); setError(null); try { await api.runTask(created); navigate(`/tasks/${created.id}`, { state: { submitted: true } }) } catch (err) { setError((err as ApiError).message); setRunning(false) } }

  return <><PageHeader eyebrow="WORKFLOW BUILDER" title="创建 Workflow" description="配置确定性的顺序步骤，由后端校验并通过真实异步执行链路运行。" />
    <div className="workflow-layout"><Panel className="workflow-form"><form onSubmit={submit}>
      <div className="workflow-section"><h2>基本信息</h2><p>Workflow 按顺序执行，任一步失败后立即停止。</p></div>
      <div className="workflow-fields"><label>任务名称<input value={name} onChange={event => setName(event.target.value)} placeholder="例如：日报生成流程" disabled={!!created} required /></label><label>目标描述 <span>可选</span><textarea rows={3} value={description} onChange={event => setDescription(event.target.value)} placeholder="说明这个工作流要完成什么" disabled={!!created} /></label></div>
      <div className="workflow-section workflow-step-heading"><div><h2>执行步骤</h2><p>当前后端支持等待、模拟 HTTP 和模拟 Shell。</p></div>{!created && <button type="button" className="button button-secondary" onClick={addStep}><Plus size={15} />添加步骤</button>}</div>
      <div className="workflow-steps">{steps.map((step, index) => { const option = actionOptions[step.actionType]; return <article key={step.id} className="workflow-step"><div className="workflow-step-index">{index + 1}</div><div className="workflow-step-content"><div className="workflow-step-top"><label>步骤名称<input value={step.name} onChange={event => updateStep(step.id, { name: event.target.value })} placeholder={`步骤 ${index + 1} 名称`} disabled={!!created} required /></label><label>动作<select value={step.actionType} onChange={event => changeAction(step, event.target.value as ActionType)} disabled={!!created}><option value="sleep">等待</option><option value="http_mock">模拟 HTTP</option><option value="shell_mock">模拟 Shell</option></select></label>{steps.length > 1 && !created && <button type="button" className="workflow-delete" title="删除步骤" onClick={() => removeStep(step.id)}><Trash2 size={16} /></button>}</div><label>{option.field}<span>{option.hint}</span><input type="number" min={option.min} max={option.max} value={step.value} onChange={event => updateStep(step.id, { value: Number(event.target.value) })} disabled={!!created} required /></label></div>{index < steps.length - 1 && <ArrowDown className="workflow-step-arrow" size={16} />}</article> })}</div>
      {error && <div className="inline-error">{error}</div>}
      {!created ? <button className="button button-primary button-wide" disabled={submitting}>{submitting ? <><LoaderCircle size={17} className="spin" />正在创建…</> : <><GitBranch size={17} />创建 Workflow</>}</button> : <div className="workflow-created"><div><StatusBadge status={created.status} /><strong>Workflow #{created.id} 已创建</strong><span>{created.steps?.length ?? steps.length} 个步骤已通过后端校验并持久化</span></div><button type="button" className="button button-primary" onClick={run} disabled={running}>{running ? <LoaderCircle size={16} className="spin" /> : <Play size={16} />}{running ? '正在提交…' : '运行 Workflow'}</button></div>}
    </form></Panel>
    <aside className="workflow-guide"><h2>可用动作</h2>{(Object.entries(actionOptions) as Array<[ActionType, typeof actionOptions[ActionType]]>).map(([type, option]) => <div key={type}><code>{type}</code><strong>{option.label}</strong><p>{type === 'sleep' ? '在当前步骤等待指定毫秒数。' : type === 'http_mock' ? '以指定 HTTP 状态码模拟一次请求。' : '以指定退出码模拟一次 Shell 执行。'}</p></div>)}<section><strong>真实执行链路</strong><p>创建后由 MySQL 保存任务与步骤；运行时经过 RabbitMQ、WorkerPool、Redis 执行锁和状态机。</p></section></aside>
    </div></>
}
