import { useState, useEffect } from 'react'
import { Filter, Plus, Trash2, Edit2, X, MoveUp, MoveDown } from 'lucide-react'
import { useI18n } from '../hooks/useI18n'

interface FilterCondition {
  field: 'from' | 'to' | 'subject' | 'body' | 'header'
  operator: 'contains' | 'equals' | 'startsWith' | 'endsWith' | 'matches'
  value: string
  headerName?: string
}

interface FilterAction {
  type: 'move' | 'copy' | 'delete' | 'markRead' | 'markSpam' | 'forward' | 'flag'
  target?: string
  forwardTo?: string
}

interface EmailFilter {
  id: string
  name: string
  enabled: boolean
  matchAll: boolean
  conditions: FilterCondition[]
  actions: FilterAction[]
  priority: number
}

const validFilterFields: FilterCondition['field'][] = ['from', 'to', 'subject', 'body', 'header']
const validFilterOperators: FilterCondition['operator'][] = ['contains', 'equals', 'startsWith', 'endsWith', 'matches']
const validFilterActions: FilterAction['type'][] = ['move', 'copy', 'delete', 'markRead', 'markSpam', 'forward', 'flag']

function parseFilterField(value: string): FilterCondition['field'] {
  if (validFilterFields.includes(value as FilterCondition['field'])) {
    return value as FilterCondition['field']
  }
  return 'from'
}

function parseFilterOperator(value: string): FilterCondition['operator'] {
  if (validFilterOperators.includes(value as FilterCondition['operator'])) {
    return value as FilterCondition['operator']
  }
  return 'contains'
}

function parseFilterAction(value: string): FilterAction['type'] {
  if (validFilterActions.includes(value as FilterAction['type'])) {
    return value as FilterAction['type']
  }
  return 'move'
}

