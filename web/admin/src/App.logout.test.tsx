import { it, expect, vi } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import App from './App'

function setInputValue(input: HTMLInputElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(
    HTMLInputElement.prototype,
    'value'
  )?.set
  if (!setter) throw new Error('input value setter unavailable')
  setter.call(input, value)
  input.dispatchEvent(new Event('input', { bubbles: true }))
}

it('F-admin-logout logout revokes the server session, not just local state', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal(
    'matchMedia',
    (query: string) => ({
      matches: false,
      media: query,
      addListener() {},
      removeListener() {},
      addEventListener() {},
      removeEventListener() {},
      dispatchEvent() {
        return false
      },
    })
  )
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  vi.stubGlobal(
    'WebSocket',
    class {
      readyState = 1
      addEventListener() {}
      removeEventListener() {}
      close() {}
      send() {}
    }
  )

  const calls: { url: string; method: string }[] = []
  async function fetchStub(url: string, options: RequestInit = {}) {
    const method = options.method ?? 'GET'
    calls.push({ url, method })
    if (url.endsWith('/api/v1/accounts')) {
      return { ok: false, status: 401, json: async () => ({}) }
    }
    if (url.endsWith('/api/v1/auth/login')) {
      return { ok: true, status: 200, json: async () => ({}) }
    }
    if (url.endsWith('/api/v1/stats')) {
      return {
        ok: true,
        status: 200,
        json: async () => ({ domains: 0, accounts: 0, messages: 0, queue_size: 0 }),
      }
    }
    if (url.endsWith('/api/v1/auth/logout')) {
      return { ok: true, status: 200, json: async () => ({}) }
    }
    return { ok: true, status: 200, json: async () => ({}) }
  }
  vi.stubGlobal('fetch', vi.fn(fetchStub))

  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  try {
    await act(async () =>
      root.render(
        createElement(MemoryRouter, { initialEntries: ['/'] }, createElement(App))
      )
    )
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    // Control: unauthenticated shell shows the login form.
    expect(
      container.textContent?.includes('Sign in'),
      'CONTROL FAILED: login screen not shown on mount'
    ).toBe(true)
    console.log('CONTROL EXPECTED: login screen on unauthenticated mount | ACTUAL: shown')

    // Log in through the real form.
    const email = document.getElementById('email') as HTMLInputElement | null
    const password = document.getElementById('password') as HTMLInputElement | null
    if (!email || !password) throw new Error('login inputs not found')
    const loginForm = container.querySelector('form')
    if (!loginForm) throw new Error('login form not found')
    await act(async () => {
      setInputValue(email, 'admin@fixture.test')
      setInputValue(password, 'fixture-pw')
      loginForm.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    expect(
      container.textContent?.includes('admin@fixture.test'),
      'CONTROL FAILED: authenticated shell not rendered after login'
    ).toBe(true)
    const loginCalls = calls.filter((c) => c.url.endsWith('/api/v1/auth/login'))
    expect(loginCalls.length, 'CONTROL FAILED: login issued no POST').toBe(1)
    console.log('CONTROL EXPECTED: shell after one login POST | ACTUAL: shown')

    // Click the real sidebar logout button (the shell's only destructive-styled
    // button — sidebar.tsx hover:text-destructive, LogOut icon).
    calls.length = 0
    const logoutButton = Array.from(container.querySelectorAll('button')).find(
      (b) => (b.getAttribute('class') ?? '').includes('text-destructive')
    )
    if (!logoutButton) throw new Error('logout button not found in sidebar')
    await act(async () => logoutButton.click())
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    // CONTRACT: logout must revoke the server session via POST /api/v1/auth/logout
    // (server.go:417; server_auth.go:533 handleLogout blacklists token + clears cookie).
    const backToLogin = container.textContent?.includes('Sign in')
    const logoutRequested = calls.some(
      (c) => c.method === 'POST' && c.url.endsWith('/api/v1/auth/logout')
    )
    console.log(
      'EXPECTED: back on login screen + POST /auth/logout issued | ACTUAL: back to login:',
      backToLogin,
      '| logout requested:',
      logoutRequested
    )
    expect(backToLogin, 'CONTROL FAILED: local logout did not return to login screen').toBe(true)
    if (!logoutRequested) {
      console.log('PROBLEM CONFIRMED')
      expect(
        logoutRequested,
        'PROBLEM CONFIRMED: logout issues no request — the HttpOnly session cookie stays valid after logout'
      ).toBe(true)
    }
    console.log('PROBLEM NOT REPRODUCED')
    console.log('FIX VERIFIED')
  } finally {
    await act(async () => root.unmount())
    container.remove()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  }
})
