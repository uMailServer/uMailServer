import { it, expect, vi, afterEach } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { SearchPage } from './search'
import { ComposePage } from './compose'
import API from '@/utils/api'
import { useKeyboardShortcuts } from '@/hooks/useKeyboardShortcuts'

const { navigate } = vi.hoisted(() => ({ navigate: vi.fn() }))
vi.mock('react-router-dom', async (orig) => ({ ...(await orig<typeof import('react-router-dom')>()), useNavigate: () => navigate }))

afterEach(() => { vi.restoreAllMocks(); document.body.innerHTML = ''; navigate.mockClear(); localStorage.clear() })

function setValue(input: HTMLInputElement, v: string) {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, v)
  input.dispatchEvent(new Event('input', { bubbles: true }))
}
async function mount(el: React.ReactElement) {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const c = document.createElement('div'); document.body.append(c)
  await act(async () => createRoot(c).render(el))
  return c
}

it('F7110 shortcuts ignore Alt chords, select elements and IME composition', async () => {
  function Probe() { useKeyboardShortcuts(); return null }
  await mount(createElement(Probe))
  document.body.dispatchEvent(new KeyboardEvent('keydown', { key: '/', altKey: true, bubbles: true, cancelable: true }))
  expect(navigate).not.toHaveBeenCalled()
  const sel = document.createElement('select'); document.body.append(sel)
  sel.dispatchEvent(new KeyboardEvent('keydown', { key: '/', bubbles: true, cancelable: true }))
  expect(navigate).not.toHaveBeenCalled()
  document.body.dispatchEvent(new KeyboardEvent('keydown', { key: '/', isComposing: true, bubbles: true, cancelable: true }))
  expect(navigate).not.toHaveBeenCalled()
  document.body.dispatchEvent(new KeyboardEvent('keydown', { key: '/', bubbles: true, cancelable: true }))
  expect(navigate).toHaveBeenCalledWith('/search')
})

it('F7112 search error state does not also render the empty state', async () => {
  vi.spyOn(console, 'error').mockImplementation(() => {})
  vi.spyOn(API, 'search').mockRejectedValue(new Error('boom'))
  const c = await mount(createElement(MemoryRouter, null, createElement(SearchPage)))
  await act(async () => setValue(c.querySelector('input')!, 'abc'))
  await act(async () => { c.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })) })
  expect(c.textContent).toContain('Search failed')
  expect(c.textContent).not.toContain('No results found')
})

it('F7112 empty state names the searched term, not later typing', async () => {
  vi.spyOn(API, 'search').mockResolvedValue({ emails: [], total: 0 } as never)
  const c = await mount(createElement(MemoryRouter, null, createElement(SearchPage)))
  await act(async () => setValue(c.querySelector('input')!, 'abc'))
  await act(async () => { c.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })) })
  await act(async () => setValue(c.querySelector('input')!, 'abcdef'))
  expect(c.textContent).toContain('No results for "abc"')
})

it('F7113 clearing cancels in-flight search and resets loading', async () => {
  let resolve!: (v: never) => void
  vi.spyOn(API, 'search').mockReturnValue(new Promise((r) => { resolve = r as never }))
  const c = await mount(createElement(MemoryRouter, null, createElement(SearchPage)))
  await act(async () => setValue(c.querySelector('input')!, 'abc'))
  await act(async () => { c.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })) })
  await act(async () => { (c.querySelector('button[type=button]') as HTMLButtonElement).click() })
  await act(async () => resolve({ emails: [{ id: 'x', from: 'a', fromName: 'a', subject: 'S', body: '', date: '', folder: 'inbox', read: true }], total: 1 } as never))
  expect(localStorage.getItem('umail_recent_searches')).toBeNull()
  expect(c.textContent).toContain('Search your mail')
  expect((c.querySelector('button[type=submit]') as HTMLButtonElement).textContent).toBe('Search')
})

it('F7114/F7115 compose warns on unload and on discard for cc-only edits', async () => {
  const c = await mount(createElement(MemoryRouter, null, createElement(ComposePage)))
  const ev = () => { const e = new Event('beforeunload', { cancelable: true }); window.dispatchEvent(e); return e.defaultPrevented }
  expect(ev()).toBe(false)
  const ccInput = c.querySelector('input[aria-label=Cc]') as HTMLInputElement
  await act(async () => setValue(ccInput, 'a@b.co'))
  await act(async () => { ccInput.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true })) })
  expect(c.textContent).toContain('a@b.co')
  expect(ev()).toBe(true)
  const conf = vi.spyOn(window, 'confirm').mockReturnValue(false)
  await act(async () => { (c.querySelector('button[aria-label=Back]') as HTMLButtonElement).click() })
  expect(conf).toHaveBeenCalled()
  expect(navigate).not.toHaveBeenCalled()
})
