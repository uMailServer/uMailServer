import { it, expect, vi, afterEach } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { Accounts } from './Accounts'
import { Login } from './Login'
import { MemoryRouter } from 'react-router-dom'

const flush = () => act(async () => { await new Promise((r) => setTimeout(r, 0)) })
const btn = (root: ParentNode, label: string) =>
  Array.from(root.querySelectorAll('button')).filter((b) => b.textContent?.trim() === label)
function setInputValue(input: HTMLInputElement, value: string) {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
  input.dispatchEvent(new Event('input', { bubbles: true }))
}
afterEach(() => { vi.unstubAllGlobals() })

const acct = { email: 'a@example.com', is_admin: true, is_active: true, quota_used: 0, quota_limit: 0, created_at: '', updated_at: '' }

it('F6864 a failed delete (409 last admin) does not leak into the create dialog', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('fetch', vi.fn(async (_u: string, o: RequestInit = {}) => {
    if (o.method === 'DELETE') return { ok: false, status: 409, json: async () => ({ error: 'cannot delete the last active administrator' }) }
    return { ok: true, status: 200, json: async () => [acct] }
  }))
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  try {
    await act(async () => root.render(createElement(Accounts)))
    await flush()
    // open the card menu -> Delete
    await act(async () => (container.querySelector('.group button') as HTMLButtonElement).click())
    await flush()
    const del = Array.from(document.querySelectorAll('[role="menuitem"]')).find((e) => e.textContent?.trim() === 'Delete') as HTMLElement
    await act(async () => del.click())
    await flush()
    await act(async () => btn(document.body, 'Delete').pop()!.click())
    await flush()
    expect(document.body.textContent, 'control: 409 message shown').toContain('cannot delete the last active administrator')
    await act(async () => btn(document.body, 'Cancel')[0].click())
    await flush()
    await act(async () => btn(container, 'Add Account')[0].click())
    await flush()
    expect(document.body.textContent, 'PROBLEM CONFIRMED: stale delete error in create dialog')
      .not.toContain('cannot delete the last active administrator')
  } finally {
    await act(async () => root.unmount()); container.remove()
  }
})

it('F6865 login 429 surfaces Retry-After', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('fetch', vi.fn(async () => ({
    ok: false, status: 429,
    headers: { get: (h: string) => (h.toLowerCase() === 'retry-after' ? '120' : null) },
    json: async () => ({ error: 'too many login attempts' }),
  })))
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  try {
    await act(async () => root.render(createElement(MemoryRouter, null, createElement(Login, { onLogin: vi.fn() }))))
    await act(async () => {
      setInputValue(container.querySelector('#email') as HTMLInputElement, 'a@example.com')
      setInputValue(container.querySelector('#password') as HTMLInputElement, 'pw')
    })
    await act(async () => (container.querySelector('button[type="submit"]') as HTMLButtonElement).click())
    await flush()
    expect(container.textContent).toContain('too many login attempts')
    expect(container.textContent, 'PROBLEM CONFIRMED: Retry-After ignored').toContain('120')
  } finally {
    await act(async () => root.unmount()); container.remove()
  }
})
