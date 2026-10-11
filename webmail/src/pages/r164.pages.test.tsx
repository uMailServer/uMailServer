import { it, expect, vi, afterEach } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { SentPage } from './sent'
import { DraftsPage } from './drafts'
import { TrashPage } from './trash'
import { ContactsPage } from './contacts'
import { FolderPage } from './folder'
import { ComposePage } from './compose'
import API, { Mail } from '@/utils/api'

function mail(id: string, subject: string): Mail {
  return { id, from: 'a@b.c', fromName: 'A', to: ['rcpt@x.y'], subject, body: 'b', preview: 'pv',
    date: '2026-01-01', read: true, starred: false, folder: 'x', hasAttachments: false, size: 1 }
}
async function mount(el: any, entries = ['/']) {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  await act(async () => root.render(createElement(MemoryRouter, { initialEntries: entries }, el)))
  return { container, root }
}
afterEach(() => { vi.restoreAllMocks(); document.body.innerHTML = '' })

for (const [name, Page, folder, mockText] of [
  ['sent', SentPage, 'sent', 'Project Meeting'],
  ['drafts', DraftsPage, 'drafts', 'Meeting Notes'],
  ['trash', TrashPage, 'trash', 'zzz'],
] as const) {
  it(`${name} uses real API and shows real rows`, async () => {
    const spy = vi.spyOn(API, 'getMail').mockResolvedValue({ emails: [mail('r1', 'REAL-SUBJ')] })
    const { container, root } = await mount(createElement(Page))
    expect(spy).toHaveBeenCalledWith(folder)
    expect(container.textContent).toContain('REAL-SUBJ')
    expect(container.textContent).not.toContain(mockText)
    await act(async () => root.unmount())
  })
  it(`${name} surfaces load errors`, async () => {
    vi.spyOn(API, 'getMail').mockRejectedValue(new Error('kaboom'))
    const { container, root } = await mount(createElement(Page))
    expect(container.textContent).toContain('kaboom')
    await act(async () => root.unmount())
  })
}

it('contacts/folder pages show no fabricated data', async () => {
  const a = await mount(createElement(ContactsPage))
  expect(a.container.textContent).not.toContain('John Smith')
  const b = await mount(createElement(FolderPage), ['/folder/work'])
  expect(b.container.textContent).not.toContain('Annual Leave Planning')
})

it('compose really sends, surfaces errors and blocks double submit', async () => {
  let rej: (e: Error) => void = () => {}
  const spy = vi.spyOn(API, 'sendMail').mockImplementation(() => new Promise<void>((_, r) => { rej = r }))
  const { container, root } = await mount(createElement(ComposePage), ['/compose?replyTo=bob%40x.y&subject=Re%3A%20Hi'])
  expect(container.textContent).not.toContain('John Smith')
  const btn = Array.from(container.querySelectorAll('button')).find((b) => b.textContent?.includes('Send'))!
  await act(async () => { btn.click(); btn.click() })
  expect(spy).toHaveBeenCalledTimes(1)
  expect(spy.mock.calls[0][0].to).toEqual(['bob@x.y'])
  expect(spy.mock.calls[0][0].subject).toBe('Re: Hi')
  await act(async () => rej(new Error('smtp down')))
  expect(window.location.pathname).not.toBe('/sent')
  await act(async () => root.unmount())
})
