import { createContext, useContext, useState, useCallback, useEffect } from 'react'
import api from '../utils/api'

interface AuthContextType {
  user: { email: string } | null
  token: string | null
  isAuthenticated: boolean
  isLoading: boolean
  loading: boolean
  error: string | null
  login: (email: string, password: string) => Promise<boolean>
  logout: () => void
}

const AuthContext = createContext<AuthContextType | null>(null)

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<{ email: string } | null>(() => {
    try {
      const saved = localStorage.getItem('user')
      return saved ? JSON.parse(saved) : null
    } catch {
      return null
    }
  })
  const [token, setToken] = useState<string | null>(() => localStorage.getItem('token'))
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const login = useCallback(async (email: string, password: string): Promise<boolean> => {
    setLoading(true)
    setError(null)
    try {
      const data = await api.post('/auth/login', { email, password })
      if (data.token) {
        setToken(data.token)
        setUser({ email })
        localStorage.setItem('token', data.token)
        localStorage.setItem('user', JSON.stringify({ email }))
        api.setToken(data.token)
        return true
      }
      return false
    } catch (err: any) {
      setError(err.message || 'Login failed')
      return false
    } finally {
      setLoading(false)
    }
  }, [])

  const logout = useCallback(() => {
    setToken(null)
    setUser(null)
    localStorage.removeItem('token')
    localStorage.removeItem('user')
    api.setToken(null)
  }, [])

  // Set token on api when it changes
  useEffect(() => {
    if (token) {
      api.setToken(token)
    }
  }, [token])

  const value: AuthContextType = {
    user,
    token,
    isAuthenticated: !!token,
    isLoading: false,
    loading,
    error,
    login,
    logout
  }

  return (
    <AuthContext.Provider value={value}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth() {
  const context = useContext(AuthContext)
  if (!context) {
    throw new Error('useAuth must be used within an AuthProvider')
  }
  return context
}
