import { useEffect, useState, type ChangeEvent } from 'react'

type TotpPhase = 'loading' | 'off' | 'pending' | 'enabled'

type TotpStatus = {
  enabled: boolean
  pending_setup: boolean
}

// Extract the base32 secret from the server's otpauth URI so the user can
// also enter it manually into their authenticator app.
function secretFromUri(uri: string): string {
  try {
    return new URL(uri).searchParams.get('secret') ?? ''
  } catch {
    return ''
  }
}

function TwoFactorPage() {
  const [phase, setPhase] = useState<TotpPhase>('loading')
  const [setupUri, setSetupUri] = useState('')
  const [code, setCode] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    let stale = false
    const loadStatus = async () => {
      try {
        const res = await fetch('/api/v1/account/totp', { credentials: 'include' })
        if (!res.ok) throw new Error('status unavailable')
        const data = (await res.json()) as TotpStatus
        if (stale) return
        if (data.enabled) {
          setPhase('enabled')
        } else if (data.pending_setup) {
          // A pending secret is unusable without its otpauth URI (lost on
          // refresh) — regenerate a fresh one so the user can finish setup.
          setPhase('pending')
          void beginSetup()
        } else {
          setPhase('off')
        }
      } catch {
        if (!stale) setError('Could not load your two-factor status. Please reload the page.')
      }
    }
    void loadStatus()
    return () => {
      stale = true
    }
  }, [])

  const refreshStatus = async () => {
    const res = await fetch('/api/v1/account/totp', { credentials: 'include' })
    if (!res.ok) throw new Error('status unavailable')
    const data = (await res.json()) as TotpStatus
    if (data.enabled) {
      setPhase('enabled')
      setSetupUri('')
      setCode('')
    } else {
      setPhase(data.pending_setup ? 'pending' : 'off')
    }
  }

  const beginSetup = async () => {
    setError('')
    setBusy(true)
    try {
      const res = await fetch('/api/v1/account/totp/setup', { method: 'POST', credentials: 'include' })
      if (!res.ok) throw new Error('setup failed')
      const data = (await res.json()) as { uri: string }
      setSetupUri(data.uri)
      setCode('')
      setPhase('pending')
    } catch {
      setError('Could not start two-factor setup. Please try again.')
    } finally {
      setBusy(false)
    }
  }

  const verify = async () => {
    setError('')
    setBusy(true)
    try {
      const res = await fetch('/api/v1/account/totp/verify', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ code }),
      })
      if (!res.ok) {
        const data = (await res.json().catch(() => null)) as { error?: string } | null
        setError(data?.error ?? 'Verification failed. Please try again.')
        return
      }
      await refreshStatus()
    } catch {
      setError('Verification failed. Please try again.')
    } finally {
      setBusy(false)
    }
  }

  const disable = async () => {
    setError('')
    // The server requires a current TOTP code to disable an enabled factor.
    if (code.trim() === '') {
      setError('Enter a current code from your authenticator app to disable two-factor.')
      return
    }
    setBusy(true)
    try {
      const res = await fetch('/api/v1/account/totp/disable', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ code: code.trim() }),
      })
      if (!res.ok) {
        const data = (await res.json().catch(() => null)) as { error?: string } | null
        setError(data?.error ?? 'Could not disable two-factor authentication. Please try again.')
        return
      }
      await refreshStatus()
      setCode('')
    } catch {
      setError('Could not disable two-factor authentication. Please try again.')
    } finally {
      setBusy(false)
    }
  }

  const onCodeChange = (e: ChangeEvent<HTMLInputElement>) => {
    setCode(e.target.value)
  }

  if (phase === 'loading') {
    return (
      <div>
        <h2 className="text-lg font-medium text-gray-900 mb-6">Two-Factor Authentication</h2>
        <p className="text-sm text-gray-600">Loading your two-factor status…</p>
      </div>
    )
  }

  return (
    <div>
      <h2 className="text-lg font-medium text-gray-900 mb-6">Two-Factor Authentication</h2>

      {phase === 'enabled' ? (
        <div className="p-4 bg-green-50 border border-green-200 rounded-md">
          <p className="font-medium text-green-800">Two-factor authentication is enabled.</p>
          <p className="text-sm text-green-700 mt-1">
            Sign-in attempts require a code from your authenticator app.
          </p>
          <input
            type="text"
            value={code}
            onChange={onCodeChange}
            maxLength={6}
            aria-label="Current authenticator code"
            placeholder="123456"
            className="mt-3 block w-32 px-3 py-2 border border-gray-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-red-500"
          />
          <button
            type="button"
            onClick={disable}
            disabled={busy}
            className="mt-3 px-4 py-2 text-sm font-medium text-white bg-red-600 rounded-md hover:bg-red-700 focus:outline-none focus:ring-2 focus:ring-red-500 focus:ring-offset-2 disabled:opacity-50"
          >
            {busy ? 'Disabling…' : 'Disable two-factor'}
          </button>
        </div>
      ) : phase === 'pending' ? (
        <div>
          <p className="text-sm text-gray-600 mb-3">
            Scan the code below with your authenticator app (or enter the secret manually), then confirm
            the 6-digit code it shows.
          </p>
          <pre className="p-3 bg-gray-50 border border-gray-200 rounded-md text-xs overflow-x-auto whitespace-pre-wrap break-all">
            {setupUri}
          </pre>
          {secretFromUri(setupUri) !== '' && (
            <p className="mt-2 text-sm text-gray-700">
              Manual entry secret:{' '}
              <code className="px-1.5 py-0.5 bg-gray-100 rounded font-mono">{secretFromUri(setupUri)}</code>
            </p>
          )}
          <div className="mt-3 flex items-center gap-2">
            <input
              type="text"
              value={code}
              onChange={onCodeChange}
              maxLength={6}
              aria-label="6-digit verification code"
              placeholder="123456"
              className="w-32 px-3 py-2 border border-gray-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-primary-500"
            />
            <button
              type="button"
              onClick={verify}
              disabled={busy || code.length === 0}
              className="px-4 py-2 text-sm font-medium text-white bg-primary-600 rounded-md hover:bg-primary-700 focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2 disabled:opacity-50"
            >
              {busy ? 'Verifying…' : 'Verify code'}
            </button>
            <button
              type="button"
              onClick={beginSetup}
              disabled={busy}
              className="px-4 py-2 text-sm font-medium text-gray-700 bg-white border border-gray-300 rounded-md hover:bg-gray-50 focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2 disabled:opacity-50"
            >
              Generate a new secret
            </button>
          </div>
        </div>
      ) : (
        <div>
          <p className="text-sm text-gray-600 mb-3">
            Add a second factor to your sign-in: an authenticator app generates a one-time code on top
            of your password.
          </p>
          <button
            type="button"
            onClick={beginSetup}
            disabled={busy}
            className="px-4 py-2 text-sm font-medium text-white bg-primary-600 rounded-md hover:bg-primary-700 focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2 disabled:opacity-50"
          >
            {busy ? 'Starting…' : 'Enable two-factor authentication'}
          </button>
        </div>
      )}

      {error !== '' && (
        <div className="mt-4 p-3 bg-red-50 border border-red-200 rounded-md text-sm text-red-700" role="alert">
          {error}
        </div>
      )}
    </div>
  )
}

export default TwoFactorPage
