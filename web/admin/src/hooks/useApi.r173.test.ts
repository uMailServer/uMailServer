import { it, expect, vi } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { useDomains, useAccounts, useQueue } from './useApi'

function mount(fn: () => void) {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  return { root, container, render: () => act(async () => root.render(createElement(function P() { fn(); return null }))) }
}

it('R173 list hooks page past the server default limit of 100', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const urls: string[] = []
  const rows = (n: number) => Array.from({ length: n }, (_, i) => ({ name: `d${i}` }))
  vi.stubGlobal('fetch', vi.fn().mockImplementation(async (u: string) => {
    urls.push(u)
    const off = Number(new URL(u, 'http://x').searchParams.get('offset') ?? 0)
    return { ok: true, status: 200, json: async () => (off === 0 ? rows(1000) : rows(5)) }
  }))
  let d!: ReturnType<typeof useDomains>, a!: ReturnType<typeof useAccounts>, q!: ReturnType<typeof useQueue>
  const m = mount(() => { d = useDomains(); a = useAccounts(); q = useQueue() })
  try {
    await m.render()
    await act(async () => { await d.fetchDomains(); await a.fetchAccounts('x.com'); await q.fetchQueue() })
    expect(d.domains).toHaveLength(1005)
    expect(a.accounts).toHaveLength(1005)
    expect(q.entries).toHaveLength(1005)
    expect(urls.every((u) => u.includes('limit=1000'))).toBe(true)
    expect(urls.some((u) => u.includes('domain=x.com'))).toBe(true)
  } finally {
    await act(async () => m.root.unmount()); m.container.remove(); vi.unstubAllGlobals()
  }
})

it('R173 401 from API fires unauthorized event', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 401, json: async () => ({ error: 'x' }) }))
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const h = vi.fn()
  window.addEventListener('umail:unauthorized', h)
  let d!: ReturnType<typeof useDomains>
  const m = mount(() => { d = useDomains() })
  try {
    await m.render()
    await act(async () => { await d.fetchDomains().catch(() => undefined) })
    expect(h).toHaveBeenCalled()
  } finally {
    window.removeEventListener('umail:unauthorized', h)
    await act(async () => m.root.unmount()); m.container.remove(); vi.unstubAllGlobals()
  }
})

it('R173 stale list response does not overwrite newer one', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const pend: Array<(v: unknown) => void> = []
  vi.stubGlobal('fetch', vi.fn().mockImplementation(() => new Promise((r) => pend.push(r))))
  let a!: ReturnType<typeof useAccounts>
  const m = mount(() => { a = useAccounts() })
  const resp = (email: string) => ({ ok: true, status: 200, json: async () => [{ email }] })
  try {
    await m.render()
    let p1!: Promise<unknown>, p2!: Promise<unknown>
    await act(async () => { p1 = a.fetchAccounts('a.com'); p2 = a.fetchAccounts('b.com') })
    await act(async () => { pend[1](resp('new@b.com')); await p2 })
    await act(async () => { pend[0](resp('old@a.com')); await p1 })
    expect(a.accounts?.[0].email).toBe('new@b.com')
  } finally {
    await act(async () => m.root.unmount()); m.container.remove(); vi.unstubAllGlobals()
  }
})
