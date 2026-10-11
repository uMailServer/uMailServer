import { it, expect, vi, afterEach } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { Accounts } from './Accounts'
import { Queue } from './Queue'
import { useAccounts } from '@/hooks/useApi'
import { formatBytes, gbToBytes, bytesToGb } from '@/lib/utils'

const flush = () => act(async () => { await new Promise((r) => setTimeout(r, 0)) })
const GB = 1024 ** 3
function setInputValue(input: HTMLInputElement, value: string) {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
  input.dispatchEvent(new Event('input', { bubbles: true }))
}
const btn = (root: ParentNode, label: string) =>
  Array.from(root.querySelectorAll('button')).filter((b) => b.textContent?.trim() === label)
afterEach(() => { vi.unstubAllGlobals() })

async function withPage(el: Parameters<typeof createElement>[0], fn: (c: HTMLElement) => Promise<void>) {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  try {
    await act(async () => root.render(createElement(el)))
    await flush()
    await fn(container)
  } finally {
    await act(async () => root.unmount()); container.remove()
  }
}

async function openEdit(c: HTMLElement) {
  await act(async () => (c.querySelector('.group button') as HTMLButtonElement).click())
  await flush()
  const edit = Array.from(document.querySelectorAll('[role="menuitem"]')).find((e) => e.textContent?.trim() === 'Edit') as HTMLElement
  await act(async () => edit.click())
  await flush()
}

const acct = { email: 'a@example.com', is_admin: false, is_active: true, quota_used: GB, quota_limit: 2 * GB, created_at: '', updated_at: '' }

it('F6943 apiRequest generic 429 surfaces Retry-After', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('fetch', vi.fn(async () => ({
    ok: false, status: 429,
    headers: { get: (h: string) => (h.toLowerCase() === 'retry-after' ? '45' : null) },
    json: async () => ({ error: 'rate limit exceeded' }),
  })))
  let a!: ReturnType<typeof useAccounts>
  const container = document.createElement('div'); document.body.append(container)
  const root = createRoot(container)
  try {
    await act(async () => root.render(createElement(function P() { a = useAccounts(); return null })))
    let msg = ''
    await act(async () => { try { await a.fetchAccounts() } catch (e) { msg = (e as Error).message } })
    expect(msg).toContain('rate limit exceeded')
    expect(msg, 'PROBLEM CONFIRMED: Retry-After ignored').toContain('45')
  } finally {
    await act(async () => root.unmount()); container.remove()
  }
})

it('F6944 edit dialog has a quota field and PUTs quota_limit in bytes', async () => {
  const calls: { url: string; o: RequestInit }[] = []
  vi.stubGlobal('fetch', vi.fn(async (u: string, o: RequestInit = {}) => {
    calls.push({ url: u, o })
    if (o.method === 'PUT') return { ok: true, status: 200, json: async () => acct }
    return { ok: true, status: 200, json: async () => [acct] }
  }))
  await withPage(Accounts, async (c) => {
    await openEdit(c)
    const q = document.querySelector('#edit-quota') as HTMLInputElement
    expect(q, 'PROBLEM CONFIRMED: no quota field').toBeTruthy()
    expect(q.value).toBe('2')
    await act(async () => setInputValue(q, '5'))
    await act(async () => btn(document.body, 'Save Changes')[0].click())
    await flush()
    const put = calls.find((x) => x.o.method === 'PUT')!
    expect(JSON.parse(put.o.body as string).quota_limit).toBe(5 * GB)
  })
})

it('F6944 unchanged quota is not re-sent; invalid quota is rejected', async () => {
  const calls: { o: RequestInit }[] = []
  vi.stubGlobal('fetch', vi.fn(async (_u: string, o: RequestInit = {}) => {
    calls.push({ o })
    if (o.method === 'PUT') return { ok: true, status: 200, json: async () => acct }
    return { ok: true, status: 200, json: async () => [{ ...acct, quota_limit: 1.5 * GB + 7 }] }
  }))
  await withPage(Accounts, async (c) => {
    await openEdit(c)
    await act(async () => btn(document.body, 'Save Changes')[0].click())
    await flush()
    const put = calls.find((x) => x.o.method === 'PUT')!
    expect('quota_limit' in JSON.parse(put.o.body as string), 'rounded display value must not overwrite exact limit').toBe(false)
  })
  calls.length = 0
  await withPage(Accounts, async (c) => {
    await openEdit(c)
    await act(async () => setInputValue(document.querySelector('#edit-quota') as HTMLInputElement, '-3'))
    await act(async () => btn(document.body, 'Save Changes')[0].click())
    await flush()
    expect(calls.some((x) => x.o.method === 'PUT')).toBe(false)
    expect(document.body.textContent).toContain('Quota')
  })
})

