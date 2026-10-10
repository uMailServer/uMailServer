import { describe, it, expect, vi, beforeEach } from 'vitest'
import API from './api'

const calls = () => (fetch as never as ReturnType<typeof vi.fn>).mock.calls as [string, RequestInit][]

describe('F6311 moveMail uses POST /mail/move', () => {
  beforeEach(() => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, headers: { get: () => 'application/json' }, json: async () => ({ status: 'moved' }) }) as never
  })

  it('single id sends {id, from, to}', async () => {
    await API.moveMail('m1', 'trash', 'inbox')
    const [url, init] = calls()[0]
    expect(url).toMatch(/\/mail\/move$/)
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body as string)).toEqual({ id: 'm1', from: 'trash', to: 'inbox' })
  })

  it('multiple ids send {ids, from, to}', async () => {
    await API.moveMail(['a', 'b'], 'trash', 'inbox')
    expect(JSON.parse(calls()[0][1].body as string)).toEqual({ ids: ['a', 'b'], from: 'trash', to: 'inbox' })
  })

  it('rejects on server error', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: false, status: 500, statusText: 'x', headers: { get: () => 'application/json' }, json: async () => ({ error: 'boom' }) }) as never
    await expect(API.moveMail('m1', 'trash', 'inbox')).rejects.toBeTruthy()
  })

  it('trash Restore no longer permanently deletes; login demo text is DEV-gated', async () => {
    const fs = await import('node:fs')
    const trash = fs.readFileSync(`${process.cwd()}/src/pages/trash.tsx`, 'utf8')
    const restore = trash.slice(trash.indexOf('const handleRestore '), trash.indexOf('const handleRestoreSelected'))
    expect(restore).toContain('moveMail')
    expect(restore).not.toContain('deleteMail')
    const login = fs.readFileSync(`${process.cwd()}/src/pages/login.tsx`, 'utf8')
    expect(login).toMatch(/import\.meta\.env\.DEV[\s\S]*Demo accounts/)
  })
})
