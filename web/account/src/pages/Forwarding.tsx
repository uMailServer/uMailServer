import { useEffect, useState, type ChangeEvent } from 'react'

type ForwardingStatus = {
  forward_to: string
  keep_copy: boolean
  forwarding_on: boolean
}

function ForwardingPage() {
  const [phase, setPhase] = useState<'loading' | 'ready'>('loading')
  const [forwardTo, setForwardTo] = useState('')
  const [keepCopy, setKeepCopy] = useState(false)
  const [forwardingOn, setForwardingOn] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    let stale = false
    const loadStatus = async () => {
      try {
        const res = await fetch('/api/v1/account/forwarding', { credentials: 'include' })
        if (!res.ok) throw new Error('status unavailable')
        const data = (await res.json()) as ForwardingStatus
        if (stale) return
        setForwardTo(data.forward_to ?? '')
        setKeepCopy(Boolean(data.keep_copy))
        setForwardingOn(Boolean(data.forwarding_on))
        setPhase('ready')
      } catch {
        if (!stale) setError('Could not load your forwarding settings. Please reload the page.')
      }
    }
    void loadStatus()
    return () => {
      stale = true
    }
  }, [])

  const save = async () => {
    setError('')
    setNotice('')
    setBusy(true)
    try {
      const res = await fetch('/api/v1/account/forwarding', {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ forward_to: forwardTo.trim(), keep_copy: keepCopy }),
      })
      if (!res.ok) {
        const data = (await res.json().catch(() => null)) as { error?: string } | null
        setError(data?.error ?? 'Saving forwarding settings failed. Please try again.')
        return
      }
      const data = (await res.json()) as ForwardingStatus
      // The server's response is the truth — apply exactly what it stored.
      setForwardTo(data.forward_to ?? '')
      setKeepCopy(Boolean(data.keep_copy))
      setForwardingOn(Boolean(data.forwarding_on))
      setNotice('Forwarding settings saved.')
    } catch {
      setError('Saving forwarding settings failed. Please try again.')
    } finally {
      setBusy(false)
    }
  }

  const onAddressChange = (e: ChangeEvent<HTMLInputElement>) => {
    setForwardTo(e.target.value)
  }

  const onKeepCopyChange = (e: ChangeEvent<HTMLInputElement>) => {
    setKeepCopy(e.target.checked)
  }

  if (phase === 'loading') {
    return (
      <div>
        <h2 className="text-lg font-medium text-gray-900 mb-6">Mail Forwarding</h2>
        <p className="text-sm text-gray-600">Loading your forwarding settings…</p>
      </div>
    )
  }

  return (
    <div>
      <h2 className="text-lg font-medium text-gray-900 mb-6">Mail Forwarding</h2>

      <p className="text-sm text-gray-600 mb-4" aria-live="polite">
        {forwardingOn
          ? `Forwarding is on: new mail is delivered to ${forwardTo}${keepCopy ? ' and a copy is kept in your mailbox.' : '.'}`
          : 'Forwarding is off: mail is delivered to your mailbox only.'}
      </p>

      <div className="space-y-3">
        <div>
          <label htmlFor="forward-to" className="block text-sm font-medium text-gray-700 mb-1">
            Forward new mail to
          </label>
          <input
            id="forward-to"
            type="text"
            value={forwardTo}
            onChange={onAddressChange}
            placeholder="destination@example.com"
            className="w-full max-w-md px-3 py-2 border border-gray-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-primary-500"
          />
          <p className="mt-1 text-xs text-gray-500">Leave empty to turn forwarding off.</p>
        </div>

        <label className="flex items-center gap-2 text-sm text-gray-700">
          <input
            type="checkbox"
            checked={keepCopy}
            onChange={onKeepCopyChange}
            className="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
          Keep a copy of forwarded mail in your mailbox
        </label>

        <button
          type="button"
          onClick={save}
          disabled={busy}
          className="px-4 py-2 text-sm font-medium text-white bg-primary-600 rounded-md hover:bg-primary-700 focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2 disabled:opacity-50"
        >
          {busy ? 'Saving…' : 'Save changes'}
        </button>
      </div>

      {error !== '' && (
        <div className="mt-4 p-3 bg-red-50 border border-red-200 rounded-md text-sm text-red-700" role="alert">
          {error}
        </div>
      )}
      {notice !== '' && (
        <div className="mt-4 p-3 bg-green-50 border border-green-200 rounded-md text-sm text-green-700" role="status">
          {notice}
        </div>
      )}
    </div>
  )
}

export default ForwardingPage
