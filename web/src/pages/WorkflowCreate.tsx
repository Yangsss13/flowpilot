import { ArrowDown, Database, FileOutput, FlaskConical, GitBranch, LoaderCircle, Play, Plus, Sparkles, Trash2 } from 'lucide-react'
import { FormEvent, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { ApiError, api } from '../api'
import { PageHeader, Panel, StatusBadge } from '../components'
import type { Task } from '../types'

type ActionType = 'rag_query' | 'llm_summarize' | 'sleep' | 'http_mock' | 'shell_mock'
interface DraftStep {
  id: number
  name: string
  actionType: ActionType
  value: number
  query: string
  topK: number
  minScore: number
  instruction: string
}

const defaultSummaryInstruction = '根据前面所有检索证据生成一份结构清晰的最终报告，保留来源、页码、幻灯片或时间轴引用；证据不足时明确说明。'
const mockOptions = {
  sleep: { label: '等待（测试）', field: '持续时间 (ms)', hint: '1-30000 毫秒', min: 1, max: 30000, description: '验证异步等待、状态更新和取消逻辑。' },
  http_mock: { label: '模拟 HTTP（测试）', field: '响应状态码', hint: '100-599', min: 100, max: 599, description: '用状态码验证成功或失败分支，不会发送网络请求。' },
  shell_mock: { label: '模拟 Shell（测试）', field: '退出码', hint: '0 表示成功，非 0 表示失败', min: 0, max: 255, description: '用退出码验证失败停止逻辑，不会运行系统命令。' },
} as const

function newStep(id: number, actionType: ActionType = 'sleep'): DraftStep {
  return {
    id, name: actionType === 'llm_summarize' ? '生成最终报告' : '', actionType,
    value: actionType === 'sleep' ? 1000 : actionType === 'http_mock' ? 200 : 0,
    query: '', topK: 3, minScore: 0.5, instruction: defaultSummaryInstruction,
  }
}

function payload(step: DraftStep): Record<string, unknown> {
  if (step.actionType === 'rag_query') return { query: step.query.trim(), top_k: step.topK, min_score: step.minScore }
  if (step.actionType === 'llm_summarize') return { instruction: step.instruction.trim() }
  if (step.actionType === 'sleep') return { duration_ms: step.value }
  if (step.actionType === 'http_mock') return { status: step.value }
  return { exit_code: step.value }
}

export default function WorkflowCreate() {
  const [name, setName] = useState(''); const [description, setDescription] = useState('')
  const [steps, setSteps] = useState<DraftStep[]>([newStep(1)])
  const [knowledgeEnabled, setKnowledgeEnabled] = useState<boolean | null>(null)
  const [summaryEnabled, setSummaryEnabled] = useState(false)
  const [created, setCreated] = useState<Task | null>(null); const [submitting, setSubmitting] = useState(false); const [running, setRunning] = useState(false); const [error, setError] = useState<string | null>(null)
  const navigate = useNavigate()

  useEffect(() => { void api.getCapabilities().then(value => {
    setKnowledgeEnabled(value.knowledge_enabled)
    setSummaryEnabled(value.workflow_summary_enabled)
    if (value.knowledge_enabled) setSteps(current => current.length === 1 && current[0].actionType === 'sleep' && !current[0].name && current[0].value === 1000 ? [{ ...current[0], actionType: 'rag_query' }] : current)
  }).catch(() => { setKnowledgeEnabled(false); setSummaryEnabled(false) }) }, [])

  const hasRAGStep = steps.some(step => step.actionType === 'rag_query')
  const hasSummaryStep = steps.some(step => step.actionType === 'llm_summarize')
  function nextID(current: DraftStep[]) { return Math.max(...current.map(step => step.id)) + 1 }
  function updateStep(id: number, update: Partial<DraftStep>) { setSteps(current => current.map(step => step.id === id ? { ...step, ...update } : step)) }
  function changeAction(step: DraftStep, actionType: ActionType) {
    updateStep(step.id, {
      actionType,
      value: actionType === 'sleep' ? 1000 : actionType === 'http_mock' ? 200 : 0,
      ...(actionType === 'llm_summarize' && !step.name ? { name: '生成最终报告' } : {}),
    })
  }
  function addStep() { setSteps(current => [...current, newStep(nextID(current), knowledgeEnabled ? 'rag_query' : 'sleep')]) }
  function addSummaryStep() { setSteps(current => [...current, newStep(nextID(current), 'llm_summarize')]) }
  function removeStep(id: number) { setSteps(current => current.filter(step => step.id !== id)) }
  async function submit(event: FormEvent) { event.preventDefault(); setSubmitting(true); setError(null)
    try { setCreated(await api.createWorkflow({ name: name.trim(), description: description.trim(), steps: steps.map(step => ({ name: step.name.trim(), action_type: step.actionType, action_payload: payload(step) })) })) }
    catch (err) { setError((err as ApiError).message) } finally { setSubmitting(false) }
  }
  async function run() { if (!created) return; setRunning(true); setError(null); try { await api.runTask(created); navigate(`/tasks/${created.id}`, { state: { submitted: true } }) } catch (err) { setError((err as ApiError).message); setRunning(false) } }

  return <><PageHeader eyebrow="WORKFLOW BUILDER" title="创建可交付结果的 Workflow" description="固定检索步骤收集证据，再由 AI 汇总为带来源引用的最终报告。" />
    <div className="workflow-layout"><Panel className="workflow-form"><form onSubmit={submit}>
      <div className="workflow-section"><h2>基本信息</h2><p>你决定查什么和执行顺序；AI 只负责最后汇总，不会擅自改变计划。</p></div>
      <div className="workflow-fields"><label>任务名称<input value={name} onChange={event => setName(event.target.value)} placeholder="例如：生成项目面试分析报告" disabled={!!created} required /></label><label>目标描述 <span>可选</span><textarea rows={3} value={description} onChange={event => setDescription(event.target.value)} placeholder="说明最终报告要解决什么问题" disabled={!!created} /></label></div>
      <div className="workflow-section workflow-step-heading"><div><h2>执行步骤</h2><p>先添加一个或多个知识库检索，最后添加 AI 汇总，产出真正的最终报告。</p></div>{!created && <div className="workflow-step-actions">{!hasSummaryStep && <button type="button" className="button button-secondary" onClick={addStep}><Plus size={15} />添加检索</button>}{summaryEnabled && hasRAGStep && !hasSummaryStep && <button type="button" className="button button-secondary workflow-summary-button" onClick={addSummaryStep}><Sparkles size={15} />添加 AI 汇总</button>}</div>}</div>
      <div className="workflow-steps">{steps.map((step, index) => { const mock = step.actionType === 'rag_query' || step.actionType === 'llm_summarize' ? null : mockOptions[step.actionType]; const isLast = index === steps.length - 1; const hasPriorRAG = steps.slice(0, index).some(previous => previous.actionType === 'rag_query'); return <article key={step.id} className={`workflow-step ${step.actionType === 'llm_summarize' ? 'workflow-summary-step' : ''}`}><div className="workflow-step-index">{index + 1}</div><div className="workflow-step-content"><div className="workflow-step-top"><label>步骤名称<input value={step.name} onChange={event => updateStep(step.id, { name: event.target.value })} placeholder={`步骤 ${index + 1} 名称`} disabled={!!created} required /></label><label>动作<select value={step.actionType} onChange={event => changeAction(step, event.target.value as ActionType)} disabled={!!created}>{knowledgeEnabled && <option value="rag_query">知识库检索</option>}{summaryEnabled && isLast && (hasPriorRAG || step.actionType === 'llm_summarize') && <option value="llm_summarize">AI 汇总报告</option>}<option value="sleep">等待（测试）</option><option value="http_mock">模拟 HTTP（测试）</option><option value="shell_mock">模拟 Shell（测试）</option></select></label>{steps.length > 1 && !created && <button type="button" className="workflow-delete" title="删除步骤" onClick={() => removeStep(step.id)}><Trash2 size={16} /></button>}</div>
        {step.actionType === 'rag_query' ? <div className="workflow-rag-fields"><label>查询内容 <span>必填</span><textarea rows={3} value={step.query} onChange={event => updateStep(step.id, { query: event.target.value })} placeholder="例如：FlowPilot 的核心后端链路是什么？" disabled={!!created} required /></label><label>返回数量 <span>1-10</span><input type="number" min={1} max={10} value={step.topK} onChange={event => updateStep(step.id, { topK: Number(event.target.value) })} disabled={!!created} required /></label><label>最低相似度 <span>0-1</span><input type="number" min={0} max={1} step={0.05} value={step.minScore} onChange={event => updateStep(step.id, { minScore: Number(event.target.value) })} disabled={!!created} required /></label></div> : step.actionType === 'llm_summarize' ? <div className="workflow-summary-fields"><label>报告要求 <span>AI 只能使用前序检索证据</span><textarea rows={4} value={step.instruction} onChange={event => updateStep(step.id, { instruction: event.target.value })} disabled={!!created} required /></label><div><FileOutput size={16} /><span>该步骤成功后，报告会同时保存为步骤 Observation 和任务最终结果。</span></div></div> : mock && <label>{mock.field}<span>{mock.hint}</span><input type="number" min={mock.min} max={mock.max} value={step.value} onChange={event => updateStep(step.id, { value: Number(event.target.value) })} disabled={!!created} required /></label>}
      </div>{index < steps.length - 1 && <ArrowDown className="workflow-step-arrow" size={16} />}</article> })}</div>
      {error && <div className="inline-error">{error}</div>}
      {!created ? <button className="button button-primary button-wide" disabled={submitting}>{submitting ? <><LoaderCircle size={17} className="spin" />正在创建…</> : <><GitBranch size={17} />创建 Workflow</>}</button> : <div className="workflow-created"><div><StatusBadge status={created.status} /><strong>Workflow #{created.id} 已创建</strong><span>{created.steps?.length ?? steps.length} 个步骤已通过能力和顺序校验</span></div><button type="button" className="button button-primary" onClick={run} disabled={running}>{running ? <LoaderCircle size={16} className="spin" /> : <Play size={16} />}{running ? '正在提交…' : '运行并生成报告'}</button></div>}
    </form></Panel>
    <aside className="workflow-guide"><h2>真实动作</h2><div className="workflow-real-action"><Database size={16} /><strong>知识库检索</strong><p>调用 Embedding 与 Qdrant，收集文档、PPT 或视频中的真实证据。</p>{knowledgeEnabled === false && <small>当前后端未启用知识能力。</small>}</div><div className="workflow-real-action workflow-summary-guide"><Sparkles size={16} /><strong>AI 汇总报告</strong><p>读取前面所有检索结果，按你的要求生成最终交付物，并保留来源引用。</p>{!summaryEnabled && <small>当前后端未同时启用知识库与 Chat 模型。</small>}</div><h2 className="workflow-test-heading">测试动作</h2>{Object.entries(mockOptions).map(([type, option]) => <div key={type}><FlaskConical size={15} /><strong>{option.label}</strong><p>{option.description}</p></div>)}<section><strong>确定性边界</strong><p>Workflow 的步骤和顺序由你固定；AI 不规划、不 replan，只在最后一步转换已有证据。</p></section></aside>
    </div></>
}
