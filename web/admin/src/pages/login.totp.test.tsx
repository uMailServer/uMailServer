import { it, expect, vi } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { Login } from './Login'

function setInputValue(input: HTMLInputElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
  if (!setter) throw new Error('setter unavailable')
  setter.call(input, value)
  input.dispatchEvent(new Event('input', { bubbles: true }))
}

// F6101: a 2FA-enabled admin must be able to supply totp_code at login.
it('login reveals a TOTP field on "TOTP code required" and resubmits totp_code', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const fetchMock = vi
    .fn()
    .mockResolvedValueOnce({ ok: false, status: 401, json: async () => ({ error: 'TOTP code required' }) })
    .mockResolvedValueOnce({ ok: true, status: 200, json: async () => ({ expiresIn: 60 }) })
  vi.stubGlobal('fetch', fetchMock)
  const onLogin = vi.fn()
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  try {
    await act(async () => root.render(createElement(MemoryRouter, null, createElement(Login, { onLogin }))))
    const submit = () => container.querySelector('button[type="submit"]') as HTMLButtonElement
    await act(async () => {
      setInputValue(container.querySelector('#email') as HTMLInputElement, 'a@example.com')
      setInputValue(container.querySelector('#password') as HTMLInputElement, 'pw')
    })
    await act(async () => submit().click())
    await act(async () => {})
    const totp = container.querySelector('#totp') as HTMLInputElement | null
    expect(totp, 'PROBLEM CONFIRMED: no TOTP input after server demands a code').not.toBeNull()
    expect(onLogin).not.toHaveBeenCalled()
    await act(async () => setInputValue(totp!, '123456'))
    await act(async () => submit().click())
    await act(async () => {})
    const body = JSON.parse(fetchMock.mock.calls[1][1].body)
    expect(body.totp_code).toBe('123456')
    expect(onLogin).toHaveBeenCalled()
  } finally {
    await act(async () => root.unmount())
    container.remove()
    vi.unstubAllGlobals()
  }
})
