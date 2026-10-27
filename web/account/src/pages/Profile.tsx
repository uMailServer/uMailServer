import { useEffect, useState } from 'react'
import { User } from 'lucide-react'

// What GET /api/v1/account returns about the signed-in account.
type SelfAccount = {
  email: string
  quota_used: number
  quota_limit: number // 0 = unlimited
  max_message_size: number
  two_factor_enabled: boolean
  forward_to?: string
  created_at: string
  last_login_at?: string
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let v = n / 1024
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(v < 10 ? 1 : 0)} ${units[i]}`
}

// Go encodes an unset time as year 1.
function formatDate(s?: string): string {
  if (!s || s.startsWith('0001-')) return 'Never'
  const d = new Date(s)
  return Number.isNaN(d.getTime()) ? 'Unknown' : d.toLocaleString()
}

// The profile is read-only: the address and limits are set by the
// administrator; password, two-factor and forwarding have their own pages.
function ProfilePage() {
  const [account, setAccount] = useState<SelfAccount | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    let stale = false
    const load = async () => {
      try {
        const res = await fetch('/api/v1/account', { credentials: 'include' })
        if (res.status === 401) {
          if (typeof window !== 'undefined') window.location.href = '/account/login'
          return
        }
        if (!res.ok) throw new Error('account unavailable')
        const data = (await res.json()) as SelfAccount
        if (!stale) setAccount(data)
      } catch {
        if (!stale) setError('Could not load your account. Please reload the page.')
      }
    }
    void load()
    return () => {
      stale = true
    }
  }, [])

  const row = (label: string, value: string) => (
    <div className="flex justify-between py-3 border-b border-gray-100 text-sm">
      <span className="text-gray-500">{label}</span>
      <span className="text-gray-900 text-right break-all">{value}</span>
    </div>
  )

  const quota = account
    ? account.quota_limit > 0
      ? `${formatBytes(account.quota_used)} of ${formatBytes(account.quota_limit)}`
      : `${formatBytes(account.quota_used)} (no limit)`
    : ''
  const percent = account && account.quota_limit > 0 ? Math.min(100, Math.round((account.quota_used / account.quota_limit) * 100)) : 0

  return (
    <div>
      <h2 className="text-lg font-medium text-gray-900 mb-6">Profile Settings</h2>

      {error && <div className="p-4 mb-4 bg-red-50 border border-red-200 rounded-md text-sm text-red-700">{error}</div>}
      {!account && !error && <p className="text-sm text-gray-500">Loading...</p>}

      {account && (
        <div>
          <div className="flex items-center mb-4">
            <User className="h-5 w-5 text-gray-500 mr-3" />
            <span className="font-medium text-gray-900 break-all">{account.email}</span>
          </div>
          {row('Storage', quota)}
          {account.quota_limit > 0 && (
            <div className="h-2 bg-gray-100 rounded mt-2 mb-1" role="progressbar" aria-valuenow={percent} aria-valuemin={0} aria-valuemax={100}>
              <div className={`h-2 rounded ${percent >= 90 ? 'bg-red-500' : 'bg-blue-500'}`} style={{ width: `${percent}%` }} />
            </div>
          )}
          {account.max_message_size > 0 && row('Largest message', formatBytes(account.max_message_size))}
          {row('Two-factor authentication', account.two_factor_enabled ? 'On' : 'Off')}
          {row('Forwarding', account.forward_to ? `To ${account.forward_to}` : 'Off')}
          {row('Account created', formatDate(account.created_at))}
          {row('Last sign-in', formatDate(account.last_login_at))}
          <p className="mt-4 text-sm text-gray-500">
            Your address and storage limit are managed by your administrator.
          </p>
        </div>
      )}
    </div>
  )
}

export default ProfilePage
