import { it, expect, vi } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { ThemeProvider } from '@/components/theme-provider'
import { AuthProvider } from '@/contexts/AuthContext'
import { Header } from './header'
import api from '@/utils/api'

it('F-signout the header Sign Out item revokes the server session', async () => {
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
  const postCalls: { endpoint: string }[] = []
  vi.spyOn(api, 'post').mockImplementation((endpoint: string) => {
    postCalls.push({ endpoint })
    return Promise.resolve({}) as never
  })

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
            children: createElement(
              MemoryRouter,
              { initialEntries: ['/inbox'] },
              createElement(Header, { onMenuToggle: () => {}, sidebarCollapsed: false })
            ),
          }),
        })
      )
    )

    // Control: the header rendered with its profile trigger.
    const triggers = Array.from(container.querySelectorAll('button'))
    expect(triggers.length, 'CONTROL FAILED: header buttons missing').toBeGreaterThan(0)
    console.log('CONTROL EXPECTED: header rendered | ACTUAL: shown')

    // Open the user profile dropdown (the header's last button = avatar trigger).
    const buttons = Array.from(container.querySelectorAll('button'))
    const avatarTrigger = buttons[buttons.length - 1]
    if (!avatarTrigger || !avatarTrigger.querySelector('span.rounded-full')) {
      throw new Error('CONTROL FAILED: profile dropdown trigger not found')
    }
    // Radix DropdownMenu triggers open on pointerdown (jsdom never fires it
    // from .click()); dispatch both with left-button, no ctrl — the exact
    // modifiers the trigger accepts.
    await act(async () => {
      for (const type of ['pointerdown', 'click']) {
        avatarTrigger.dispatchEvent(
          new MouseEvent(type, { bubbles: true, cancelable: true, button: 0 })
        )
      }
    })
    await flush()
    await flush()
    const signOut = Array.from(document.querySelectorAll('[role="menuitem"]')).find((item) =>
      item.textContent?.includes('Sign Out')
    )
    if (!signOut) {
      throw new Error('CONTROL FAILED: Sign Out menu item not rendered')
    }
    console.log('CONTROL EXPECTED: Sign Out menu item present | ACTUAL: shown')

    await act(async () => (signOut as HTMLElement).click())
    await flush()

    // CONTRACT: signing out must revoke the server session via POST
    // /api/v1/auth/logout (wired through AuthContext.logout, fixed in the
    // previous round).
    const logoutRequested = postCalls.some((c) => c.endpoint === '/auth/logout')
    console.log(
      'EXPECTED: POST /auth/logout issued on Sign Out | ACTUAL:',
      logoutRequested ? 'issued' : 'not issued',
      '| post endpoints:',
      JSON.stringify(postCalls.map((c) => c.endpoint))
    )
    if (!logoutRequested) {
      console.log('PROBLEM CONFIRMED')
      expect(
        logoutRequested,
        'PROBLEM CONFIRMED: Sign Out is not wired — clicking it issues no request and no logout happens'
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

async function flush() {
  await new Promise((resolve) => setImmediate(resolve))
}
