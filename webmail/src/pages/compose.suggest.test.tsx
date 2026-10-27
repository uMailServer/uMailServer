import { it, expect, vi, afterEach } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { ComposePage } from './compose'
import { suggestionsFor } from '@/utils/recipients'
import API, { ApiError, type Contact } from '@/utils/api'

// Recipient fields offered no suggestions; they now complete from the
// user's contacts.

const jane: Contact = { uid: 'u1', addressbook: 'contacts', name: 'Jane Doe', emails: ['jane@example.com', 'jd@work.example'], phones: null }
const bob: Contact = { uid: 'u2', addressbook: 'phone', name: '', emails: ['bob@other.example'], phones: null }

async function mount() {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  await act(async () => root.render(createElement(MemoryRouter, { initialEntries: ['/compose'] }, createElement(ComposePage))))
  return { container, root }
}
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); document.body.innerHTML = ''; sessionStorage.clear() })

function type(el: HTMLInputElement, value: string) {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(el, value)
  el.dispatchEvent(new Event('input', { bubbles: true }))
}
const key = (el: HTMLElement, k: string) => el.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true }))
const chips = (c: HTMLElement) => Array.from(c.querySelectorAll('button[aria-label^="Remove "]')).map((b) => b.getAttribute('aria-label')!.slice(7))

it('matches name or address, skipping addresses already added', () => {
  const all = [{ name: 'Jane Doe', email: 'jane@example.com' }, { name: '', email: 'bob@other.example' }]
  expect(suggestionsFor(all, 'DOE', []).map((s) => s.email)).toEqual(['jane@example.com'])
  expect(suggestionsFor(all, 'example', ['JANE@example.com']).map((s) => s.email)).toEqual(['bob@other.example'])
  expect(suggestionsFor(all, '  ', [])).toEqual([])
})

it('completes a recipient from contacts with the keyboard and the mouse', async () => {
  vi.spyOn(API, 'getContacts').mockResolvedValue([jane, bob])
  const { container, root } = await mount()
  const to = container.querySelector('input[aria-label="To"]') as HTMLInputElement

  await act(async () => type(to, 'jane'))
  const options = () => Array.from(container.querySelectorAll('[role="option"]')).map((o) => o.textContent)
  expect(options()).toEqual(['Jane Doe jane@example.com', 'Jane Doe jd@work.example'])
  await act(async () => { key(to, 'ArrowDown') })
  await act(async () => { key(to, 'Enter') })
  expect(chips(container)).toEqual(['jd@work.example'])
  expect(to.value).toBe('')
  expect(options()).toEqual([])

  // The chosen address is not offered again; a click picks the other one.
  await act(async () => type(to, 'jane'))
  expect(options()).toEqual(['Jane Doe jane@example.com'])
  const opt = container.querySelector('[role="option"]') as HTMLElement
  await act(async () => { opt.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true })) })
  expect(chips(container)).toEqual(['jd@work.example', 'jane@example.com'])

  // Escape closes the list and Enter then commits the typed text as before.
  await act(async () => type(to, 'bob@other.example'))
  await act(async () => { key(to, 'Escape') })
  expect(options()).toEqual([])
  await act(async () => { key(to, 'Enter') })
  expect(chips(container)).toEqual(['jd@work.example', 'jane@example.com', 'bob@other.example'])
  await act(async () => root.unmount())
})

it('still takes typed addresses when contacts cannot be loaded', async () => {
  vi.spyOn(API, 'getContacts').mockRejectedValue(new ApiError(503, 'contacts not available'))
  const { container, root } = await mount()
  const to = container.querySelector('input[aria-label="To"]') as HTMLInputElement
  await act(async () => type(to, 'x@example.com'))
  expect(container.querySelector('[role="listbox"]')).toBeNull()
  await act(async () => { key(to, 'Enter') })
  expect(chips(container)).toEqual(['x@example.com'])
  await act(async () => root.unmount())
})
