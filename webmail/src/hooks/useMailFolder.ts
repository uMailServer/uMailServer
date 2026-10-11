import { useCallback, useEffect, useRef, useState } from 'react'
import api, { Mail } from '@/utils/api'

export interface UseMailFolderResult {
  emails: Mail[]
  loading: boolean
  error: string | null
  reload: () => Promise<void>
  removeLocal: (ids: string[]) => void
}

/**
 * Loads one folder from GET /mail/<folder>. A response is applied only if it
 * belongs to the latest request (folder switch / reload) and the component is
 * still mounted, so a slow earlier fetch can never overwrite newer state.
 */
export function useMailFolder(folder: string): UseMailFolderResult {
  const [emails, setEmails] = useState<Mail[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const requestRef = useRef(0)
  const mountedRef = useRef(true)

  const load = useCallback(async () => {
    const id = ++requestRef.current
    setLoading(true)
    setError(null)
    try {
      const data = await api.getMail(folder)
      if (!mountedRef.current || id !== requestRef.current) return
      setEmails(Array.isArray(data?.emails) ? data.emails : [])
    } catch (err) {
      if (!mountedRef.current || id !== requestRef.current) return
      setEmails([])
      setError(err instanceof Error && err.message ? err.message : 'Failed to load messages')
    } finally {
      if (mountedRef.current && id === requestRef.current) setLoading(false)
    }
  }, [folder])

  useEffect(() => {
    mountedRef.current = true
    // Drop the previous folder's rows immediately so they are never shown
    // under the new folder's heading.
    setEmails([])
    void load()
    return () => {
      mountedRef.current = false
      requestRef.current++
    }
  }, [load])

  const removeLocal = useCallback((ids: string[]) => {
    const set = new Set(ids)
    setEmails((prev) => prev.filter((e) => !set.has(e.id)))
  }, [])

  return { emails, loading, error, reload: load, removeLocal }
}
