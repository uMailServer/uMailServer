import { useState, useEffect } from 'react'
import { RefreshCw, Trash2, AlertCircle, CheckCircle, Clock } from 'lucide-react'

function Queue() {
  const [queue, setQueue] = useState([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetchQueue()
  }, [])

  const fetchQueue = async () => {
    try {
      const token = localStorage.getItem('adminToken')
      const response = await fetch('/api/v1/queue', {
        headers: {
          'Authorization': `Bearer ${token}`
        }
      })
      if (response.ok) {
        const data = await response.json()
        setQueue(data)
      }
    } catch (error) {
      console.error('Failed to fetch queue:', error)
    } finally {
      setLoading(false)
    }
  }

  const handleRetry = async (id) => {
    try {
      const token = localStorage.getItem('adminToken')
      const response = await fetch(`/api/v1/queue/${id}`, {
        method: 'POST',
        headers: {
          'Authorization': `Bearer ${token}`
        }
      })

      if (response.ok) {
        fetchQueue()
      }
    } catch (error) {
      console.error('Failed to retry:', error)
    }
  }

  const handleDrop = async (id) => {
    if (!confirm('Are you sure you want to drop this message?')) return

    try {
      const token = localStorage.getItem('adminToken')
      const response = await fetch(`/api/v1/queue/${id}`, {
        method: 'DELETE',
        headers: {
          'Authorization': `Bearer ${token}`
        }
      })

      if (response.ok) {
        fetchQueue()
      }
    } catch (error) {
      console.error('Failed to drop:', error)
    }
  }

  const getStatusIcon = (status) => {
    switch (status) {
      case 'pending':
        return <Clock className="w-5 h-5 text-yellow-500" />
      case 'failed':
        return <AlertCircle className="w-5 h-5 text-red-500" />
      case 'delivered':
        return <CheckCircle className="w-5 h-5 text-green-500" />
      default:
        return <Clock className="w-5 h-5 text-gray-500" />
    }
  }

  const getStatusBadge = (status) => {
    const classes = {
      pending: 'bg-yellow-100 text-yellow-800',
      failed: 'bg-red-100 text-red-800',
      delivered: 'bg-green-100 text-green-800'
    }

    return (
      <span className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${classes[status] || classes.pending}`}>
        {status}
      </span>
    )
  }

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold text-gray-900">Mail Queue</h1>
        <button
          onClick={fetchQueue}
          className="flex items-center px-4 py-2 bg-gray-100 text-gray-700 rounded-lg hover:bg-gray-200 transition-colors"
        >
          <RefreshCw className="w-5 h-5 mr-2" />
          Refresh
        </button>
      </div>

      {/* Queue Table */}
      <div className="bg-white rounded-lg shadow-sm border border-gray-200">
        <table className="w-full">
          <thead>
            <tr className="border-b border-gray-200">
              <th className="px-6 py-3 text-left text-sm font-medium text-gray-500">Status</th>
              <th className="px-6 py-3 text-left text-sm font-medium text-gray-500">From</th>
              <th className="px-6 py-3 text-left text-sm font-medium text-gray-500">To</th>
              <th className="px-6 py-3 text-left text-sm font-medium text-gray-500">Retry Count</th>
              <th className="px-6 py-3 text-left text-sm font-medium text-gray-500">Last Error</th>
              <th className="px-6 py-3 text-right text-sm font-medium text-gray-500">Actions</th>
            </tr>
          </thead>
          <tbody>
            {queue.length === 0 ? (
              <tr>
                <td colSpan="6" className="px-6 py-8 text-center text-gray-500">
                  Queue is empty. No messages pending.
                </td>
              </tr>
            ) : (
              queue.map((entry) => (
                <tr key={entry.id} className="border-b border-gray-200 hover:bg-gray-50">
                  <td className="px-6 py-4">
                    <div className="flex items-center">
                      {getStatusIcon(entry.status)}
                      <span className="ml-2">{getStatusBadge(entry.status)}</span>
                    </div>
                  </td>
                  <td className="px-6 py-4 text-gray-900">{entry.from}</td>
                  <td className="px-6 py-4 text-gray-900">{entry.to.join(', ')}</td>
                  <td className="px-6 py-4 text-gray-500">{entry.retry_count}</td>
                  <td className="px-6 py-4 text-red-600 text-sm max-w-xs truncate">
                    {entry.last_error || '-'}
                  </td>
                  <td className="px-6 py-4 text-right">
                    <div className="flex items-center justify-end space-x-2">
                      {entry.status === 'failed' && (
                        <button
                          onClick={() => handleRetry(entry.id)}
                          className="p-2 text-blue-600 hover:bg-blue-50 rounded-lg transition-colors"
                          title="Retry"
                        >
                          <RefreshCw className="w-5 h-5" />
                        </button>
                      )}
                      <button
                        onClick={() => handleDrop(entry.id)}
                        className="p-2 text-red-600 hover:bg-red-50 rounded-lg transition-colors"
                        title="Drop"
                      >
                        <Trash2 className="w-5 h-5" />
                      </button>
                    </div>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}

export default Queue
