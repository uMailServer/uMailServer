import { useEffect, useState } from 'react'
import { Outlet, Link, useLocation, useNavigate } from 'react-router-dom'
import { User, Lock, Shield, Forward, Palmtree, LogOut, Filter } from 'lucide-react'

function Layout() {
  const location = useLocation()
  const navigate = useNavigate()
  const isLoginRoute = location.pathname === '/login'
  // Auth gate: protected routes render only after the session is verified.
  // Re-entering /login resets it, so Back after sign-out re-probes instead of
  // showing the previous user's stale page.
  const [session, setSession] = useState<'pending' | 'ok'>('pending')

  useEffect(() => {
    if (isLoginRoute) {
      setSession('pending')
      return
    }
    let stale = false
    void (async () => {
      try {
        const res = await fetch('/api/v1/account/forwarding', { credentials: 'include' })
        if (stale) return
        if (res.status === 401) {
          navigate('/login')
          return
        }
      } catch {
        // Network failure is not proof of a missing session: let the page
        // show its own error rather than locking the user out.
        if (stale) return
      }
      setSession('ok')
    })()
    return () => {
      stale = true
    }
  }, [isLoginRoute])

  // Revoke the server session: without this the HttpOnly cookie stays valid
  // and "Sign out" only hides the UI. Best-effort — a failing request must
  // not block the local sign-out.
  const handleSignOut = () => {
    void fetch('/api/v1/auth/logout', {
      method: 'POST',
      credentials: 'include',
    }).catch(() => {})
    navigate('/login')
  }

  const navItems = [
    { path: '/profile', label: 'Profile', icon: User },
    { path: '/password', label: 'Password', icon: Lock },
    { path: '/2fa', label: 'Two-Factor Auth', icon: Shield },
    { path: '/forwarding', label: 'Forwarding', icon: Forward },
    { path: '/filters', label: 'Email Filters', icon: Filter },
    { path: '/vacation', label: 'Vacation Reply', icon: Palmtree },
  ]

  // The login page renders bare, without the signed-in navigation.
  if (isLoginRoute) return <Outlet />
  if (session !== 'ok') return null

  return (
    <div className="min-h-screen bg-gray-50">
      <header className="bg-white shadow-sm border-b">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
          <div className="flex justify-between items-center h-16">
            <div className="flex items-center">
              <h1 className="text-xl font-semibold text-gray-900">
                Account Settings
              </h1>
            </div>
            <button onClick={handleSignOut} className="flex items-center text-sm text-gray-600 hover:text-gray-900">
              <LogOut className="w-4 h-4 mr-2" />
              Sign out
            </button>
          </div>
        </div>
      </header>

      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
        <div className="flex flex-col md:flex-row gap-8">
          <nav className="w-full md:w-64">
            <div className="bg-white rounded-lg shadow">
              {navItems.map((item) => {
                const Icon = item.icon
                const isActive = location.pathname === item.path
                return (
                  <Link
                    key={item.path}
                    to={item.path}
                    className={`flex items-center px-4 py-3 text-sm font-medium border-b last:border-b-0 ${
                      isActive
                        ? 'text-primary-600 bg-primary-50'
                        : 'text-gray-600 hover:bg-gray-50'
                    }`}
                  >
                    <Icon className="w-5 h-5 mr-3" />
                    {item.label}
                  </Link>
                )
              })}
            </div>
          </nav>

          <main className="flex-1">
            <div className="bg-white rounded-lg shadow p-6">
              <Outlet />
            </div>
          </main>
        </div>
      </div>
    </div>
  )
}

export default Layout
