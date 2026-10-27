import { it, expect, vi, afterEach } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { Dashboard } from './Dashboard'

// The stat tiles carried fixed "+0" green trend arrows (the server keeps no
// history) and "Messages Today" showed a count that was always 0.

afterEach(() => { vi.unstubAllGlobals(); document.body.innerHTML = '' })

it('shows the server stats without invented trends', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    if (String(url).includes('/stats')) {
      return { ok: true, status: 200, json: async () => ({ domains: 2, accounts: 7, messages: 1234, queue_size: 3 }) }
    }
    return { ok: true, status: 200, json: async () => ({}) }
  }))
  const c = document.createElement('div'); document.body.append(c)
  await act(async () => createRoot(c).render(createElement(Dashboard, { isConnected: true, activities: [] })))
  await act(async () => { await new Promise((r) => setTimeout(r, 0)) })
  const text = c.textContent ?? ''
  for (const want of ['1,234', 'Stored Messages', 'Messages in all mailboxes', '7', 'Total Accounts']) {
    expect(text).toContain(want)
  }
  expect(text).not.toContain('Messages Today')
  expect(text).not.toMatch(/\+0/)
  expect(c.querySelectorAll('svg.lucide-trending-up, svg.lucide-trending-down').length).toBe(0)
})
