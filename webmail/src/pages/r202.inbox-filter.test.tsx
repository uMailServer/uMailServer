import { it, expect, vi, afterEach } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import API, { Mail } from '@/utils/api'
import { InboxPage } from './inbox'

const mail = (id: string): Mail => ({ id, from: 'a@b.c', fromName: 'A', to: ['r@x.y'], subject: 'S-' + id, body: 'b', preview: 'pv',
  date: '2026-01-01', read: false, starred: true, folder: 'inbox', hasAttachments: false, size: 1 })

afterEach(() => { vi.restoreAllMocks(); document.body.innerHTML = '' })

// F6761: Starred view asks the server for flagged messages and pages with it.
it('starred view uses server-side flag filter for first page and load more', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const spy = vi.spyOn(API, 'getMail').mockResolvedValue({ emails: [mail('1')], total: 2, unreadTotal: 1 })
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  await act(async () => root.render(createElement(MemoryRouter, null, createElement(InboxPage, { folder: 'starred' }))))
  expect(spy).toHaveBeenCalledWith('inbox', { flag: 'flagged' })
  const more = Array.from(container.querySelectorAll('button')).find((b) => b.textContent === 'Load more') as HTMLButtonElement
  expect(more).toBeTruthy()
  spy.mockResolvedValue({ emails: [mail('2')], total: 2 })
  await act(async () => { more.click() })
  expect(spy).toHaveBeenLastCalledWith('inbox', { offset: 1, flag: 'flagged' })
})

it('api client serializes flag/unread', async () => {
  const f = vi.fn().mockResolvedValue({ ok: true, status: 200, headers: new Headers({ 'content-type': 'application/json' }), json: async () => ({}) })
  vi.stubGlobal('fetch', f)
  await API.getMail('inbox', { offset: 3, flag: 'flagged', unread: true })
  expect(f.mock.calls[0][0]).toContain('/mail/inbox?offset=3&flag=flagged&unread=1')
})
