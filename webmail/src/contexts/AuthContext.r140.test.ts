import { it, expect, vi } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { AuthProvider, useAuth } from './AuthContext'
import api from '../utils/api'
import { ApiError } from '../utils/api'

// F6227: the webmail could not sign in TOTP-enabled accounts (no code field,
// totp_code never sent) and mapped every failure to "Invalid email or password".
it('F6227 TOTP-required login asks for a code, then sends totp_code', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const bodies: unknown[] = []
  vi.spyOn(api, 'post').mockImplementation((_e: string, body?: unknown) => {
    bodies.push(body)
    if (bodies.length === 1) return Promise.reject(new ApiError(401, 'TOTP code required')) as never
    return Promise.resolve({}) as never
  })
  let current!: ReturnType<typeof useAuth>
  function Probe() { current = useAuth(); return null }
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  try {
    await act(async () => root.render(createElement(AuthProvider, { children: createElement(Probe) })))
    let ok = true
    await act(async () => { ok = await current.login('u@x.com', 'pw') })
    expect(ok).toBe(false)
    expect(current.requiresTotp).toBe(true)
    expect(current.error).toMatch(/authenticator/i)
    await act(async () => { ok = await current.login('u@x.com', 'pw', '123456') })
    expect(ok).toBe(true)
    expect(bodies[1]).toEqual({ email: 'u@x.com', password: 'pw', totp_code: '123456' })
    expect(current.requiresTotp).toBe(false)
  } finally {
    await act(async () => root.unmount()); container.remove(); vi.restoreAllMocks(); vi.unstubAllGlobals()
  }
})

it('F6227 rate limit and bad credentials get distinct messages', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const spy = vi.spyOn(api, 'post')
  let current!: ReturnType<typeof useAuth>
  function Probe() { current = useAuth(); return null }
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  try {
    await act(async () => root.render(createElement(AuthProvider, { children: createElement(Probe) })))
    spy.mockRejectedValueOnce(new ApiError(429, 'too many login attempts'))
    await act(async () => { await current.login('u@x.com', 'pw') })
    expect(current.error).toBe('too many login attempts')
    spy.mockRejectedValueOnce(new ApiError(401, 'invalid credentials'))
    await act(async () => { await current.login('u@x.com', 'pw') })
    expect(current.error).toBe('Invalid email or password')
    expect(current.requiresTotp).toBe(false)
  } finally {
    await act(async () => root.unmount()); container.remove(); vi.restoreAllMocks(); vi.unstubAllGlobals()
  }
})
