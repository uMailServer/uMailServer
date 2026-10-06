import { it, expect, vi } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { ThemeProvider } from '@/components/theme-provider'
import { AuthProvider, useAuth } from './AuthContext'
import { EmailProvider } from './EmailContext'

// F-email-auth REGRESSION (introduced by the round-12 provider mount):
// EmailProvider now mounts at the app root — including the unauthenticated
// /login route — and its mount effect calls loadEmails() unconditionally.
// Facet A (unauthenticated): GET /mail/inbox fires without a session; the api
// layer answers every 401 with `window.location.href = '/login'`, a full-page
// reload while ALREADY on /login → reload loop.
// Contract A: an unauthenticated session must not trigger authenticated calls.
// Facet B (post-login): the provider never remounts, so after a successful
// login nothing re-fetches and the unread badge stays 0.
// Contract B: loadEmails must run when authentication becomes true.

let current!: ReturnType<typeof useAuth>
function Probe() {
  current = useAuth()
  return null
}

it('F-email-auth EmailProvider fetches only when authenticated, and re-fetches on login', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('matchMedia', vi.fn().mockReturnValue({
    matches: false,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }))

  const calls: Array<{ url: string; method: string }> = []
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    const method = init?.method ?? 'GET'
    calls.push({ url, method })
    if (url.includes('/auth/login')) {
      return { ok: true, status: 200, headers: new Headers({ 'content-type': 'application/json' }), json: async () => ({}) }
    }
    if (url.includes('/mail/')) {
      return {
        ok: true,
        status: 200,
        headers: new Headers({ 'content-type': 'application/json' }),
        json: async () => ({ emails: [{ id: '1', from: 'a@b.c', fromName: 'A', to: ['me@x.y'], subject: 'S', body: 'B', preview: 'P', date: '2026-01-01', read: false, starred: false, folder: 'inbox', hasAttachments: false, size: 1 }] }),
      }
    }
    return { ok: true, status: 200, headers: new Headers({ 'content-type': 'application/json' }), json: async () => ({}) }
  }) as unknown as typeof fetch)

  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  try {
    await act(async () =>
      root.render(
        createElement(
          MemoryRouter,
          { initialEntries: ['/login'] },
          createElement(
            ThemeProvider,
            null,
            createElement(
              AuthProvider,
              null,
              createElement(
                EmailProvider,
                null,
                createElement(Probe),
              ),
            ),
          ),
        ),
      ),
    )
    await act(async () => { await new Promise((r) => setImmediate(r)) })

    // FACET A (contract): no authenticated call while unauthenticated.
    const mailCalls = calls.filter((c) => c.url.includes('/mail/'))
    console.log(
      'EXPECTED: no /mail/ requests while unauthenticated | ACTUAL:',
      mailCalls.length,
      JSON.stringify(mailCalls.map((c) => c.url)),
    )
    if (mailCalls.length > 0) {
      console.log('PROBLEM CONFIRMED: unauthenticated mount fires authenticated API calls (401 -> /login reload loop)')
      expect(mailCalls.length).toBe(0)
    }

    // FACET B (contract): logging in must trigger the mail fetch.
    const mailBeforeLogin = calls.filter((c) => c.url.includes('/mail/')).length
    await act(async () => {
      await current.login('demo@localhost', 'demo1234')
    })
    await act(async () => { await new Promise((r) => setImmediate(r)) })
    const mailDelta = calls.filter((c) => c.url.includes('/mail/')).length - mailBeforeLogin
    console.log(
      'EXPECTED: /mail/ fetched after successful login (badge population) | ACTUAL delta:',
      mailDelta,
    )
    if (mailDelta === 0) {
      console.log('PROBLEM CONFIRMED: no mail fetch after login — unread badge stays 0')
      expect(mailDelta).toBeGreaterThan(0)
    }

    console.log('PROBLEM NOT REPRODUCED')
    console.log('FIX VERIFIED')
  } finally {
    await act(async () => root.unmount())
    container.remove()
    vi.unstubAllGlobals()
  }
})
