import { BrowserRouter, Routes, Route, Link, Navigate } from 'react-router-dom'
import { useState, useEffect } from 'react'
import {
  LayoutDashboard,
  Globe,
  Users,
  Mail,
  Settings,
  Menu,
  X,
  LogOut,
  Server,
  Activity,
  AlertTriangle
} from 'lucide-react'

// Components
import Dashboard from './pages/Dashboard'
import Domains from './pages/Domains'
import Accounts from './pages/Accounts'
import Queue from './pages/Queue'
import SettingsPage from './pages/Settings'
import Login from './pages/Login'

function App() {
  const [isAuthenticated, setIsAuthenticated] = useState(false)
  const [user, setUser] = useState(null)
  const [isSidebarOpen, setIsSidebarOpen] = useState(true)

  useEffect(() => {
    // Check for existing token
    const token = localStorage.getItem('adminToken')
    if (token) {
      setIsAuthenticated(true)
      // TODO: Validate token and get user info
      setUser({ email: 'admin@example.com' })
    }
  }, [])

  const handleLogin = (token, userData) => {
    localStorage.setItem('adminToken', token)
    setIsAuthenticated(true)
    setUser(userData)
  }

  const handleLogout = () => {
    localStorage.removeItem('adminToken')
    setIsAuthenticated(false)
    setUser(null)
  }

  if (!isAuthenticated) {
    return <Login onLogin={handleLogin} />
  }

  return (
    <BrowserRouter>
      <div className="min-h-screen bg-gray-50 flex">
        {/* Sidebar */}
        <aside
          className={`${
            isSidebarOpen ? 'w-64' : 'w-0'
          } bg-white border-r border-gray-200 transition-all duration-300 overflow-hidden flex-shrink-0`}
        >
          <div className="h-16 flex items-center px-6 border-b border-gray-200">
            <Server className="w-6 h-6 text-violet-600 mr-2" />
            <span className="font-semibold text-lg">uMail Admin</span>
          </div>

          <nav className="p-4 space-y-1">
            <NavItem to="/" icon={<LayoutDashboard className="w-5 h-5" />}>
              Dashboard
            </NavItem>
            <NavItem to="/domains" icon={<Globe className="w-5 h-5" />}>
              Domains
            </NavItem>
            <NavItem to="/accounts" icon={<Users className="w-5 h-5" />}>
              Accounts
            </NavItem>
            <NavItem to="/queue" icon={<Mail className="w-5 h-5" />}>
              Queue
            </NavItem>
            <NavItem to="/settings" icon={<Settings className="w-5 h-5" />}>
              Settings
            </NavItem>
          </nav>
        </aside>

        {/* Main Content */}
        <div className="flex-1 flex flex-col min-w-0">
          {/* Header */}
          <header className="h-16 bg-white border-b border-gray-200 flex items-center justify-between px-4">
            <button
              onClick={() => setIsSidebarOpen(!isSidebarOpen)}
              className="p-2 hover:bg-gray-100 rounded-lg"
            >
              {isSidebarOpen ? <X className="w-5 h-5" /> : <Menu className="w-5 h-5" />}
            </button>

            <div className="flex items-center space-x-4">
              <div className="flex items-center text-sm text-gray-600">
                <span className="mr-2">{user?.email}</span>
              </div>
              <button
                onClick={handleLogout}
                className="p-2 hover:bg-gray-100 rounded-lg text-gray-600 hover:text-red-600"
              >
                <LogOut className="w-5 h-5" />
              </button>
            </div>
          </header>

          {/* Page Content */}
          <main className="flex-1 p-6 overflow-auto">
            <Routes>
              <Route path="/" element={<Dashboard />} />
              <Route path="/domains" element={<Domains />} />
              <Route path="/accounts" element={<Accounts />} />
              <Route path="/queue" element={<Queue />} />
              <Route path="/settings" element={<SettingsPage />} />
              <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
          </main>
        </div>
      </div>
    </BrowserRouter>
  )
}

// Navigation Item Component
function NavItem({ to, icon, children }) {
  return (
    <Link
      to={to}
      className="flex items-center px-4 py-2 text-gray-700 hover:bg-gray-100 rounded-lg transition-colors"
    >
      <span className="mr-3 text-gray-500">{icon}</span>
      <span>{children}</span>
    </Link>
  )
}

export default App
