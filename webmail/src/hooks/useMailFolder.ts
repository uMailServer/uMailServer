import { useCallback, useEffect, useRef, useState } from 'react'
import api, { Mail, FLAG_SEEN, FLAG_FLAGGED } from '@/utils/api'

export type MailFlagField = 'read' | 'starred'

export interface UseMailFolderResult {
  emails: Mail[]
  /** Folder size reported by the server (>= emails.length when more pages exist). */
  total: number
  hasMore: boolean
  loading: boolean
  loadingMore: boolean
  error: string | null
  reload: () => Promise<void>
  loadMore: () => Promise<void>
  removeLocal: (ids: string[]) => void
  /**
   * Optimistically sets read/starred on the given ids, persists via
   * POST /mail/flags and rolls back on failure. Ids that already have a flag
   * request in flight are skipped (double-click guard). Resolves true on success.
   */
  setFlag: (ids: string[], field: MailFlagField, value: boolean) => Promise<boolean>
}

const FLAG_OF: Record<MailFlagField, string> = { read: FLAG_SEEN, starred: FLAG_FLAGGED }

/**
 * Loads one folder from GET /mail/<folder>. A response is applied only if it
 * belongs to the latest request (folder switch / reload) and the component is
 * still mounted, so a slow earlier fetch can never overwrite newer state.
 */
export function useMailFolder(folder: string): UseMailFolderResult {
  const [emails, setEmails] = useState<Mail[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const requestRef = useRef(0)
  const mountedRef = useRef(true)
  const loadingMoreRef = useRef(false)
  const emailsRef = useRef<Mail[]>([])
  const pendingFlags = useRef<Set<string>>(new Set())

  const apply = useCallback((updater: (prev: Mail[]) => Mail[]) => {
    const next = updater(emailsRef.current)
    emailsRef.current = next
    setEmails(next)
  }, [])

  const load = useCallback(async () => {
    const id = ++requestRef.current
    loadingMoreRef.current = false
    setLoadingMore(false)
    setLoading(true)
    setError(null)
    try {
      const data = await api.getMail(folder)
      if (!mountedRef.current || id !== requestRef.current) return
      const list = Array.isArray(data?.emails) ? data.emails : []
      emailsRef.current = list
      setEmails(list)
      setTotal(typeof data?.total === 'number' ? data.total : list.length)
    } catch (err) {
      if (!mountedRef.current || id !== requestRef.current) return
      emailsRef.current = []
      setEmails([])
      setTotal(0)
      setError(err instanceof Error && err.message ? err.message : 'Failed to load messages')
    } finally {
      if (mountedRef.current && id === requestRef.current) setLoading(false)
    }
  }, [folder])

  useEffect(() => {
    mountedRef.current = true
    // Drop the previous folder's rows immediately so they are never shown
    // under the new folder's heading.
    emailsRef.current = []
    setEmails([])
    setTotal(0)
    pendingFlags.current.clear()
    void load()
    return () => {
      mountedRef.current = false
      requestRef.current++
    }
  }, [load])

  const loadMore = useCallback(async () => {
    if (loadingMoreRef.current) return
    const offset = emailsRef.current.length
    if (offset === 0) return
    const id = requestRef.current
    loadingMoreRef.current = true
    setLoadingMore(true)
    try {
      const data = await api.getMail(folder, { offset })
      if (!mountedRef.current || id !== requestRef.current) return
      const more = Array.isArray(data?.emails) ? data.emails : []
      const seen = new Set(emailsRef.current.map((e) => e.id))
      apply((prev) => [...prev, ...more.filter((m) => !seen.has(m.id))])
      // An empty page means the folder shrank: stop offering "load more".
      if (more.length === 0) setTotal(emailsRef.current.length)
      else if (typeof data?.total === 'number') setTotal(data.total)
    } catch (err) {
      if (!mountedRef.current || id !== requestRef.current) return
      setError(err instanceof Error && err.message ? err.message : 'Failed to load more messages')
    } finally {
      if (mountedRef.current && id === requestRef.current) {
        loadingMoreRef.current = false
        setLoadingMore(false)
      }
    }
  }, [folder, apply])

  const removeLocal = useCallback((ids: string[]) => {
    const set = new Set(ids)
    apply((prev) => prev.filter((e) => !set.has(e.id)))
    setTotal((t) => Math.max(0, t - ids.length))
  }, [apply])

  const setFlag = useCallback(async (ids: string[], field: MailFlagField, value: boolean) => {
    const gen = requestRef.current
    const targets = ids.filter((id) => {
      if (pendingFlags.current.has(id)) return false
      const cur = emailsRef.current.find((e) => e.id === id)
      return !!cur && cur[field] !== value
    })
    if (targets.length === 0) return true
    const set = new Set(targets)
    targets.forEach((id) => pendingFlags.current.add(id))
    apply((prev) => prev.map((e) => (set.has(e.id) ? { ...e, [field]: value } : e)))
    const release = () => targets.forEach((id) => pendingFlags.current.delete(id))
    try {
      await api.setFlags(targets, value ? { add: [FLAG_OF[field]] } : { remove: [FLAG_OF[field]] })
      release()
      return true
    } catch {
      release()
      if (mountedRef.current && gen === requestRef.current) {
        apply((prev) => prev.map((e) => (set.has(e.id) && e[field] === value ? { ...e, [field]: !value } : e)))
      }
      return false
    }
  }, [apply])

  return {
    emails, total, hasMore: emails.length < total, loading, loadingMore, error,
    reload: load, loadMore, removeLocal, setFlag,
  }
}
