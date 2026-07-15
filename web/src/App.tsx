import { Bot, BookOpen, Command, LayoutDashboard, ListTodo, Menu, X } from 'lucide-react'
import { useState } from 'react'
import { NavLink, Route, Routes, useLocation } from 'react-router-dom'
import AgentCreate from './pages/AgentCreate'
import Dashboard from './pages/Dashboard'
import Knowledge from './pages/Knowledge'
import TaskDetail from './pages/TaskDetail'
import Tasks from './pages/Tasks'

const nav = [
  { to: '/', label: '总览', icon: LayoutDashboard, end: true },
  { to: '/agent/new', label: '创建 Agent', icon: Bot },
  { to: '/tasks', label: '任务中心', icon: ListTodo },
  { to: '/knowledge', label: '知识库', icon: BookOpen },
]

export default function App() {
  const [open, setOpen] = useState(false)
  const location = useLocation()
  const current = nav.find(item => item.end ? location.pathname === '/' : location.pathname.startsWith(item.to))?.label ?? '任务详情'
  return <div className="app-shell">
    <aside className={`sidebar ${open ? 'sidebar-open' : ''}`}>
      <div className="brand"><span className="brand-mark"><Command size={21} /></span><span><strong>FlowPilot</strong><small>AI Workflow Console</small></span></div>
      <nav>{nav.map(({ to, label, icon: Icon, end }) => <NavLink key={to} to={to} end={end} onClick={() => setOpen(false)}><Icon size={18} />{label}</NavLink>)}</nav>
      <div className="sidebar-meta"><span className="live-dot" />本地控制台<small>真实后端 · API /api</small></div>
    </aside>
    {open && <button className="scrim" aria-label="关闭导航" onClick={() => setOpen(false)} />}
    <div className="main-column">
      <div className="topbar"><button className="menu-button" onClick={() => setOpen(!open)}>{open ? <X /> : <Menu />}</button><span>{current}</span><div className="topbar-status"><span className="live-dot" />Console online</div></div>
      <main><Routes><Route path="/" element={<Dashboard />} /><Route path="/agent/new" element={<AgentCreate />} /><Route path="/tasks" element={<Tasks />} /><Route path="/tasks/:id" element={<TaskDetail />} /><Route path="/knowledge" element={<Knowledge />} /></Routes></main>
    </div>
  </div>
}