function FiltersPage() {
  const { t } = useI18n()
  const [filters, setFilters] = useState<EmailFilter[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [editingFilter, setEditingFilter] = useState<EmailFilter | null>(null)
  const [showAddForm, setShowAddForm] = useState(false)

  useEffect(() => {
    loadFilters()
  }, [])

  const loadFilters = async () => {
    try {
      const response = await fetch('/api/v1/filters', {
        credentials: 'include' // HttpOnly cookie handles auth
      })
      if (response.ok) {
        const data = await response.json()
        setFilters(data.filters || [])
      }
    } catch (err) {
      console.error('Failed to load filters:', err)
    } finally {
      setLoading(false)
    }
  }

  const handleSave = async (filter: EmailFilter) => {
    setSaving(true)
    try {
      const method = filter.id ? 'PUT' : 'POST'
      const url = filter.id ? `/api/v1/filters/${filter.id}` : '/api/v1/filters'

      const response = await fetch(url, {
        method,
        headers: {
          'Content-Type': 'application/json'
        },
        credentials: 'include', // HttpOnly cookie handles auth
        body: JSON.stringify(filter)
      })

      if (response.ok) {
        await loadFilters()
        setEditingFilter(null)
        setShowAddForm(false)
      }
    } catch (err) {
      console.error('Failed to save filter:', err)
    } finally {
      setSaving(false)
    }
  }

  const handleDelete = async (id: string) => {
    if (!confirm(t('filters.deleteConfirm'))) return

    try {
      const response = await fetch(`/api/v1/filters/${id}`, {
        method: 'DELETE',
        credentials: 'include' // HttpOnly cookie handles auth
      })

      if (response.ok) {
        await loadFilters()
      }
    } catch (err) {
      console.error('Failed to delete filter:', err)
    }
  }

  const handleToggle = async (filter: EmailFilter) => {
    try {
      const response = await fetch(`/api/v1/filters/${filter.id}/toggle`, {
        method: 'POST',
        credentials: 'include' // HttpOnly cookie handles auth
      })

      if (response.ok) {
        await loadFilters()
      }
    } catch (err) {
      console.error('Failed to toggle filter:', err)
    }
  }

  const handleMove = async (index: number, direction: 'up' | 'down') => {
    const newIndex = direction === 'up' ? index - 1 : index + 1
    if (newIndex < 0 || newIndex >= filters.length) return

    const newFilters = [...filters]
    const temp = newFilters[index]
    newFilters[index] = newFilters[newIndex]
    newFilters[newIndex] = temp

    // Update priorities
    newFilters.forEach((f, i) => {
      f.priority = i
    })

    setFilters(newFilters)

    // Save new order
    try {
      await fetch('/api/v1/filters/reorder', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json'
        },
        credentials: 'include', // HttpOnly cookie handles auth
        body: JSON.stringify({ filterIds: newFilters.map(f => f.id) })
      })
    } catch (err) {
      console.error('Failed to reorder filters:', err)
    }
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
      <div className="flex items-center justify-between mb-6">
        <h2 className="text-lg font-medium text-gray-900">
          {t('filters.title')}
        </h2>
        <button
          onClick={() => setShowAddForm(true)}
          className="flex items-center px-4 py-2 bg-primary-600 text-white rounded-md hover:bg-primary-700"
        >
          <Plus className="h-4 w-4 mr-2" />
          {t('filters.create')}
        </button>
      </div>

      {(showAddForm || editingFilter) && (
        <FilterEditor
          filter={editingFilter}
          onSave={handleSave}
          onCancel={() => {
            setEditingFilter(null)
            setShowAddForm(false)
          }}
          saving={saving}
        />
      )}

      <div className="space-y-4">
        {filters.length === 0 && !showAddForm ? (
          <div className="text-center py-12 text-gray-500">
            <Filter className="h-12 w-12 mx-auto mb-4 text-gray-300" />
            <p>{t('filters.noFilters')}</p>
            <p className="text-sm mt-2">{t('filters.noFiltersDescription')}</p>
          </div>
        ) : (
          filters.map((filter, index) => (
            <div
              key={filter.id}
              className={`border rounded-lg p-4 ${filter.enabled ? 'bg-white' : 'bg-gray-50'}`}
            >
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-3">
                  <label className="relative inline-flex items-center cursor-pointer">
                    <input
                      type="checkbox"
                      checked={filter.enabled}
                      onChange={() => handleToggle(filter)}
                      className="sr-only peer"
                    />
                    <div className="w-11 h-6 bg-gray-200 peer-focus:outline-none peer-focus:ring-4 peer-focus:ring-primary-300 rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all peer-checked:bg-primary-600"></div>
                  </label>
                  <div>
                    <h3 className={`font-medium ${filter.enabled ? 'text-gray-900' : 'text-gray-500'}`}>
                      {filter.name}
                    </h3>
                    <p className="text-sm text-gray-500">
                      {filter.matchAll ? t('filters.matchAll') : t('filters.matchAny')} ·
                      {' '}{filter.conditions.length} {t('filters.conditions')} ·
                      {' '}{filter.actions.length} {t('filters.actions')}
                    </p>
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <button
                    onClick={() => handleMove(index, 'up')}
                    disabled={index === 0}
                    className="p-2 text-gray-400 hover:text-gray-600 disabled:opacity-30"
                  >
                    <MoveUp className="h-4 w-4" />
                  </button>
                  <button
                    onClick={() => handleMove(index, 'down')}
                    disabled={index === filters.length - 1}
                    className="p-2 text-gray-400 hover:text-gray-600 disabled:opacity-30"
                  >
                    <MoveDown className="h-4 w-4" />
                  </button>
                  <button
                    onClick={() => setEditingFilter(filter)}
                    className="p-2 text-gray-400 hover:text-blue-600"
                  >
                    <Edit2 className="h-4 w-4" />
                  </button>
                  <button
                    onClick={() => handleDelete(filter.id)}
                    className="p-2 text-gray-400 hover:text-red-600"
                  >
                    <Trash2 className="h-4 w-4" />
                  </button>
                </div>
              </div>
            </div>
          ))
        )}
      </div>
    </div>
  )
}

