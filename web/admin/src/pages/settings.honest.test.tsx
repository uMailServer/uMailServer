import { it, expect, vi } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { SettingsPage } from './Settings'

it('R173 Settings never claims a save that did not hit the API', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const f = vi.fn(); vi.stubGlobal('fetch', f)
  const c = document.createElement('div'); document.body.append(c)
  const root = createRoot(c)
  try {
    await act(async () => root.render(createElement(SettingsPage)))
    const btn = Array.from(c.querySelectorAll('button')).find((b) => /Save Changes/.test(b.textContent ?? ''))
    expect(btn).toBeTruthy()
    await act(async () => { btn!.click() })
    expect(c.textContent).not.toMatch(/saved successfully/i)
    expect(btn!.disabled).toBe(true)
    expect(c.textContent).toMatch(/read-only|not yet|preview/i)
  } finally { await act(async () => root.unmount()); c.remove(); vi.unstubAllGlobals() }
})
