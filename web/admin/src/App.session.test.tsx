import { it, expect, vi, afterEach } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import App from './App'

// F6860/F6861: the realtime stream must not be opened for a logged-out
// shell (401 reconnect budget burned before login), and a non-admin who
// logs in must be refused instead of landing in a panel where every call 403s.

function setInputValue(input: HTMLInputElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
  setter!.call(input, value)
  input.dispatchEvent(new Event('input', { bubbles: true }))
}

let sources = 0
class FakeES {
  constructor() { sources++ }
  addEventListener() {}
  close() {}
}

function setup(adminCheckStatus: number) {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('matchMedia', (q: string) => ({ matches: false, media: q, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {}, dispatchEvent: () => false }))
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  vi.stubGlobal('EventSource', FakeES)
  sources = 0
  const calls: { url: string; method: string }[] = []
  let loggedIn = false
  vi.stubGlobal('fetch', vi.fn(async (url: string, o: RequestInit = {}) => {
    calls.push({ url, method: o.method ?? 'GET' })
    if (url.endsWith('/api/v1/auth/login')) { loggedIn = true; return { ok: true, status: 200, json: async () => ({}) } }
    if (url.includes('/api/v1/accounts')) {
      if (!loggedIn) return { ok: false, status: 401, json: async () => ({}) }
      const ok = adminCheckStatus === 200
      return { ok, status: adminCheckStatus, json: async () => (ok ? [] : { error: 'admin access required' }) }
    }
    return { ok: true, status: 200, json: async () => ({ domains: 0, accounts: 0, messages: 0, queue_size: 0 }) }
  }))
  return calls
}

async function loginFlow(container: HTMLElement) {
  await act(async () => { await new Promise((r) => setTimeout(r, 0)) })
  const form = container.querySelector('form')!
  await act(async () => {
    setInputValue(document.getElementById('email') as HTMLInputElement, 'user@fixture.test')
    setInputValue(document.getElementById('password') as HTMLInputElement, 'pw')
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    await new Promise((r) => setTimeout(r, 0))
  })
  await act(async () => { await new Promise((r) => setTimeout(r, 0)) })
}

async function mount() {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  await act(async () => root.render(createElement(MemoryRouter, { initialEntries: ['/'] }, createElement(App))))
  return { container, root }
}

afterEach(() => { vi.unstubAllGlobals() })

it('F6860 does not open the event stream while logged out, opens it after login', async () => {
  setup(200)
  const { container, root } = await mount()
  try {
    await act(async () => { await new Promise((r) => setTimeout(r, 0)) })
    expect(sources, 'PROBLEM CONFIRMED: EventSource opened on the login screen').toBe(0)
    await loginFlow(container)
    expect(sources, 'stream must connect once authenticated').toBeGreaterThan(0)
  } finally {
    await act(async () => root.unmount()); container.remove()
  }
})

it('F6861 refuses a non-admin login and revokes the session', async () => {
  const calls = setup(403)
  const { container, root } = await mount()
  try {
    await loginFlow(container)
    expect(container.textContent, 'PROBLEM CONFIRMED: non-admin got the admin shell').toContain('Sign in')
    expect(container.textContent).toMatch(/administrator/i)
    expect(calls.some((c) => c.method === 'POST' && c.url.endsWith('/auth/logout'))).toBe(true)
  } finally {
    await act(async () => root.unmount()); container.remove()
  }
})
