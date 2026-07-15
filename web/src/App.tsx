import { Bot, BookOpen, CheckCircle2, Command, GitBranch, LayoutDashboard, ListTodo, LoaderCircle, Menu, TriangleAlert, WifiOff, X } from 'lucide-react'
import { useEffect, useState } from 'react'
import { NavLink, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import AgentCreate from './pages/AgentCreate'
import Dashboard from './pages/Dashboard'
import Knowledge from './pages/Knowledge'
import TaskDetail from './pages/TaskDetail'
import Tasks from './pages/Tasks'
import Welcome from './pages/Welcome'
import WorkflowCreate from './pages/WorkflowCreate'
import { api } from './api'
import type { BackendConnection, CapabilitiesResponse } from './types'

const nav = [
  { to: '/', label: '总览', icon: LayoutDashboard, end: true },
  { to: '/agent/new', label: '创建 Agent', icon: Bot },
  { to: '/workflow/new', label: '创建 Workflow', icon: GitBranch },
  { to: '/tasks', label: '任务中心', icon: ListTodo },
  { to: '/knowledge', label: '知识库', icon: BookOpen },
]

export default function App() {
  const [open, setOpen] = useState(false)
  const [splash, setSplash] = useState(true)
  const [connection, setConnection] = useState<BackendConnection>('checking')
  const [capabilities, setCapabilities] = useState<CapabilitiesResponse | null>(null)
  const [welcomed, setWelcomed] = useState(() => sessionStorage.getItem('flowpilot-welcomed') === 'true' || window.location.pathname !== '/')
  const location = useLocation()
  const navigate = useNavigate()
  const current = nav.find(item => item.end ? location.pathname === '/' : location.pathname.startsWith(item.to))?.label ?? '任务详情'
  const isWelcome = location.pathname === '/' && !welcomed

  useEffect(() => {
    const timer = window.setTimeout(() => setSplash(false), 1100)
    return () => window.clearTimeout(timer)
  }, [])

  useEffect(() => {
    let active = true
    async function probe() {
      try {
        await api.checkBackend()
        if (!active) return
        try {
          const readiness = await api.checkReadiness()
          if (active) setConnection(readiness.status === 'ready' ? 'online' : 'degraded')
        } catch { if (active) setConnection('degraded') }
      } catch { if (active) setConnection('offline') }
    }
    void probe()
    api.getCapabilities().then(value => { if (active) setCapabilities(value) }).catch(() => undefined)
    return () => { active = false }
  }, [])

  function enter(destination: '/' | '/agent/new') {
    sessionStorage.setItem('flowpilot-welcomed', 'true')
    setWelcomed(true)
    navigate(destination)
  }

  const connectionLabel = connection === 'online' ? '后端已就绪' : connection === 'degraded' ? '依赖未就绪' : connection === 'offline' ? '后端未连接' : '正在连接'
  const ConnectionIcon = connection === 'online' ? CheckCircle2 : connection === 'degraded' ? TriangleAlert : connection === 'offline' ? WifiOff : LoaderCircle
  const visibleNav = nav.filter(item => {
    if (item.to === '/agent/new') return capabilities?.agent_enabled !== false
    if (item.to === '/knowledge') return capabilities?.knowledge_enabled !== false
    return true
  })
  const splashLayer = splash && <div className="brand-splash" role="status" aria-label="FlowPilot 正在启动"><div className="brand-splash-mark"><Command size={30} /></div><strong>FlowPilot</strong><span>AI Workflow Console</span></div>

  if (isWelcome) return <>{splashLayer}<Welcome onEnter={enter} connection={connection} capabilities={capabilities} /></>

  return <div className="app-shell">{splashLayer}
    <aside className={`sidebar ${open ? 'sidebar-open' : ''}`}>
      <div className="brand"><span className="brand-mark"><Command size={21} /></span><span><strong>FlowPilot</strong><small>AI Workflow Console</small></span></div>
      <nav>{visibleNav.map(({ to, label, icon: Icon, end }) => <NavLink key={to} to={to} end={end} onClick={() => setOpen(false)}><Icon size={18} />{label}</NavLink>)}</nav>
      <div className={`sidebar-meta connection-${connection}`}><ConnectionIcon size={15} className={connection === 'checking' ? 'spin' : ''} />{connectionLabel}<small>真实后端 · API /api</small></div>
    </aside>
    {open && <button className="scrim" aria-label="关闭导航" onClick={() => setOpen(false)} />}
    <div className="main-column">
      <div className="topbar"><button className="menu-button" aria-label={open ? '关闭导航' : '打开导航'} onClick={() => setOpen(!open)}>{open ? <X /> : <Menu />}</button><span>{current}</span><div className={`topbar-status connection-${connection}`}><ConnectionIcon size={14} className={connection === 'checking' ? 'spin' : ''} />{connectionLabel}</div></div>
      <main><Routes><Route path="/" element={<Dashboard capabilities={capabilities} />} /><Route path="/agent/new" element={<AgentCreate capabilities={capabilities} />} /><Route path="/workflow/new" element={<WorkflowCreate />} /><Route path="/tasks" element={<Tasks />} /><Route path="/tasks/:id" element={<TaskDetail />} /><Route path="/knowledge" element={<Knowledge enabled={capabilities?.knowledge_enabled} />} /></Routes></main>
    </div>
  </div>
}
