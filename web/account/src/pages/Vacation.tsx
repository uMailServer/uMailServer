import { useState, useEffect } from 'react'
import { Palmtree, Calendar, Clock, Mail, AlertCircle } from 'lucide-react'
import { useI18n } from '../hooks/useI18n'

interface VacationConfig {
  enabled: boolean
  start_date?: string
  end_date?: string
  subject: string
  message: string
  html_message?: string
  send_interval: number
  exclude_addresses: string[]
  ignore_lists: boolean
  ignore_bulk: boolean
}

function VacationPage() {
  const { t } = useI18n()
  const [config, setConfig] = useState<VacationConfig>({
    enabled: false,
    subject: '',
    message: '',
    send_interval: 168, // 7 days in hours
    exclude_addresses: [],
    ignore_lists: true,
    ignore_bulk: true
  })
  const [saving, setSaving] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')
  const [excludeInput, setExcludeInput] = useState('')

  useEffect(() => {
    loadConfig()
  }, [])

  const loadConfig = async () => {
    try {
      const response = await fetch('/api/v1/vacation', {
        credentials: 'include' // HttpOnly cookie handles auth
      })
      if (response.ok) {
        const data = await response.json()
        setConfig({
          enabled: data.enabled || false,
          subject: data.subject || t('vacation.defaultSubject'),
          message: data.message || t('vacation.defaultMessage'),
          start_date: data.start_date,
          end_date: data.end_date,
          html_message: data.html_message,
          send_interval: data.send_interval || 168,
          exclude_addresses: data.exclude_addresses || [],
          ignore_lists: data.ignore_lists !== false,
          ignore_bulk: data.ignore_bulk !== false
        })
      }
    } catch (err) {
      console.error('Failed to load vacation config:', err)
    } finally {
      setLoading(false)
    }
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setSuccess('')
    setSaving(true)

    try {
      const response = await fetch('/api/v1/vacation', {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json'
        },
        credentials: 'include', // HttpOnly cookie handles auth
        body: JSON.stringify(config)
      })

      if (!response.ok) {
        const data = await response.json()
        throw new Error(data.error || 'Failed to save vacation settings')
      }

      setSuccess(t('vacation.saveSuccess'))
    } catch (err: any) {
      setError(err.message || t('vacation.saveError'))
    } finally {
      setSaving(false)
    }
  }

  const handleAddExclude = () => {
    if (excludeInput && !config.exclude_addresses.includes(excludeInput)) {
      setConfig({
        ...config,
        exclude_addresses: [...config.exclude_addresses, excludeInput]
      })
      setExcludeInput('')
    }
  }

  const handleRemoveExclude = (email: string) => {
    setConfig({
      ...config,
      exclude_addresses: config.exclude_addresses.filter(e => e !== email)
    })
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary-600"></div>
      </div>
    )
  }

  return (
    <div>
      <h2 className="text-lg font-medium text-gray-900 mb-6">
        {t('vacation.title')}
      </h2>

      {error && (
        <div className="mb-4 p-4 bg-red-50 border border-red-200 rounded-md flex items-center text-red-700">
          <AlertCircle className="h-5 w-5 mr-2" />
          {error}
        </div>
      )}

      {success && (
        <div className="mb-4 p-4 bg-green-50 border border-green-200 rounded-md text-green-700">
          {success}
        </div>
      )}

      <form onSubmit={handleSubmit} className="space-y-6">
        {/* Enable/Disable Toggle */}
        <div className="flex items-center justify-between p-4 bg-gray-50 rounded-lg">
          <div className="flex items-center">
            <Palmtree className="h-5 w-5 text-primary-600 mr-3" />
            <div>
              <span className="block text-sm font-medium text-gray-900">
                {t('vacation.enableVacation')}
              </span>
              <span className="block text-sm text-gray-500">
                {t('vacation.enableDescription')}
              </span>
            </div>
          </div>
          <label className="relative inline-flex items-center cursor-pointer">
            <input
              type="checkbox"
              checked={config.enabled}
              onChange={(e) => setConfig({ ...config, enabled: e.target.checked })}
              className="sr-only peer"
            />
            <div className="w-11 h-6 bg-gray-200 peer-focus:outline-none peer-focus:ring-4 peer-focus:ring-primary-300 rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all peer-checked:bg-primary-600"></div>
          </label>
        </div>

        {config.enabled && (
          <>
            {/* Date Range */}
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div>
                <label className="flex items-center text-sm font-medium text-gray-700 mb-1">
                  <Calendar className="h-4 w-4 mr-1" />
                  {t('vacation.startDate')}
                </label>
                <input
                  type="datetime-local"
                  value={config.start_date ? config.start_date.slice(0, 16) : ''}
                  onChange={(e) => setConfig({ ...config, start_date: e.target.value })}
                  className="block w-full px-3 py-2 border border-gray-300 rounded-md shadow-sm focus:ring-primary-500 focus:border-primary-500"
                />
              </div>
              <div>
                <label className="flex items-center text-sm font-medium text-gray-700 mb-1">
                  <Calendar className="h-4 w-4 mr-1" />
                  {t('vacation.endDate')}
                </label>
                <input
                  type="datetime-local"
                  value={config.end_date ? config.end_date.slice(0, 16) : ''}
                  onChange={(e) => setConfig({ ...config, end_date: e.target.value })}
                  className="block w-full px-3 py-2 border border-gray-300 rounded-md shadow-sm focus:ring-primary-500 focus:border-primary-500"
                />
              </div>
            </div>

            {/* Subject */}
            <div>
              <label className="flex items-center text-sm font-medium text-gray-700 mb-1">
                <Mail className="h-4 w-4 mr-1" />
                {t('vacation.subject')}
              </label>
              <input
                type="text"
                value={config.subject}
                onChange={(e) => setConfig({ ...config, subject: e.target.value })}
                placeholder={t('vacation.subjectPlaceholder')}
                required
                className="block w-full px-3 py-2 border border-gray-300 rounded-md shadow-sm focus:ring-primary-500 focus:border-primary-500"
              />
            </div>

            {/* Message */}
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">
                {t('vacation.message')}
              </label>
              <textarea
                value={config.message}
                onChange={(e) => setConfig({ ...config, message: e.target.value })}
                placeholder={t('vacation.messagePlaceholder')}
                required
                rows={6}
                className="block w-full px-3 py-2 border border-gray-300 rounded-md shadow-sm focus:ring-primary-500 focus:border-primary-500"
              />
              <p className="mt-1 text-sm text-gray-500">
                {t('vacation.messageHelp')}
              </p>
            </div>

            {/* Send Interval */}
            <div>
              <label className="flex items-center text-sm font-medium text-gray-700 mb-1">
                <Clock className="h-4 w-4 mr-1" />
                {t('vacation.sendInterval')}
              </label>
              <select
                value={config.send_interval}
                onChange={(e) => setConfig({ ...config, send_interval: parseInt(e.target.value) })}
                className="block w-full px-3 py-2 border border-gray-300 rounded-md shadow-sm focus:ring-primary-500 focus:border-primary-500"
              >
                <option value={24}>{t('vacation.interval.1day')}</option>
                <option value={72}>{t('vacation.interval.3days')}</option>
                <option value={168}>{t('vacation.interval.7days')}</option>
                <option value={336}>{t('vacation.interval.14days')}</option>
              </select>
              <p className="mt-1 text-sm text-gray-500">
                {t('vacation.sendIntervalHelp')}
              </p>
            </div>

            {/* Exclude Addresses */}
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-2">
                {t('vacation.excludeAddresses')}
              </label>
              <div className="flex gap-2 mb-2">
                <input
                  type="email"
                  value={excludeInput}
                  onChange={(e) => setExcludeInput(e.target.value)}
                  placeholder={t('vacation.excludePlaceholder')}
                  className="flex-1 px-3 py-2 border border-gray-300 rounded-md shadow-sm focus:ring-primary-500 focus:border-primary-500"
                />
                <button
                  type="button"
                  onClick={handleAddExclude}
                  className="px-4 py-2 bg-gray-100 text-gray-700 rounded-md hover:bg-gray-200"
                >
                  {t('common.add')}
                </button>
              </div>
              {config.exclude_addresses.length > 0 && (
                <div className="flex flex-wrap gap-2">
                  {config.exclude_addresses.map(email => (
                    <span
                      key={email}
                      className="inline-flex items-center px-2 py-1 bg-gray-100 text-gray-700 rounded-md text-sm"
                    >
                      {email}
                      <button
                        type="button"
                        onClick={() => handleRemoveExclude(email)}
                        className="ml-1 text-gray-400 hover:text-red-500"
                      >
                        ×
                      </button>
                    </span>
                  ))}
                </div>
              )}
            </div>

            {/* Options */}
            <div className="space-y-3">
              <label className="flex items-center">
                <input
                  type="checkbox"
                  checked={config.ignore_lists}
                  onChange={(e) => setConfig({ ...config, ignore_lists: e.target.checked })}
                  className="h-4 w-4 text-primary-600 focus:ring-primary-500 border-gray-300 rounded"
                />
                <span className="ml-2 text-sm text-gray-700">
                  {t('vacation.ignoreLists')}
                </span>
              </label>
              <label className="flex items-center">
                <input
                  type="checkbox"
                  checked={config.ignore_bulk}
                  onChange={(e) => setConfig({ ...config, ignore_bulk: e.target.checked })}
                  className="h-4 w-4 text-primary-600 focus:ring-primary-500 border-gray-300 rounded"
                />
                <span className="ml-2 text-sm text-gray-700">
                  {t('vacation.ignoreBulk')}
                </span>
              </label>
            </div>
          </>
        )}

        <div className="flex justify-end pt-4 border-t">
          <button
            type="submit"
            disabled={saving}
            className="px-4 py-2 border border-transparent rounded-md shadow-sm text-sm font-medium text-white bg-primary-600 hover:bg-primary-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-primary-500 disabled:opacity-50"
          >
            {saving ? t('common.saving') : t('common.save')}
          </button>
        </div>
      </form>
    </div>
  )
}

export default VacationPage
