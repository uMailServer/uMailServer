import { it, expect, vi, afterEach } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { ContactsPage } from './contacts'
import { ComposePage } from './compose'
import API, { ApiError, type Contact } from '@/utils/api'

const jane: Contact = { uid: 'u1', addressbook: 'contacts', name: 'Jane Doe', emails: ['jane@example.com'], phones: ['+1 555'], organization: 'Acme' }
const bob: Contact = { uid: 'u2', addressbook: 'phone', name: '', emails: ['bob@other.example'], phones: null }

async function mount(el: any, entries = ['/contacts']) {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  await act(async () => root.render(createElement(MemoryRouter, { initialEntries: entries }, el)))
  return { container, root }
}
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); document.body.innerHTML = '' })

function setValue(el: HTMLInputElement | HTMLTextAreaElement, value: string) {
  const proto = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype
  Object.getOwnPropertyDescriptor(proto, 'value')!.set!.call(el, value)
  el.dispatchEvent(new Event('input', { bubbles: true }))
}
const button = (label: string | RegExp) =>
  Array.from(document.querySelectorAll('button')).find((b) =>
    typeof label === 'string' ? b.textContent?.trim() === label || b.getAttribute('aria-label') === label : label.test(b.textContent ?? ''))!

it('lists the server contacts and filters them', async () => {
  vi.spyOn(API, 'getContacts').mockResolvedValue([jane, bob])
  const { container, root } = await mount(createElement(ContactsPage))
  expect(container.textContent).toContain('Jane Doe')
  expect(container.textContent).toContain('bob@other.example')
  expect(container.textContent).toContain('Acme')
  const search = container.querySelector('input[aria-label="Search contacts"]') as HTMLInputElement
  await act(async () => setValue(search, 'acme'))
  expect(container.textContent).toContain('Jane Doe')
  expect(container.textContent).not.toContain('bob@other.example')
  await act(async () => root.unmount())
})

it('creates a contact from the form', async () => {
  const list = vi.spyOn(API, 'getContacts').mockResolvedValue([])
  const create = vi.spyOn(API, 'createContact').mockResolvedValue(jane)
  const { container, root } = await mount(createElement(ContactsPage))
  expect(container.textContent).toContain('No contacts yet')
  await act(async () => button('New contact').click())
  await act(async () => {
    setValue(document.getElementById('contact-name') as HTMLInputElement, 'Jane Doe')
    setValue(document.getElementById('contact-emails') as HTMLTextAreaElement, 'jane@example.com\n\n j2@example.com ')
  })
  await act(async () => button('Save').click())
  expect(create).toHaveBeenCalledWith({ name: 'Jane Doe', emails: ['jane@example.com', 'j2@example.com'], phones: [], organization: '', note: '' })
  expect(list).toHaveBeenCalledTimes(2) // reloaded after saving
  await act(async () => root.unmount())
})

it('edits and deletes the right contact', async () => {
  vi.spyOn(API, 'getContacts').mockResolvedValue([jane, bob])
  const update = vi.spyOn(API, 'updateContact').mockResolvedValue(bob)
  const del = vi.spyOn(API, 'deleteContact').mockResolvedValue()
  vi.stubGlobal('confirm', () => true)
  const { root } = await mount(createElement(ContactsPage))
  await act(async () => button('Edit bob@other.example').click())
  expect((document.getElementById('contact-emails') as HTMLTextAreaElement).value).toBe('bob@other.example')
  await act(async () => setValue(document.getElementById('contact-name') as HTMLInputElement, 'Bob'))
  await act(async () => button('Save').click())
  expect(update.mock.calls[0][0]).toMatchObject({ addressbook: 'phone', uid: 'u2' })
  expect(update.mock.calls[0][1]).toMatchObject({ name: 'Bob', emails: ['bob@other.example'] })
  await act(async () => button('Delete Jane Doe').click())
  expect(del.mock.calls[0][0]).toMatchObject({ addressbook: 'contacts', uid: 'u1' })
  await act(async () => root.unmount())
})

it('surfaces a load failure', async () => {
  vi.spyOn(API, 'getContacts').mockRejectedValue(new ApiError(503, 'contacts not available'))
  const { container, root } = await mount(createElement(ContactsPage))
  expect(container.textContent).toContain('contacts not available')
  expect(container.textContent).not.toContain('No contacts yet')
  await act(async () => root.unmount())
})

it('compose prefills the recipient from ?to=', async () => {
  const send = vi.spyOn(API, 'sendMail').mockResolvedValue()
  const { container, root } = await mount(createElement(ComposePage), ['/compose?to=jane%40example.com&subject=Hi'])
  const btn = Array.from(container.querySelectorAll('button')).find((b) => b.textContent?.includes('Send'))!
  await act(async () => btn.click())
  expect(send.mock.calls[0][0].to).toEqual(['jane@example.com'])
  await act(async () => root.unmount())
})
