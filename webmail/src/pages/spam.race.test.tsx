import { it, expect, vi, afterEach } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { SpamPage } from './spam'
import { InboxPage } from './inbox'
import { EmailDetailPage } from './email-detail'
import { Routes, Route } from 'react-router-dom'
import API, { Mail, ApiError } from '@/utils/api'

function mail(id: string, subject: string, body = 'body', folder = 'Inbox'): Mail {
  return { id, from: 'a@b.c', fromName: 'A', to: ['me@x.y'], subject, body, preview: body,
    date: '2026-01-01', read: false, starred: false, folder, hasAttachments: false, size: 1 }
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

it('spam page renders real API data and no fabricated mock rows', async () => {
  const spy = vi.spyOn(API, 'getMail').mockResolvedValue({ emails: [mail('r1', 'Real spam subject', 'x', 'Spam')] })
  const { container, root } = await mount(createElement(SpamPage))
  expect(spy).toHaveBeenCalledWith('spam')
  expect(container.textContent).toContain('Real spam subject')
  expect(container.textContent).not.toContain('Casino Winner')
  await act(async () => root.unmount())
})

it('inbox shows error state on API failure instead of mock data', async () => {
  vi.spyOn(API, 'getMail').mockRejectedValue(new Error('boom'))
  const { container, root } = await mount(createElement(InboxPage, { folder: 'inbox' }))
  expect(container.textContent).toContain('boom')
  expect(container.textContent).not.toContain('John Smith')
  await act(async () => root.unmount())
})

it('inbox ignores a stale response after folder switch', async () => {
  const pending: Array<{ f: string; res: (v: any) => void }> = []
  vi.spyOn(API, 'getMail').mockImplementation((f) => new Promise((res) => { pending.push({ f, res }) }))
  const { container, root } = await mount(createElement(InboxPage, { folder: 'inbox' }))
  await act(async () => root.render(createElement(MemoryRouter, null, createElement(InboxPage, { folder: 'trash' }))))
  // MemoryRouter remount means a fresh hook; resolve old first, then new
  await act(async () => pending[0].res({ emails: [mail('old', 'OLD-SUBJECT')] }))
  await act(async () => pending[pending.length - 1].res({ emails: [mail('new', 'NEW-SUBJECT')] }))
  expect(container.textContent).toContain('NEW-SUBJECT')
  expect(container.textContent).not.toContain('OLD-SUBJECT')
  await act(async () => root.unmount())
})

it('detail page loads the message by id and sanitises HTML', async () => {
  vi.spyOn(API, 'setFlags').mockResolvedValue([])
  vi.spyOn(API, 'getMessage').mockResolvedValue(mail('m9', 'Real subject', '<p>hi</p><img src="x" onerror="alert(1)"><script>alert(2)</script>'))
  const { container, root } = await mount(
    createElement(Routes, null, createElement(Route, { path: '/email/:id', element: createElement(EmailDetailPage) })),
    ['/email/m9'])
  expect(container.textContent).toContain('Real subject')
  expect(container.textContent).not.toContain('Project Meeting Discussion')
  expect(container.innerHTML).not.toContain('onerror')
  expect(container.innerHTML).not.toContain('<script')
  await act(async () => root.unmount())
})

it('detail page shows not-found when no folder has the id', async () => {
  vi.spyOn(API, 'getMessage').mockRejectedValue(new ApiError(404, 'Email not found'))
  const { container, root } = await mount(
    createElement(Routes, null, createElement(Route, { path: '/email/:id', element: createElement(EmailDetailPage) })),
    ['/email/nope'])
  expect(container.textContent).toContain('not found')
  await act(async () => root.unmount())
})
