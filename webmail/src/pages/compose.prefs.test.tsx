import { it, expect, vi, afterEach, beforeEach } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { ComposePage } from './compose'
import API from '@/utils/api'

// The Settings toggles for spell check and draft autosave were stored but
// never read by the composer.

beforeEach(() => { sessionStorage.clear(); localStorage.clear() })
afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks(); vi.unstubAllGlobals(); document.body.innerHTML = '' })

async function mount() {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.spyOn(API, 'getContacts').mockResolvedValue([])
  const c = document.createElement('div'); document.body.append(c)
  await act(async () => createRoot(c).render(createElement(MemoryRouter, { initialEntries: ['/compose'] }, createElement(ComposePage))))
  return c
}
function type(el: HTMLTextAreaElement, value: string) {
  Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(el, value)
  el.dispatchEvent(new Event('input', { bubbles: true }))
}

it('honours the spell check preference', async () => {
  localStorage.setItem('umail.webmail.settings', JSON.stringify({ spellCheck: false }))
  const c = await mount()
  expect(c.querySelector('textarea')!.getAttribute('spellcheck')).toBe('false')
})

it('does not autosave when draft autosave is off, and does when it is on', async () => {
  for (const autoSaveDraft of [false, true]) {
    document.body.innerHTML = ''
    localStorage.setItem('umail.webmail.settings', JSON.stringify({ autoSaveDraft }))
    vi.useFakeTimers()
    const save = vi.spyOn(API, 'saveDraft').mockResolvedValue({ id: 'd', folder: 'Drafts', saved: true } as never)
    const c = await mount()
    await act(async () => type(c.querySelector('textarea')!, 'hello'))
    await act(async () => { vi.advanceTimersByTime(2500) })
    expect(save.mock.calls.length > 0).toBe(autoSaveDraft)
    vi.useRealTimers()
    vi.restoreAllMocks()
  }
})
