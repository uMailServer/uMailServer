import { it, expect, vi } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { SettingsPage } from './Settings'

const settings = {
  config_file: 'x.yaml', hostname: 'mx', data_dir: '/d', listeners: [],
  max_message_size: 1024, max_recipients: 1,
  tls: { min_version: '1.3', acme: false, manual_cert: false, client_auth: false },
  security: { max_login_attempts: 5, lockout_duration: '1m', submission_require_tls: true, ip_per_hour: 1, user_per_hour: 1, http_requests_per_minute: 1, audit_log: true },
  spam: { enabled: true, reject_threshold: 9, junk_threshold: 3, greylisting: true, bayesian: true, auto_train: true, rbl_servers: null, antivirus: false, dmarc_reports: false },
  notifications: { alerts: false, alert_email: false, alert_webhook: false, push: false },
}
const limits = {
  ip_per_minute: 10, ip_per_hour: 100, ip_per_day: 1000, ip_connections: 5,
  user_per_minute: 20, user_per_hour: 200, user_per_day: 2000, user_max_recipients: 50,
  global_per_minute: 300, global_per_hour: 3000,
}

const click = async (el: Element | undefined) => { await act(async () => { el?.dispatchEvent(new MouseEvent('click', { bubbles: true })) }) }

it('Security tab edits live rate limits and PUTs the full config', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('PointerEvent', MouseEvent)
  const f = vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
    if (url.endsWith('/admin/ratelimits/config')) {
      if (init?.method === 'PUT') return { ok: true, status: 200, json: async () => ({ status: 'config_updated' }) }
      return { ok: true, status: 200, json: async () => limits }
    }
    if (url.endsWith('/admin/jwt/status')) return { ok: true, status: 200, json: async () => ({}) }
    return { ok: true, status: 200, json: async () => settings }
  })
  vi.stubGlobal('fetch', f)
  const c = document.createElement('div'); document.body.append(c)
  const root = createRoot(c)
  await act(async () => root.render(createElement(SettingsPage)))
  try {
    await click(Array.from(c.querySelectorAll('[role="tab"]')).find((t) => /security/i.test(t.textContent ?? '')))
    const input = c.querySelector('input[aria-label="Inbound messages per IP per hour"]') as HTMLInputElement
    expect(input.value).toBe('100')
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!
    await act(async () => { setter.call(input, '250'); input.dispatchEvent(new Event('input', { bubbles: true })) })
    expect(c.textContent).toMatch(/not written to the config file/i)
    await click(Array.from(c.querySelectorAll('button')).find((b) => /apply limits/i.test(b.textContent ?? '')))
    const put = f.mock.calls.find((x) => x[1]?.method === 'PUT')!
    const body = JSON.parse(put[1].body)
    expect(body.ip_per_hour).toBe(250)
    expect(body.global_per_hour).toBe(3000)
    expect(Object.keys(body)).toHaveLength(10)
    expect(c.textContent).toContain('Rate limits updated')
  } finally { await act(async () => root.unmount()); c.remove(); vi.unstubAllGlobals() }
})

it('Security tab shows a rate limit update failure', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('PointerEvent', MouseEvent)
  const f = vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
    if (url.endsWith('/admin/ratelimits/config')) {
      if (init?.method === 'PUT') return { ok: false, status: 400, headers: new Headers(), json: async () => ({ error: 'ip_per_hour must be at least 1' }) }
      return { ok: true, status: 200, json: async () => limits }
    }
    if (url.endsWith('/admin/jwt/status')) return { ok: true, status: 200, json: async () => ({}) }
    return { ok: true, status: 200, json: async () => settings }
  })
  vi.stubGlobal('fetch', f)
  const c = document.createElement('div'); document.body.append(c)
  const root = createRoot(c)
  await act(async () => root.render(createElement(SettingsPage)))
  try {
    await click(Array.from(c.querySelectorAll('[role="tab"]')).find((t) => /security/i.test(t.textContent ?? '')))
    await click(Array.from(c.querySelectorAll('button')).find((b) => /apply limits/i.test(b.textContent ?? '')))
    expect(c.textContent).toContain('Update failed: ip_per_hour must be at least 1')
    expect(c.textContent).not.toContain('Rate limits updated')
  } finally { await act(async () => root.unmount()); c.remove(); vi.unstubAllGlobals() }
})
