import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Trash2 } from 'lucide-react'
import { useI18n } from '../hooks/useI18n'

// Maps the DELETE /api/v1/account status to a locale key. 403 = wrong
// password, 429 = shared password-guess budget, 409 = last active admin.
function errorKey(status: number): string {
  if (status === 403) return 'deleteAccount.wrongPassword'
  if (status === 429) return 'deleteAccount.tooMany'
  if (status === 409) return 'deleteAccount.lastAdmin'
  return 'deleteAccount.failed'
}

function DeleteAccountPage() {
  const { t } = useI18n()
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const close = () => {
    if (busy) return
    setOpen(false)
    setPassword('')
    setError('')
  }

  const handleConfirm = async (e: React.FormEvent) => {
    e.preventDefault()
    if (busy) return
    if (!password) {
      setError(t('deleteAccount.passwordRequired'))
      return
    }
    setBusy(true)
    setError('')
    try {
      const res = await fetch('/api/v1/account', {
        method: 'DELETE',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ password }),
      })
      if (res.ok) {
        // The server revoked the session and cleared the cookie.
        navigate('/login')
        return
      }
      if (res.status === 401) {
        navigate('/login')
        return
      }
      setError(t(errorKey(res.status)))
    } catch {
      setError(t('deleteAccount.failed'))
    } finally {
      setBusy(false)
      setPassword('')
    }
  }

  return (
    <div>
      <h2 className="text-lg font-medium text-gray-900 mb-6">{t('deleteAccount.title')}</h2>
      <div className="p-4 bg-red-50 border border-red-200 rounded-md">
        <p className="text-sm text-red-700 mb-4">{t('deleteAccount.warning')}</p>
        <button
          type="button"
          onClick={() => setOpen(true)}
          className="inline-flex items-center px-4 py-2 text-sm font-medium text-white bg-red-600 rounded-md hover:bg-red-700"
        >
          <Trash2 className="w-4 h-4 mr-2" />
          {t('deleteAccount.open')}
        </button>
      </div>

      {open && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 px-4">
          <form
            role="dialog"
            aria-modal="true"
            aria-labelledby="delete-account-title"
            onSubmit={handleConfirm}
            className="w-full max-w-md bg-white rounded-lg shadow-lg p-6 space-y-4"
          >
            <h3 id="delete-account-title" className="text-lg font-medium text-gray-900">
              {t('deleteAccount.dialogTitle')}
            </h3>
            <p className="text-sm text-gray-600">{t('deleteAccount.dialogBody')}</p>
            <div>
              <label htmlFor="delete-password" className="block text-sm font-medium text-gray-700">
                {t('deleteAccount.password')}
              </label>
              <input
                id="delete-password"
                type="password"
                autoComplete="current-password"
                autoFocus
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                className="mt-1 block w-full px-3 py-2 border border-gray-300 rounded-md shadow-sm"
              />
            </div>
            {error && (
              <div className="rounded-md border border-red-200 bg-red-50 p-3" role="alert">
                <p className="text-sm text-red-600">{error}</p>
              </div>
            )}
            <div className="flex justify-end gap-3">
              <button
                type="button"
                onClick={close}
                disabled={busy}
                className="px-4 py-2 text-sm font-medium text-gray-700 bg-white border border-gray-300 rounded-md hover:bg-gray-50 disabled:opacity-50"
              >
                {t('common.cancel')}
              </button>
              <button
                type="submit"
                disabled={busy}
                className="px-4 py-2 text-sm font-medium text-white bg-red-600 rounded-md hover:bg-red-700 disabled:opacity-50"
              >
                {busy ? t('deleteAccount.deleting') : t('deleteAccount.confirm')}
              </button>
            </div>
          </form>
        </div>
      )}
    </div>
  )
}

export default DeleteAccountPage