interface FilterEditorProps {
  filter: EmailFilter | null
  onSave: (filter: EmailFilter) => void
  onCancel: () => void
  saving: boolean
}

function FilterEditor({ filter, onSave, onCancel, saving }: FilterEditorProps) {
  const { t } = useI18n()
  const [name, setName] = useState(filter?.name || '')
  const [matchAll, setMatchAll] = useState(filter?.matchAll ?? true)
  const [conditions, setConditions] = useState<FilterCondition[]>(
    filter?.conditions || [{ field: 'from', operator: 'contains', value: '' }]
  )
  const [actions, setActions] = useState<FilterAction[]>(
    filter?.actions || [{ type: 'move', target: 'INBOX' }]
  )

  const addCondition = () => {
    setConditions([...conditions, { field: 'from', operator: 'contains', value: '' }])
  }

  const removeCondition = (index: number) => {
    setConditions(conditions.filter((_, i) => i !== index))
  }

  const updateCondition = (index: number, updates: Partial<FilterCondition>) => {
    setConditions(conditions.map((c, i) => i === index ? { ...c, ...updates } : c))
  }

  const addAction = () => {
    setActions([...actions, { type: 'markRead' }])
  }

  const removeAction = (index: number) => {
    setActions(actions.filter((_, i) => i !== index))
  }

  const updateAction = (index: number, updates: Partial<FilterAction>) => {
    setActions(actions.map((a, i) => i === index ? { ...a, ...updates } : a))
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    onSave({
      id: filter?.id || '',
      name,
      enabled: filter?.enabled ?? true,
      matchAll,
      conditions,
      actions,
      priority: filter?.priority ?? 0
    })
  }

  return (
    <form onSubmit={handleSubmit} className="mb-8 bg-gray-50 rounded-lg p-6 border">
      <h3 className="text-lg font-medium mb-4">
        {filter ? t('filters.edit') : t('filters.create')}
      </h3>

      <div className="mb-6">
        <label className="block text-sm font-medium text-gray-700 mb-1">
          {t('filters.filterName')}
        </label>
        <input
          type="text"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder={t('filters.namePlaceholder')}
          required
          className="block w-full px-3 py-2 border border-gray-300 rounded-md shadow-sm focus:ring-primary-500 focus:border-primary-500"
        />
      </div>

      <div className="mb-6">
        <div className="flex items-center gap-4 mb-3">
          <span className="text-sm font-medium text-gray-700">{t('filters.match')}</span>
          <select
            value={matchAll ? 'all' : 'any'}
            onChange={(e) => setMatchAll(e.target.value === 'all')}
            className="px-3 py-1 border border-gray-300 rounded-md text-sm"
          >
            <option value="all">{t('filters.matchAll')}</option>
            <option value="any">{t('filters.matchAny')}</option>
          </select>
          <span className="text-sm text-gray-500">{t('filters.followingConditions')}</span>
        </div>

        <div className="space-y-3">
          {conditions.map((condition, index) => (
            <div key={index} className="flex items-center gap-2">
              <select
                value={condition.field}
                onChange={(e) => updateCondition(index, { field: parseFilterField(e.target.value) })}
                className="px-3 py-2 border border-gray-300 rounded-md text-sm"
              >
                <option value="from">{t('filters.from')}</option>
                <option value="to">{t('filters.to')}</option>
                <option value="subject">{t('filters.subject')}</option>
                <option value="body">{t('filters.body')}</option>
                <option value="header">{t('filters.header')}</option>
              </select>

              {condition.field === 'header' && (
                <input
                  type="text"
                  value={condition.headerName || ''}
                  onChange={(e) => updateCondition(index, { headerName: e.target.value })}
                  placeholder={t('filters.headerName')}
                  className="px-3 py-2 border border-gray-300 rounded-md text-sm w-32"
                />
              )}

              <select
                value={condition.operator}
                onChange={(e) => updateCondition(index, { operator: parseFilterOperator(e.target.value) })}
                className="px-3 py-2 border border-gray-300 rounded-md text-sm"
              >
                <option value="contains">{t('filters.contains')}</option>
                <option value="equals">{t('filters.equals')}</option>
                <option value="startsWith">{t('filters.startsWith')}</option>
                <option value="endsWith">{t('filters.endsWith')}</option>
                <option value="matches">{t('filters.matches')}</option>
              </select>

              <input
                type="text"
                value={condition.value}
                onChange={(e) => updateCondition(index, { value: e.target.value })}
                placeholder={t('filters.valuePlaceholder')}
                required
                className="flex-1 px-3 py-2 border border-gray-300 rounded-md text-sm"
              />

              <button
                type="button"
                onClick={() => removeCondition(index)}
                disabled={conditions.length === 1}
                className="p-2 text-gray-400 hover:text-red-600 disabled:opacity-30"
              >
                <X className="h-4 w-4" />
              </button>
            </div>
          ))}
        </div>

        <button
          type="button"
          onClick={addCondition}
          className="mt-3 text-sm text-primary-600 hover:text-primary-700"
        >
          + {t('filters.addCondition')}
        </button>
      </div>

      <div className="mb-6">
        <h4 className="text-sm font-medium text-gray-700 mb-3">{t('filters.performActions')}</h4>
        <div className="space-y-3">
          {actions.map((action, index) => (
            <div key={index} className="flex items-center gap-2">
              <select
                value={action.type}
                onChange={(e) => updateAction(index, { type: parseFilterAction(e.target.value) })}
                className="px-3 py-2 border border-gray-300 rounded-md text-sm"
              >
                <option value="move">{t('filters.moveTo')}</option>
                <option value="copy">{t('filters.copyTo')}</option>
                <option value="delete">{t('filters.delete')}</option>
                <option value="markRead">{t('filters.markRead')}</option>
                <option value="markSpam">{t('filters.markSpam')}</option>
                <option value="forward">{t('filters.forward')}</option>
                <option value="flag">{t('filters.flag')}</option>
              </select>

              {(action.type === 'move' || action.type === 'copy') && (
                <select
                  value={action.target || 'INBOX'}
                  onChange={(e) => updateAction(index, { target: e.target.value })}
                  className="px-3 py-2 border border-gray-300 rounded-md text-sm"
                >
                  <option value="INBOX">{t('filters.inbox')}</option>
                  <option value="Sent">{t('filters.sent')}</option>
                  <option value="Drafts">{t('filters.drafts')}</option>
                  <option value="Trash">{t('filters.trash')}</option>
                  <option value="Junk">{t('filters.junk')}</option>
                  <option value="Archive">{t('filters.archive')}</option>
                </select>
              )}

              {action.type === 'forward' && (
                <input
                  type="email"
                  value={action.forwardTo || ''}
                  onChange={(e) => updateAction(index, { forwardTo: e.target.value })}
                  placeholder={t('filters.forwardToPlaceholder')}
                  required
                  className="flex-1 px-3 py-2 border border-gray-300 rounded-md text-sm"
                />
              )}

              <button
                type="button"
                onClick={() => removeAction(index)}
                disabled={actions.length === 1}
                className="p-2 text-gray-400 hover:text-red-600 disabled:opacity-30"
              >
                <X className="h-4 w-4" />
              </button>
            </div>
          ))}
        </div>

        <button
          type="button"
          onClick={addAction}
          className="mt-3 text-sm text-primary-600 hover:text-primary-700"
        >
          + {t('filters.addAction')}
        </button>
      </div>

      <div className="flex justify-end gap-3">
        <button
          type="button"
          onClick={onCancel}
          className="px-4 py-2 border border-gray-300 rounded-md text-sm font-medium text-gray-700 hover:bg-gray-50"
        >
          {t('common.cancel')}
        </button>
        <button
          type="submit"
          disabled={saving}
          className="px-4 py-2 bg-primary-600 text-white rounded-md text-sm font-medium hover:bg-primary-700 disabled:opacity-50"
        >
          {saving ? t('common.saving') : t('common.save')}
        </button>
      </div>
    </form>
  )
}

export default FiltersPage
