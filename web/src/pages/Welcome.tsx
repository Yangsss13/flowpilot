import { ArrowRight, Bot, CheckCircle2, CircleAlert, LoaderCircle, TriangleAlert, Workflow } from 'lucide-react'
import type { BackendConnection, CapabilitiesResponse } from '../types'

interface WelcomeProps {
  onEnter: (destination: '/' | '/agent/new') => void
  connection: BackendConnection
  capabilities: CapabilitiesResponse | null
}

export default function Welcome({ onEnter, connection, capabilities }: WelcomeProps) {
  const status = {
    checking: { icon: LoaderCircle, label: '正在连接本地服务' },
    online: { icon: CheckCircle2, label: '后端服务已就绪' },
    degraded: { icon: TriangleAlert, label: '后端依赖未就绪' },
    offline: { icon: CircleAlert, label: '后端服务未连接' },
  }[connection]
  const StatusIcon = status.icon

  return <main className="welcome">
    <nav className="welcome-nav" aria-label="欢迎页导航">
      <div className="welcome-brand"><span><Workflow size={20} /></span><strong>FlowPilot</strong></div>
      <div className={`welcome-status welcome-status-${connection}`}><StatusIcon size={15} className={connection === 'checking' ? 'spin' : ''} />{status.label}</div>
    </nav>

    <section className="welcome-hero">
      <div className="welcome-copy">
        <p className="welcome-kicker">AI WORKFLOW CONTROL PLANE</p>
        <h1><span>把目标变成</span><span><em>可追踪</em>的执行计划</span></h1>
        <p className="welcome-lead">生成计划、调用工具、记录 Observation，并持续追踪每一次执行。</p>
        <div className="welcome-actions">
          {capabilities?.agent_enabled !== false && <button className="welcome-primary" onClick={() => onEnter('/agent/new')}><Bot size={18} />创建 Agent</button>}
          <button className="welcome-secondary" onClick={() => onEnter('/')}>进入控制台<ArrowRight size={17} /></button>
        </div>
        <dl className="welcome-capabilities">
          <div><dt>计划</dt><dd>结构化步骤与依赖</dd></div>
          <div><dt>执行</dt><dd>工具调用与 Observation</dd></div>
          <div><dt>追踪</dt><dd>状态、日志与最终答案</dd></div>
        </dl>
      </div>

      <div className="welcome-visual" aria-label="FlowPilot 真实任务详情预览">
        <div className="welcome-visual-label"><span>真实任务现场</span><strong>Workflow #1</strong></div>
        <div className="welcome-screenshot">
          <img src="/flowpilot-console.png" alt="FlowPilot 任务详情页面，展示执行计划、步骤输入和运行状态" />
        </div>
      </div>
    </section>

    <footer className="welcome-footer"><span>React + Go</span><p>计划生成、异步执行和知识检索均连接真实后端</p></footer>
  </main>
}
