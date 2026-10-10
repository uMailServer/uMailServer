import { describe, it, expect, vi, beforeEach } from 'vitest'
import API from './api'

const calls = () => (fetch as never as ReturnType<typeof vi.fn>).mock.calls as [string, RequestInit][]

describe('F6225 push/vacation/filters/threads client matches internal/api shapes', () => {
  beforeEach(() => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, headers: { get: () => 'application/json' }, json: async () => ({}) }) as never
  })

  it('subscribePush sends flat p256dh/auth fields', async () => {
    await API.subscribePush({ endpoint: 'https://push/e', keys: { p256dh: 'P', auth: 'A' } })
    const [url, init] = calls()[0]
    expect(url).toMatch(/\/push\/subscribe$/)
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body as string)).toEqual({ endpoint: 'https://push/e', p256dh: 'P', auth: 'A' })
  })

  it('unsubscribePush sends the endpoint in a DELETE body (server ignores ?endpoint=)', async () => {
    await API.unsubscribePush('https://push/e?x=1')
    const [url, init] = calls()[0]
    expect(url).toMatch(/\/push\/unsubscribe$/)
    expect(init.method).toBe('DELETE')
    expect(JSON.parse(init.body as string)).toEqual({ endpoint: 'https://push/e?x=1' })
  })

  it('setVacation uses PUT (POST is 405)', async () => {
    await API.setVacation({ enabled: true, subject: 's', message: 'm', send_interval: 24 })
    expect(calls()[0][1].method).toBe('PUT')
  })
})

describe('F6226 trash uses the encoded deleteMail helper', () => {
  it('trash page source no longer interpolates raw ids into the query', async () => {
    const fs = await import('node:fs')
    const src = fs.readFileSync(`${process.cwd()}/src/pages/trash.tsx`, 'utf8')
    expect(src).not.toMatch(/mail\/delete\?id=\$\{/)
  })
})

describe('F6228 invalid dates render empty instead of "Invalid Date"', () => {
  it('formatDate / formatFullDate', async () => {
    const { formatDate, formatFullDate } = await import('./date')
    expect(formatDate('not a date')).toBe('')
    expect(formatFullDate('')).toBe('')
  })
})
