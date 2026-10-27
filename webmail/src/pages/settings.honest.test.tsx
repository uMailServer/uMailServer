import { it, expect, vi, afterEach, beforeEach } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'

// The Settings page stored a dozen toggles nothing read ("Show online
// status", "Read receipts", "Desktop notifications", ...). It now offers only
// settings that act: push notifications and two compose preferences.

const push = vi.hoisted(() => ({
  state: 'off' as string,
  enablePush: vi.fn(),
  disablePush: vi.fn(),
}))
vi.mock('@/utils/push', () => ({
  getPushState: vi.fn(async () => push.state),
  enablePush: push.enablePush,
  disablePush: push.disablePush,
}))
const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }))
vi.mock('sonner', () => ({ toast }))

vi.mock('@/components/theme-provider', () => ({ useTheme: () => ({ theme: 'light', setTheme: () => {}, resolvedTheme: 'light' }) }))

import { SettingsPage } from './settings'

beforeEach(() => { localStorage.clear(); push.state = 'off'; push.enablePush.mockReset(); push.disablePush.mockReset(); toast.error.mockReset() })
afterEach(() => { vi.unstubAllGlobals(); document.body.innerHTML = '' })

async function mount() {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const c = document.createElement('div'); document.body.append(c)
  await act(async () => createRoot(c).render(createElement(MemoryRouter, null, createElement(SettingsPage))))
  await act(async () => { await Promise.resolve() })
  return c
}
const sw = (c: HTMLElement, label: string) => c.querySelector(`[aria-label="${label}"]`) as HTMLButtonElement

it('shows only settings that act, and links to the account portal', async () => {
  const c = await mount()
  for (const gone of ['Show online status', 'Read receipts', 'Rich text mode', 'Sound notifications', 'Desktop notifications']) {
    expect(c.textContent).not.toContain(gone)
  }
  expect(sw(c, 'Push notifications')).toBeTruthy()
  expect((Array.from(c.querySelectorAll('a')).find((a) => a.textContent === 'Manage Account') as HTMLAnchorElement).getAttribute('href')).toBe('/account/')
})

it('turns push notifications on and reports why it could not', async () => {
  const c = await mount()
  push.enablePush.mockImplementation(async () => { push.state = 'on' })
  await act(async () => { sw(c, 'Push notifications').click() })
  await act(async () => { await Promise.resolve() })
  expect(push.enablePush).toHaveBeenCalledOnce()
  expect(sw(c, 'Push notifications').getAttribute('aria-checked')).toBe('true')

  push.disablePush.mockRejectedValue(new Error('server said no'))
  await act(async () => { sw(c, 'Push notifications').click() })
  await act(async () => { await Promise.resolve() })
  expect(toast.error).toHaveBeenCalledWith('server said no')
})

it('disables the push switch where it cannot work', async () => {
  push.state = 'denied'
  const c = await mount()
  expect(sw(c, 'Push notifications').disabled).toBe(true)
  expect(c.textContent).toContain('blocked for this site')
})

it('saves compose preferences on this device', async () => {
  const c = await mount()
  await act(async () => { sw(c, 'Spell check').click() })
  expect(JSON.parse(localStorage.getItem('umail.webmail.settings')!)).toMatchObject({ spellCheck: false, autoSaveDraft: true })
})
