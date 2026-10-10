import { it, expect, vi } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { Queue } from './Queue'

// F6223/F6229: GET /api/v1/queue returns `to` as an array (db.QueueEntry.To is
// []string) — it was rendered concatenated — and a failed fetch left a blank card.
it('F6223 renders array recipients joined and F6229 shows an error state on failure', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const fetchMock = vi.fn()
  vi.stubGlobal('fetch', fetchMock)
  fetchMock.mockResolvedValueOnce({
    ok: true, status: 200,
    json: async () => [{ id: '1', from: 'a@x.com', to: ['b@y.com', 'c@z.com'], status: 'failed', retry_count: 1, created_at: '2026-01-01T00:00:00Z' }],
  })
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  try {
    await act(async () => root.render(createElement(Queue)))
    await act(async () => { await Promise.resolve() })
    expect(container.textContent).toContain('b@y.com, c@z.com')
    expect(container.textContent).not.toContain('b@y.comc@z.com')
  } finally {
    await act(async () => root.unmount()); container.remove()
  }

  fetchMock.mockResolvedValue({ ok: false, status: 500, json: async () => ({ error: 'queue unavailable' }) })
  const c2 = document.createElement('div')
  document.body.append(c2)
  const r2 = createRoot(c2)
  try {
    await act(async () => r2.render(createElement(Queue)))
    await act(async () => { await Promise.resolve() })
    expect(c2.textContent).toContain('queue unavailable')
  } finally {
    await act(async () => r2.unmount()); c2.remove(); vi.unstubAllGlobals()
  }
})
