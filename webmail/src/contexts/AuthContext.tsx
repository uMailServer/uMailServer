import { createContext, useContext, useState, useCallback, useEffect, useRef } from 'react'
import api, { ApiError } from '../utils/api'

interface AuthContextType {
  user: { email: string } | null
  isAuthenticated: boolean
  isLoading: boolean
  loading: boolean
  error: string | null
  requiresTotp: boolean
  login: (email: string, password: string, totpCode?: string) => Promise<boolean>
  logout: () => void
}

const AuthContext = createContext<AuthContextType | null>(null)

function describeLoginError(err: unknown, hadTotp: boolean): string {
  if (err instanceof ApiError) {
    const msg = err.serverMessage ?? ''
    if (err.status === 401 && /totp/i.test(msg)) {
      return hadTotp ? 'Invalid authentication code' : 'Enter the 6-digit code from your authenticator app'
    }
    if (err.status === 429) return msg || 'Too many attempts. Please try again later.'
    if (err.status === 401) return 'Invalid email or password'
    if (err.status >= 500) return 'Server error. Please try again.'
    return msg || 'Login failed'
  }
  return 'Connection error. Please try again.'
}

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<{ email: string } | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [isAuthenticated, setIsAuthenticated] = useState(false)
  const [requiresTotp, setRequiresTotp] = useState(false)
  const requestIdRef = useRef(0)
  const mountedRef = useRef(true)

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      ++requestIdRef.current
    }
  }, [])

  const login = useCallback(async (email: string, password: string, totpCode?: string): Promise<boolean> => {
    if (!mountedRef.current) return false
    const requestId = ++requestIdRef.current
    const isCurrent = () => mountedRef.current && requestId === requestIdRef.current
    setLoading(true)
    setError(null)
    try {
      // Token is now in HttpOnly cookie - no need to store in memory
      await api.post<{ expiresIn?: number }>(
        '/auth/login',
        totpCode ? { email, password, totp_code: totpCode } : { email, password }
      )
      if (!isCurrent()) return false
      setRequiresTotp(false)
      setUser({ email })
      setIsAuthenticated(true)
      return true
    } catch (err: unknown) {
      if (!isCurrent()) return false
      setError(describeLoginError(err, Boolean(totpCode)))
      if (err instanceof ApiError && err.status === 401 && /totp/i.test(err.serverMessage ?? '')) {
        // Two-factor is enabled for this account: the form must ask for the code.
        setRequiresTotp(true)
      }
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
    // Revoke the server session: without this call the HttpOnly cookie stays
    // valid and "logout" only clears local state. Best-effort — a failing
    // request must not block or break the local logout.
    void api.post('/auth/logout').catch(() => {
      // Server revocation is best-effort; local state is already cleared.
    })
  }, [])

  const value: AuthContextType = {
    user,
    isAuthenticated,
    isLoading: false,
    loading,
    error,
    requiresTotp,
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
