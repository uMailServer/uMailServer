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

const click = async (el: Element | undefined) => { await act(async () => { el?.dispatchEvent(new MouseEvent('click', { bubbles: true })) }) }
const byText = (c: HTMLElement, re: RegExp) => Array.from(c.querySelectorAll('button')).find((b) => re.test(b.textContent ?? ''))

it('Security tab rotates the signing key only after confirmation', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('PointerEvent', MouseEvent)
  let kid = 'k1'
  const f = vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
    if (url.endsWith('/admin/jwt/rotate') && init?.method === 'POST') { kid = 'k2'; return { ok: true, status: 200, json: async () => ({ status: 'rotated' }) } }
    if (url.endsWith('/admin/jwt/status')) return { ok: true, status: 200, json: async () => ({ currentKid: kid, activeKeys: 2 }) }
    return { ok: true, status: 200, json: async () => settings }
  })
  vi.stubGlobal('fetch', f)
  const c = document.createElement('div'); document.body.append(c)
  const root = createRoot(c)
  await act(async () => root.render(createElement(SettingsPage)))
  try {
    await click(Array.from(c.querySelectorAll('[role="tab"]')).find((t) => /security/i.test(t.textContent ?? '')))
    expect(c.textContent).toContain('k1')
    await click(byText(c, /rotate signing key/i))
    const posts = () => f.mock.calls.filter((x) => x[1]?.method === 'POST')
    expect(posts()).toHaveLength(0)
    await click(byText(c, /confirm rotate/i))
    expect(posts()).toHaveLength(1)
    expect(c.textContent).toContain('Signing key rotated')
    expect(c.textContent).toContain('k2')
  } finally { await act(async () => root.unmount()); c.remove(); vi.unstubAllGlobals() }
})