it('F6944 507 from the server is shown as a quota message', async () => {
  vi.stubGlobal('fetch', vi.fn(async (_u: string, o: RequestInit = {}) => {
    if (o.method === 'PUT') return { ok: false, status: 507, json: async () => ({ error: 'Quota exceeded' }) }
    return { ok: true, status: 200, json: async () => [acct] }
  }))
  await withPage(Accounts, async (c) => {
    await openEdit(c)
    await act(async () => btn(document.body, 'Save Changes')[0].click())
    await flush()
    expect(document.body.textContent).toMatch(/storage quota/i)
  })
})

it('F6944 card shows used / limit, Unlimited for 0, and Over quota past the limit', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => ({
    ok: true, status: 200,
    json: async () => [
      { ...acct, email: 'u@x.com', quota_used: GB, quota_limit: 0 },
      { ...acct, email: 'o@x.com', quota_used: 3 * GB, quota_limit: 2 * GB },
    ],
  })))
  await withPage(Accounts, async (c) => {
    expect(c.textContent, 'PROBLEM CONFIRMED: 0 limit rendered as 0 B').toContain('Unlimited')
    expect(c.textContent).not.toContain('1 GB / 0 B')
    expect(c.textContent).toContain('Over quota')
    expect(c.textContent).not.toContain('150%')
  })
})

it('F6944 create with a quota creates then sets quota_limit', async () => {
  const calls: { url: string; o: RequestInit }[] = []
  vi.stubGlobal('fetch', vi.fn(async (u: string, o: RequestInit = {}) => {
    calls.push({ url: u, o })
    if (o.method === 'POST' || o.method === 'PUT') return { ok: true, status: 200, json: async () => acct }
    return { ok: true, status: 200, json: async () => [] }
  }))
  await withPage(Accounts, async (c) => {
    await act(async () => btn(c, 'Add Account')[0].click())
    await flush()
    await act(async () => {
      setInputValue(document.querySelector('#email') as HTMLInputElement, 'n@example.com')
      setInputValue(document.querySelector('#password') as HTMLInputElement, 'Str0ng!Passw0rd#1')
      setInputValue(document.querySelector('#quota') as HTMLInputElement, '1.5')
    })
    await act(async () => btn(document.body, 'Create Account')[0].click())
    await flush()
    const put = calls.find((x) => x.o.method === 'PUT')
    expect(put, 'PROBLEM CONFIRMED: quota never sent').toBeTruthy()
    expect(put!.url).toContain('n%40example.com')
    expect(JSON.parse(put!.o.body as string).quota_limit).toBe(1.5 * GB)
  })
})

it('F6945 queue has no misleading Delivered counter or filter', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, status: 200, json: async () => [
    { id: '1', from: 'a@x.com', to: ['b@y.com'], status: 'pending', retry_count: 0, created_at: '2026-01-01T00:00:00Z' },
  ] })))
  await withPage(Queue, async (c) => {
    expect(c.textContent).toContain('Pending')
    expect(c.textContent, 'PROBLEM CONFIRMED: Delivered counter always 0').not.toContain('Delivered')
  })
})

it('F6944 byte helpers use binary units consistent with the backend config.GB', () => {
  expect(formatBytes(0)).toBe('0 B')
  expect(formatBytes(GB)).toBe('1 GB')
  expect(formatBytes(1536)).toBe('1.5 KB')
  expect(gbToBytes('1.5')).toBe(1.5 * GB)
  expect(gbToBytes('0')).toBe(0)
  expect(gbToBytes('')).toBeNull()
  expect(gbToBytes('-1')).toBeNull()
  expect(gbToBytes('abc')).toBeNull()
  expect(bytesToGb(2 * GB)).toBe('2')
  expect(bytesToGb(0)).toBe('0')
})

it('F6946 icon-only buttons have accessible names (accounts)', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, status: 200, json: async () => [acct] })))
  await withPage(Accounts, async (c) => {
    const unnamed = Array.from(c.querySelectorAll('button')).filter((b) => !b.textContent?.trim() && !b.getAttribute('aria-label') && !b.querySelector('button[aria-label]'))
    expect(unnamed.length, 'PROBLEM CONFIRMED: icon buttons without aria-label').toBe(0)
  })
})

it('F6947 queue shows a placeholder, not "Invalid Date", for an unparseable timestamp', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, status: 200, json: async () => [
    { id: '1', from: 'a@x.com', to: ['b@y.com'], status: 'pending', retry_count: 0, created_at: '' },
  ] })))
  await withPage(Queue, async (c) => {
    expect(c.textContent).not.toContain('Invalid Date')
  })
})
