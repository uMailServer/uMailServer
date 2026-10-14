import { it, expect, vi } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { useDomains, useAccounts, useQueue } from '@/hooks/useApi'

const ok = (status: number, body: unknown = {}) => ({ ok: true, status, json: async () => body })
const bad = { ok: false, status: 500, json: async () => ({ error: 'refresh down' }) }

it('R285 F7590 mutation success survives a failing list refresh', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  let d!: ReturnType<typeof useDomains>, a!: ReturnType<typeof useAccounts>, q!: ReturnType<typeof useQueue>
  const container = document.createElement('div'); document.body.append(container)
  const root = createRoot(container)
  const fetchMock = vi.fn(); vi.stubGlobal('fetch', fetchMock)
  const outcome = async (fn: () => Promise<unknown>) => { try { await fn(); return 'resolved' } catch (e) { return 'rejected: ' + (e as Error).message } }
  try {
    await act(async () => root.render(createElement(function P() { d = useDomains(); a = useAccounts(); q = useQueue(); return null })))
    const cases: Array<[string, () => Promise<unknown>, number]> = [
      ['deleteDomain', () => d.deleteDomain('x.com'), 204], ['createDomain', () => d.createDomain('x.com', 1), 201],
      ['updateDomain', () => d.updateDomain('x.com', { is_active: false }), 200],
      ['createAccount', () => a.createAccount('u@x.com', 'Passw0rd!x'), 201], ['updateAccount', () => a.updateAccount('u@x.com', {}), 200],
      ['deleteAccount', () => a.deleteAccount('u@x.com'), 204], ['retryEntry', () => q.retryEntry('i'), 200], ['dropEntry', () => q.dropEntry('i'), 204],
    ]
    for (const [name, fn, st] of cases) {
      fetchMock.mockResolvedValueOnce(ok(st)).mockResolvedValueOnce(bad)
      expect(await act(async () => outcome(fn)), name).toBe('resolved')
    }
    // edge: refresh error is still surfaced via hook error state
    expect(d.error?.message).toBe('refresh down')
    expect(q.error?.message).toBe('refresh down')
    // edge: mutation failure still rejects, no refresh attempted
    fetchMock.mockClear(); fetchMock.mockResolvedValueOnce({ ok: false, status: 409, json: async () => ({ error: 'exists' }) })
    expect(await act(async () => outcome(() => a.createAccount('u@x.com', 'Passw0rd!x')))).toBe('rejected: exists')
    expect(fetchMock).toHaveBeenCalledTimes(1)
    // edge: refresh success after mutation populates list
    fetchMock.mockResolvedValueOnce(ok(204)).mockResolvedValueOnce(ok(200, [{ name: 'y.com' }]))
    await act(async () => { await d.deleteDomain('x.com') })
    expect(d.domains).toEqual([{ name: 'y.com' }])
    console.log('FIX VERIFIED')
  } finally { await act(async () => root.unmount()); container.remove(); vi.unstubAllGlobals() }
})
