import { useState, useEffect } from 'react'
import { Plus, Search, Trash2, Edit, Globe, CheckCircle, XCircle } from 'lucide-react'

function Domains() {
  const [domains, setDomains] = useState([])
  const [loading, setLoading] = useState(true)
  const [showAddModal, setShowAddModal] = useState(false)
  const [newDomain, setNewDomain] = useState({
    name: '',
    max_accounts: 100
  })

  useEffect(() => {
    fetchDomains()
  }, [])

  const fetchDomains = async () => {
    try {
      const token = localStorage.getItem('adminToken')
      const response = await fetch('/api/v1/domains', {
        headers: {
          'Authorization': `Bearer ${token}`
        }
      })
      if (response.ok) {
        const data = await response.json()
        setDomains(data)
      }
    } catch (error) {
      console.error('Failed to fetch domains:', error)
    } finally {
      setLoading(false)
    }
  }

  const handleAddDomain = async (e) => {
    e.preventDefault()
    try {
      const token = localStorage.getItem('adminToken')
      const response = await fetch('/api/v1/domains', {
        method: 'POST',
        headers: {
          'Authorization': `Bearer ${token}`,
          'Content-Type': 'application/json'
        },
        body: JSON.stringify(newDomain)
      })

      if (response.ok) {
        setShowAddModal(false)
        setNewDomain({ name: '', max_accounts: 100 })
        fetchDomains()
      }
    } catch (error) {
      console.error('Failed to add domain:', error)
    }
  }

  const handleDeleteDomain = async (name) => {
    if (!confirm(`Are you sure you want to delete ${name}?`)) return

    try {
      const token = localStorage.getItem('adminToken')
      const response = await fetch(`/api/v1/domains/${name}`, {
        method: 'DELETE',
        headers: {
          'Authorization': `Bearer ${token}`
        }
      })

      if (response.ok) {
        fetchDomains()
      }
    } catch (error) {
      console.error('Failed to delete domain:', error)
    }
  }

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold text-gray-900">Domains</h1>
        <button
          onClick={() => setShowAddModal(true)}
          className="flex items-center px-4 py-2 bg-violet-600 text-white rounded-lg hover:bg-violet-700 transition-colors"
        >
          <Plus className="w-5 h-5 mr-2" />
          Add Domain
        </button>
      </div>

      {/* Domains Table */}
      <div className="bg-white rounded-lg shadow-sm border border-gray-200">
        <table className="w-full">
          <thead>
            <tr className="border-b border-gray-200">
              <th className="px-6 py-3 text-left text-sm font-medium text-gray-500">Domain</th>
              <th className="px-6 py-3 text-left text-sm font-medium text-gray-500">Status</th>
              <th className="px-6 py-3 text-left text-sm font-medium text-gray-500">Max Accounts</th>
              <th className="px-6 py-3 text-left text-sm font-medium text-gray-500">Created</th>
              <th className="px-6 py-3 text-right text-sm font-medium text-gray-500">Actions</th>
            </tr>
          </thead>
          <tbody>
            {domains.length === 0 ? (
              <tr>
                <td colSpan="5" className="px-6 py-8 text-center text-gray-500">
                  No domains found. Add your first domain to get started.
                </td>
              </tr>
            ) : (
              domains.map((domain) => (
                <tr key={domain.name} className="border-b border-gray-200 hover:bg-gray-50">
                  <td className="px-6 py-4">
                    <div className="flex items-center">
                      <Globe className="w-5 h-5 text-gray-400 mr-3" />
                      <span className="font-medium text-gray-900">{domain.name}</span>
                    </div>
                  </td>
                  <td className="px-6 py-4">
                    {domain.is_active ? (
                      <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-green-100 text-green-800">
                        <CheckCircle className="w-3 h-3 mr-1" />
                        Active
                      </span>
                    ) : (
                      <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-red-100 text-red-800">
                        <XCircle className="w-3 h-3 mr-1" />
                        Inactive
                      </span>
                    )}
                  </td>
                  <td className="px-6 py-4 text-gray-900">{domain.max_accounts}</td>
                  <td className="px-6 py-4 text-gray-500">
                    {new Date(domain.created_at).toLocaleDateString()}
                  </td>
                  <td className="px-6 py-4 text-right">
                    <button
                      onClick={() => handleDeleteDomain(domain.name)}
                      className="text-red-600 hover:text-red-900 transition-colors"
                    >
                      <Trash2 className="w-5 h-5" />
                    </button>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      {/* Add Domain Modal */}
      {showAddModal && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
          <div className="bg-white rounded-lg shadow-xl p-6 w-full max-w-md">
            <h2 className="text-lg font-semibold text-gray-900 mb-4">Add New Domain</h2>
            <form onSubmit={handleAddDomain}>
              <div className="mb-4">
                <label className="block text-sm font-medium text-gray-700 mb-2">
                  Domain Name
                </label>
                <input
                  type="text"
                  value={newDomain.name}
                  onChange={(e) => setNewDomain({ ...newDomain, name: e.target.value })}
                  placeholder="example.com"
                  className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-violet-500"
                  required
                />
              </div>
              <div className="mb-6">
                <label className="block text-sm font-medium text-gray-700 mb-2">
                  Max Accounts
                </label>
                <input
                  type="number"
                  value={newDomain.max_accounts}
                  onChange={(e) => setNewDomain({ ...newDomain, max_accounts: parseInt(e.target.value) })}
                  className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-violet-500"
                  min="1"
                  required
                />
              </div>
              <div className="flex justify-end space-x-3">
                <button
                  type="button"
                  onClick={() => setShowAddModal(false)}
                  className="px-4 py-2 text-gray-700 hover:bg-gray-100 rounded-lg transition-colors"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  className="px-4 py-2 bg-violet-600 text-white rounded-lg hover:bg-violet-700 transition-colors"
                >
                  Add Domain
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  )
}

export default Domains
