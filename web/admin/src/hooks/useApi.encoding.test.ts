import { it, expect, vi } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { useAccounts } from './useApi'

// F6105: path/query parameters must be URL-encoded.
it('account paths and domain filter are encoded', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => [] })
  vi.stubGlobal('fetch', fetchMock)
  let cur!: ReturnType<typeof useAccounts>
  function Probe() {
    cur = useAccounts()
    return null
  }
  const c = document.createElement('div')
  const root = createRoot(c)
  try {
    await act(async () => root.render(createElement(Probe)))
    await act(async () => { await cur.fetchAccounts('a.com&x=1') })
    await act(async () => { await cur.deleteAccount('a/b?c#d@x.com') })
    const urls = fetchMock.mock.calls.map((x) => x[0])
    expect(urls[0]).toBe('/api/v1/accounts?domain=a.com%26x%3D1')
    expect(urls[1]).toBe('/api/v1/accounts/a%2Fb%3Fc%23d%40x.com')
  } finally {
    await act(async () => root.unmount())
    vi.unstubAllGlobals()
  }
})
