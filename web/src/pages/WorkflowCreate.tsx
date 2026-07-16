import { ArrowDown, Database, FlaskConical, GitBranch, LoaderCircle, Play, Plus, Trash2 } from 'lucide-react'
import { FormEvent, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { ApiError, api } from '../api'
import { PageHeader, Panel, StatusBadge } from '../components'
import type { Task } from '../types'

type ActionType = 'rag_query' | 'sleep' | 'http_mock' | 'shell_mock'
interface DraftStep {
  id: number
  name: string
  actionType: ActionType
  value: number
  query: string
  topK: number
  minScore: number
}

const mockOptions = {
  sleep: { label: '等待（测试）', field: '持续时间 (ms)', hint: '1-30000 毫秒', min: 1, max: 30000, description: '验证异步等待、状态更新和取消逻辑。' },
  http_mock: { label: '模拟 HTTP（测试）', field: '响应状态码', hint: '100-599', min: 100, max: 599, description: '用状态码验证成功或失败分支，不会发送网络请求。' },
  shell_mock: { label: '模拟 Shell（测试）', field: '退出码', hint: '0 表示成功，非 0 表示失败', min: 0, max: 255, description: '用退出码验证失败停止逻辑，不会运行系统命令。' },
} as const

function newStep(id: number): DraftStep {
  return { id, name: '', actionType: 'sleep', value: 1000, query: '', topK: 5, minScore: 0.5 }
}

function defaultValue(actionType: ActionType) {
  return actionType === 'sleep' ? 1000 : actionType === 'http_mock' ? 200 : 0
}

function payload(step: DraftStep): Record<string, unknown> {
  if (step.actionType === 'rag_query') return { query: step.query.trim(), top_k: step.topK, min_score: step.minScore }
  if (step.actionType === 'sleep') return { duration_ms: step.value }
  if (step.actionType === 'http_mock') return { status: step.value }
  return { exit_code: step.value }
}

export default function WorkflowCreate() {
  const [name, setName] = useState(''); const [description, setDescription] = useState('')
  const [steps, setSteps] = useState<DraftStep[]>([newStep(1)])
  const [knowledgeEnabled, setKnowledgeEnabled] = useState<boolean | null>(null)
  const [created, setCreated] = useState<Task | null>(null); const [submitting, setSubmitting] = useState(false); const [running, setRunning] = useState(false); const [error, setError] = useState<string | null>(null)
  const navigate = useNavigate()

  useEffect(() => { void api.getCapabilities().then(value => {
    setKnowledgeEnabled(value.knowledge_enabled)
    if (value.knowledge_enabled) setSteps(current => current.length === 1 && current[0].actionType === 'sleep' && !current[0].name && current[0].value === 1000 ? [{ ...current[0], actionType: 'rag_query' }] : current)
  }).catch(() => setKnowledgeEnabled(false)) }, [])
  function updateStep(id: number, update: Partial<DraftStep>) { setSteps(current => current.map(step => step.id === id ? { ...step, ...update } : step)) }
  function changeAction(step: DraftStep, actionType: ActionType) { updateStep(step.id, { actionType, value: defaultValue(actionType) }) }
  function addStep() { setSteps(current => [...current, newStep(Math.max(...current.map(step => step.id)) + 1)]) }
  function removeStep(id: number) { setSteps(current => current.filter(step => step.id !== id)) }
  async function submit(event: FormEvent) { event.preventDefault(); setSubmitting(true); setError(null)
    try { setCreated(await api.createWorkflow({ name: name.trim(), description: description.trim(), steps: steps.map(step => ({ name: step.name.trim(), action_type: step.actionType, action_payload: payload(step) })) })) }
    catch (err) { setError((err as ApiError).message) } finally { setSubmitting(false) }
  }
  async function run() { if (!created) return; setRunning(true); setError(null); try { await api.runTask(created); navigate(`/tasks/${created.id}`, { state: { submitted: true } }) } catch (err) { setError((err as ApiError).message); setRunning(false) } }

  return <><PageHeader eyebrow="WORKFLOW BUILDER" title="创建 Workflow" description="你固定步骤和顺序，后端通过队列与状态机可靠执行，并保存每一步的真实结果。" />
    <div className="workflow-layout"><Panel className="workflow-form"><form onSubmit={submit}>
      <div className="workflow-section"><h2>基本信息</h2><p>Workflow 按顺序执行，任一步失败后立即停止；它不会像 Agent 一样自行改变计划。</p></div>
      <div className="workflow-fields"><label>任务名称<input value={name} onChange={event => setName(event.target.value)} placeholder="例如：从知识库提取项目面试要点" disabled={!!created} required /></label><label>目标描述 <span>可选</span><textarea rows={3} value={description} onChange={event => setDescription(event.target.value)} placeholder="说明这个工作流要完成什么" disabled={!!created} /></label></div>
      <div className="workflow-section workflow-step-heading"><div><h2>执行步骤</h2><p>知识库检索是真实业务动作；等待、模拟 HTTP 和模拟 Shell 仅用于测试执行引擎。</p></div>{!created && <button type="button" className="button button-secondary" onClick={addStep}><Plus size={15} />添加步骤</button>}</div>
      <div className="workflow-steps">{steps.map((step, index) => { const mock = step.actionType === 'rag_query' ? null : mockOptions[step.actionType]; return <article key={step.id} className="workflow-step"><div className="workflow-step-index">{index + 1}</div><div className="workflow-step-content"><div className="workflow-step-top"><label>步骤名称<input value={step.name} onChange={event => updateStep(step.id, { name: event.target.value })} placeholder={`步骤 ${index + 1} 名称`} disabled={!!created} required /></label><label>动作<select value={step.actionType} onChange={event => changeAction(step, event.target.value as ActionType)} disabled={!!created}>{knowledgeEnabled && <option value="rag_query">知识库检索</option>}<option value="sleep">等待（测试）</option><option value="http_mock">模拟 HTTP（测试）</option><option value="shell_mock">模拟 Shell（测试）</option></select></label>{steps.length > 1 && !created && <button type="button" className="workflow-delete" title="删除步骤" onClick={() => removeStep(step.id)}><Trash2 size={16} /></button>}</div>
        {step.actionType === 'rag_query' ? <div className="workflow-rag-fields"><label>查询内容 <span>必填</span><textarea rows={3} value={step.query} onChange={event => updateStep(step.id, { query: event.target.value })} placeholder="例如：FlowPilot 的核心后端链路是什么？" disabled={!!created} required /></label><label>返回数量 <span>1-10</span><input type="number" min={1} max={10} value={step.topK} onChange={event => updateStep(step.id, { topK: Number(event.target.value) })} disabled={!!created} required /></label><label>最低相似度 <span>0-1</span><input type="number" min={0} max={1} step={0.05} value={step.minScore} onChange={event => updateStep(step.id, { minScore: Number(event.target.value) })} disabled={!!created} required /></label></div> : mock && <label>{mock.field}<span>{mock.hint}</span><input type="number" min={mock.min} max={mock.max} value={step.value} onChange={event => updateStep(step.id, { value: Number(event.target.value) })} disabled={!!created} required /></label>}
      </div>{index < steps.length - 1 && <ArrowDown className="workflow-step-arrow" size={16} />}</article> })}</div>
      {error && <div className="inline-error">{error}</div>}
      {!created ? <button className="button button-primary button-wide" disabled={submitting}>{submitting ? <><LoaderCircle size={17} className="spin" />正在创建…</> : <><GitBranch size={17} />创建 Workflow</>}</button> : <div className="workflow-created"><div><StatusBadge status={created.status} /><strong>Workflow #{created.id} 已创建</strong><span>{created.steps?.length ?? steps.length} 个步骤已通过后端能力校验并持久化</span></div><button type="button" className="button button-primary" onClick={run} disabled={running}>{running ? <LoaderCircle size={16} className="spin" /> : <Play size={16} />}{running ? '正在提交…' : '运行 Workflow'}</button></div>}
    </form></Panel>
    <aside className="workflow-guide"><h2>动作分组</h2><div className="workflow-real-action"><Database size={16} /><strong>知识库检索</strong><p>真实调用 Embedding 与 Qdrant，返回文档片段、相似度、页码、幻灯片或媒体时间轴。</p>{knowledgeEnabled === false && <small>当前后端未启用知识能力，因此创建页不会提供此动作。</small>}</div>{Object.entries(mockOptions).map(([type, option]) => <div key={type}><FlaskConical size={15} /><strong>{option.label}</strong><p>{option.description}</p></div>)}<section><strong>真实执行链路</strong><p>MySQL 保存任务和 Observation；RabbitMQ 负责投递；WorkerPool 控制并发；Redis 锁避免同一任务被并发执行。</p></section></aside>
    </div></>
}
