import { it, expect, vi } from 'vitest'
import { act, createElement, useEffect } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { ThemeProvider } from '@/components/theme-provider'
import { AuthProvider, useAuth } from '@/contexts/AuthContext'
import { EmailProvider } from '@/contexts/EmailContext'
import { Layout } from './layout'
import { Sidebar } from './sidebar'
import api from '@/utils/api'

// The mail load is auth-gated (F-email-auth): log in so loadEmails runs,
// exactly as in production, before asserting the badge.
function LoginProbe() {
  const { login } = useAuth()
  useEffect(() => {
    void login('demo@localhost', 'demo1234')
  }, [login])
  return null
}

const fixtures = [
  { id: '1', folder: 'inbox', read: true, subject: 'read one' },
  { id: '2', folder: 'inbox', read: false, subject: 'unread one' },
  { id: '3', folder: 'inbox', read: false, subject: 'unread two' },
]

function stubEnv() {
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
}

function inboxBadgeValue(root: HTMLElement): string | null {
  const inboxLink = Array.from(root.querySelectorAll('a')).find((a) =>
    (a.textContent ?? '').startsWith('Inbox')
  )
  if (!inboxLink) return null
  // The unread badge is the link's last element child in both variants.
  return inboxLink.lastElementChild?.textContent ?? null
}

it('F-units sidebar renders the real unread count, not a hardcoded 12', async () => {
  stubEnv()
  // Unit level: the Sidebar must honor its unreadCount prop.
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  try {
    await act(async () =>
      root.render(
        createElement(
          MemoryRouter,
          {},
          createElement(Sidebar, { collapsed: false, onToggle: () => {}, unreadCount: 2 })
        )
      )
    )
    const badge = inboxBadgeValue(container)
    console.log(
      'EXPECTED: Inbox badge shows the unreadCount prop (2) | ACTUAL:',
      JSON.stringify(badge)
    )
    expect(badge, 'CONTROL FAILED: Inbox badge missing').not.toBeNull()
    if (badge !== '2') {
      console.log('PROBLEM CONFIRMED')
      expect(
        badge,
        'PROBLEM CONFIRMED: Sidebar ignores its unreadCount prop and renders a hardcoded 12'
      ).toBe('2')
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

it('F-units the layout inbox badge reflects the real unread count', async () => {
  stubEnv()
  vi.spyOn(api, 'get').mockImplementation((endpoint: string) => {
    if (endpoint === '/mail/inbox') {
      return Promise.resolve({ emails: fixtures }) as never
    }
    return Promise.resolve({}) as never
  })
  vi.spyOn(api, 'post').mockImplementation(() =>
    Promise.resolve({
      ok: true,
      status: 200,
      headers: { get: () => 'application/json' },
      json: async () => ({}),
    }) as never
  )

  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  try {
    await act(async () =>
      root.render(
        createElement(ThemeProvider, {
          defaultTheme: 'system',
          storageKey: 'webmail-theme',
          children: createElement(AuthProvider, {
            children: [
              createElement(LoginProbe),
              createElement(EmailProvider, {
                children: createElement(
                  MemoryRouter,
                  { initialEntries: ['/'] },
                  createElement(Layout)
                ),
              }),
            ],
          }),
        })
      )
    )
    // Login settles, the auth-gated loadEmails resolves, then the badge
    // reflects the count — the production sequence.
    await flush()
    await flush()
    await flush()

    const badge = inboxBadgeValue(container)
    console.log(
      'EXPECTED: layout Inbox badge shows the real unread count (2) | ACTUAL:',
      JSON.stringify(badge)
    )
    expect(badge, 'CONTROL FAILED: Inbox badge missing in layout').not.toBeNull()
    if (badge !== '2') {
      console.log('PROBLEM CONFIRMED')
      expect(
        badge,
        'PROBLEM CONFIRMED: the layout Inbox badge shows a hardcoded 12 instead of the real unread count'
      ).toBe('2')
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

async function flush() {
  await new Promise((resolve) => setImmediate(resolve))
}
