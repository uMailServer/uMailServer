import { it, expect, vi } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { AuthProvider, useAuth } from './AuthContext'
import api from '../utils/api'

it('F-logout logout() revokes the server session, not just local state', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const postCalls: { endpoint: string }[] = []
  vi.spyOn(api, 'post').mockImplementation((endpoint: string) => {
    postCalls.push({ endpoint })
    if (endpoint === '/auth/login') return Promise.resolve({}) as never
    if (endpoint === '/auth/logout') return Promise.resolve(undefined) as never
    return Promise.reject(new Error('unexpected endpoint: ' + endpoint)) as never
  })

  let current!: ReturnType<typeof useAuth>
  function Probe() {
    current = useAuth()
    return null
  }
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  try {
    await act(async () =>
      root.render(createElement(AuthProvider, { children: createElement(Probe) }))
    )

    // Control: login works and issues exactly one POST to /auth/login.
    let loginOk = false
    await act(async () => {
      loginOk = await current.login('user@example.com', 'pw')
    })
    expect(loginOk, 'CONTROL FAILED: login did not succeed').toBe(true)
    expect(current.isAuthenticated, 'CONTROL FAILED: not authenticated after login').toBe(true)
    expect(
      postCalls.filter((c) => c.endpoint === '/auth/login').length,
      'CONTROL FAILED: login issued no POST'
    ).toBe(1)
    console.log('CONTROL EXPECTED: authenticated after one login POST | ACTUAL: shown')

    // Logout: local state must clear AND the server session must be revoked
    // via POST /api/v1/auth/logout (handleLogout blacklists the token and
    // clears the HttpOnly cookie — server.go:417, server_auth.go:533).
    postCalls.length = 0
    await act(async () => {
      current.logout()
    })
    const stateCleared =
      !current.isAuthenticated && current.user === null && !current.loading
    const logoutRequested = postCalls.some((c) => c.endpoint === '/auth/logout')
    console.log(
      'EXPECTED: state cleared + POST /auth/logout issued | ACTUAL: state cleared:',
      stateCleared,
      '| logout requested:',
      logoutRequested
    )
    expect(stateCleared, 'CONTROL FAILED: local state not cleared by logout').toBe(true)
    if (!logoutRequested) {
      console.log('PROBLEM CONFIRMED')
      expect(
        logoutRequested,
        'PROBLEM CONFIRMED: logout() issues no request — the HttpOnly session cookie stays valid after logout'
      ).toBe(true)
    }
    console.log('PROBLEM NOT REPRODUCED')

    // Secondary branch: a failing logout request must not break the local
    // logout or produce an unhandled rejection.
    postCalls.length = 0
    vi.spyOn(api, 'post').mockImplementation((endpoint: string) => {
      postCalls.push({ endpoint })
      if (endpoint === '/auth/login') return Promise.resolve({}) as never
      return Promise.reject(new Error('network down')) as never
    })
    await act(async () => {
      loginOk = await current.login('user@example.com', 'pw')
    })
    expect(loginOk, 'CONTROL FAILED: re-login failed').toBe(true)
    await act(async () => {
      current.logout()
    })
    expect(
      current.isAuthenticated,
      'CONTROL FAILED: local logout must not depend on the request succeeding'
    ).toBe(false)
    await new Promise((resolve) => setImmediate(resolve))
    console.log('FIX VERIFIED: failing logout request is swallowed, local state still cleared')
  } finally {
    await act(async () => root.unmount())
    container.remove()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  }
})
