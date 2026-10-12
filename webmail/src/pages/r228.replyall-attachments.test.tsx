import { it, expect, vi, afterEach } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import API, { Mail } from '@/utils/api'
import { replyAllRecipients } from '@/utils/quote'
import { ComposePage } from './compose'
import { EmailDetailPage } from './email-detail'

afterEach(() => { vi.restoreAllMocks(); document.body.innerHTML = '' })

const base = { id: 'm1', from: 'Al <al@x.y>', fromName: 'Al', to: ['Me <me@x.y>', 'bo@x.y'], cc: ['ME@x.y', 'bo@x.y', 'cy@x.y'],
  subject: 'S', body: 'B', preview: '', date: 'D', read: true, starred: false, folder: 'inbox', hasAttachments: true, size: 1, account: 'me@x.y' }

it('replyAllRecipients excludes self and dedups', () => {
  expect(replyAllRecipients('Al <al@x.y>', ['Me <me@x.y>', 'BO@x.y'], ['ME@x.y', 'bo@x.y', 'cy@x.y'], 'me@x.y'))
    .toEqual({ to: ['al@x.y', 'BO@x.y'], cc: ['cy@x.y'] })
})

it('compose replyall prefills to and cc', async () => {
  vi.spyOn(API, 'getMessage').mockResolvedValue(base as Mail)
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const c = document.createElement('div'); document.body.append(c)
  await act(async () => createRoot(c).render(createElement(MemoryRouter, { initialEntries: ['/compose?mode=replyall&origId=m1'] }, createElement(ComposePage))))
  await act(async () => { await Promise.resolve() })
  expect(c.textContent).toContain('al@x.y')
  expect(c.textContent).toContain('cy@x.y')
  expect(c.textContent).not.toContain('me@x.y')
})

it('message view lists attachments with download links and Reply all', async () => {
  vi.spyOn(API, 'getMessage').mockResolvedValue({ ...base, attachments: [{ index: 0, filename: 'a.pdf', contentType: 'application/pdf', size: 2048 }] } as Mail)
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const c = document.createElement('div'); document.body.append(c)
  await act(async () => createRoot(c).render(createElement(MemoryRouter, { initialEntries: ['/email/m1'] },
    createElement(Routes, null, createElement(Route, { path: '/email/:id', element: createElement(EmailDetailPage) })))))
  await act(async () => { await Promise.resolve() })
  const a = c.querySelector('a[download]') as HTMLAnchorElement
  expect(a.getAttribute('href')).toContain('/mail/attachment?')
  expect(a.getAttribute('href')).toContain('index=0')
  expect(c.textContent).toContain('a.pdf')
  expect(c.textContent).toContain('Reply all')
  expect(c.textContent).toContain('Cc:')
})
