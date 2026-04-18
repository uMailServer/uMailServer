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
  const [user, setUser] = useState<{ email: string } | null>(null)
  const [token, setToken] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const login = useCallback(async (email: string, password: string): Promise<boolean> => {
    setLoading(true)
    setError(null)
    try {
      const data = await api.post<{ token?: string }>('/auth/login', { email, password })
      if (data?.token) {
        setToken(data.token)
        setUser({ email })
        api.setToken(data.token)
        return true
      }
      return false
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Login failed')
      return false
    } finally {
      setLoading(false)
    }
  }, [])

  const logout = useCallback(() => {
    setToken(null)
    setUser(null)
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
