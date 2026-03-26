import { useState } from 'react'
import { AlertCircle } from 'lucide-react'

function ForwardingPage() {
  const [enabled, setEnabled] = useState(false)
  const [forwardAddress, setForwardAddress] = useState('')
  const [keepCopy, setKeepCopy] = useState(true)
  const [saving, setSaving] = useState(false)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setSaving(true)
    await new Promise(resolve => setTimeout(resolve, 1000))
    setSaving(false)
  }

  return (
    <div>
      <h2 className="text-lg font-medium text-gray-900 mb-6">Mail Forwarding</h2>

      <div className="flex items-start p-4 bg-blue-50 border border-blue-200 rounded-md mb-6">
        <AlertCircle className="h-5 w-5 text-blue-600 mr-3 flex-shrink-0 mt-0.5" />
        <div className="text-sm text-blue-700">
          <p className="font-medium mb-1">Important</p>
          <p>
            Forwarded emails will be sent to the specified address. If you don't keep a copy,
            emails will not be stored in your mailbox.
          </p>
        </div>
      </div>

      <form onSubmit={handleSubmit} className="space-y-6">
        <div className="flex items-center">
          <input
            id="enabled"
            type="checkbox"
            checked={enabled}
            onChange={(e) => setEnabled(e.target.checked)}
            className="h-4 w-4 text-primary-600 focus:ring-primary-500 border-gray-300 rounded"
          />
          <label htmlFor="enabled" className="ml-2 block text-sm font-medium text-gray-700">
            Enable mail forwarding
          </label>
        </div>

        {enabled && (
          <>
            <div>
              <label htmlFor="forwardAddress" className="block text-sm font-medium text-gray-700">
                Forward to address
              </label>
              <input
                type="email"
                id="forwardAddress"
                required
                value={forwardAddress}
                onChange={(e) => setForwardAddress(e.target.value)}
                placeholder="forward@example.com"
                className="mt-1 block w-full px-3 py-2 border border-gray-300 rounded-md shadow-sm focus:ring-primary-500 focus:border-primary-500"
              />
            </div>

            <div className="flex items-center">
              <input
                id="keepCopy"
                type="checkbox"
                checked={keepCopy}
                onChange={(e) => setKeepCopy(e.target.checked)}
                className="h-4 w-4 text-primary-600 focus:ring-primary-500 border-gray-300 rounded"
              />
              <label htmlFor="keepCopy" className="ml-2 block text-sm font-medium text-gray-700">
                Keep a copy in my mailbox
              </label>
            </div>
          </>
        )}

        <div className="flex justify-end">
          <button
            type="submit"
            disabled={saving}
            className="px-4 py-2 border border-transparent rounded-md shadow-sm text-sm font-medium text-white bg-primary-600 hover:bg-primary-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-primary-500 disabled:opacity-50"
          >
            {saving ? 'Saving...' : 'Save Changes'}
          </button>
        </div>
      </form>
    </div>
  )
}

export default ForwardingPage
