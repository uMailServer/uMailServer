import { describe, it, expect, vi, beforeEach } from 'vitest'
import API from './api'

describe('F6092 path/query encoding of ids', () => {
  beforeEach(() => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, headers: { get: () => 'application/json' }, json: async () => ({}) }) as never
  })
  it('encodes mail id', async () => {
    await API.deleteMail('a&folder=x#')
    expect((fetch as never as ReturnType<typeof vi.fn>).mock.calls[0][0]).toContain('id=a%26folder%3Dx%23')
  })
  it('encodes filter/thread/folder ids', async () => {
    await API.deleteFilter('../x?y')
    await API.getThread('a/b')
    await API.getMail('in/../x')
    const urls = (fetch as never as ReturnType<typeof vi.fn>).mock.calls.map((c: unknown[]) => c[0] as string)
    expect(urls[0]).toMatch(/filters\/\.\.%2Fx%3Fy$/)
    expect(urls[1]).toMatch(/threads\/a%2Fb$/)
    expect(urls[2]).toMatch(/mail\/in%2F\.\.%2Fx$/)
  })
})

describe('F6093 401 on login page must not reload-loop or swallow error', () => {
  it('throws instead of redirecting when already on /login', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: false, status: 401, headers: { get: () => null } }) as never
    window.history.pushState({}, '', '/login')
    await expect(API.login({ email: 'a@b.c', password: 'x' })).rejects.toThrow()
  })
})
