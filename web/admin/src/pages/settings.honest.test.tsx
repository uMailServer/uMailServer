import { it, expect, vi } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { SettingsPage } from './Settings'

const settings = {
  config_file: '/etc/umailserver/umailserver.yaml',
  hostname: 'mx.example.org',
  data_dir: '/var/lib/umailserver',
  listeners: [{ name: 'SMTP (MX)', enabled: true, bind: '0.0.0.0', port: 25 }],
  max_message_size: 52428800,
  max_recipients: 100,
  tls: { min_version: '1.3', acme: true, manual_cert: false, client_auth: false },
  security: {
    max_login_attempts: 5, lockout_duration: '15m0s', submission_require_tls: true,
    ip_per_hour: 500, user_per_hour: 1000, http_requests_per_minute: 120, audit_log: true,
  },
  spam: {
    enabled: true, reject_threshold: 9, junk_threshold: 3, greylisting: true, bayesian: true,
    auto_train: true, rbl_servers: ['zen.spamhaus.org'], antivirus: false, dmarc_reports: false,
  },
  notifications: { alerts: false, alert_email: false, alert_webhook: false, push: true },
}

async function render(fetchImpl: ReturnType<typeof vi.fn>) {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('fetch', fetchImpl)
  const c = document.createElement('div'); document.body.append(c)
  const root = createRoot(c)
  await act(async () => root.render(createElement(SettingsPage)))
  return { c, cleanup: async () => { await act(async () => root.unmount()); c.remove(); vi.unstubAllGlobals() } }
}

it('Settings shows the running configuration and offers no save', async () => {
  const f = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => settings })
  const { c, cleanup } = await render(f)
  try {
    expect(f.mock.calls[0][0]).toBe('/api/v1/admin/settings')
    expect(c.textContent).toContain('mx.example.org')
    expect(c.textContent).toContain('/etc/umailserver/umailserver.yaml')
    expect(c.textContent).toContain('0.0.0.0:25')
    expect(c.textContent).toContain('50 MB')
    expect(c.textContent).toMatch(/read-only/i)
    expect(Array.from(c.querySelectorAll('button')).some((b) => /save/i.test(b.textContent ?? ''))).toBe(false)
    expect(f.mock.calls.every((call) => !call[1]?.method || call[1].method === 'GET')).toBe(true)
  } finally { await cleanup() }
})

it('Settings surfaces a load failure instead of placeholder values', async () => {
  const f = vi.fn().mockResolvedValue({
    ok: false, status: 503, headers: new Headers(), json: async () => ({ error: 'settings not available' }),
  })
  const { c, cleanup } = await render(f)
  try {
    expect(c.textContent).toContain('Could not load settings: settings not available')
    expect(c.textContent).not.toContain('mail.example.com')
  } finally { await cleanup() }
})
