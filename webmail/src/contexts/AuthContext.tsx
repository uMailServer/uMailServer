import { createContext, useContext, useState, useCallback, useEffect, useRef } from 'react'
import api from '../utils/api'

interface AuthContextType {
  user: { email: string } | null
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
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [isAuthenticated, setIsAuthenticated] = useState(false)
  const requestIdRef = useRef(0)
  const mountedRef = useRef(true)

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      ++requestIdRef.current
    }
  }, [])

  const login = useCallback(async (email: string, password: string): Promise<boolean> => {
    if (!mountedRef.current) return false
    const requestId = ++requestIdRef.current
    const isCurrent = () => mountedRef.current && requestId === requestIdRef.current
    setLoading(true)
    setError(null)
    try {
      // Token is now in HttpOnly cookie - no need to store in memory
      await api.post<{ expiresIn?: number }>('/auth/login', { email, password })
      if (!isCurrent()) return false
      setUser({ email })
      setIsAuthenticated(true)
      return true
    } catch (err: unknown) {
      if (!isCurrent()) return false
      setError(err instanceof Error ? err.message : 'Login failed')
      return false
    } finally {
      if (isCurrent()) setLoading(false)
    }
  }, [])

  const logout = useCallback(() => {
    ++requestIdRef.current
    setUser(null)
    setIsAuthenticated(false)
    setLoading(false)
    setError(null)
    api.setToken(null)
  }, [])

  const value: AuthContextType = {
    user,
    isAuthenticated,
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
