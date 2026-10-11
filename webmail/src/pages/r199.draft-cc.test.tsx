import { it, expect, vi, afterEach } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import API, { Mail } from '@/utils/api'
import { ComposePage } from './compose'

afterEach(() => { vi.restoreAllMocks(); document.body.innerHTML = '' })

// F6760: opening a draft must restore Cc/Bcc returned by /mail/message.
it('draft open restores cc and bcc', async () => {
  const m = { id: 'd1', from: 'a@b.c', fromName: '', to: ['t@x.y'], cc: ['c@x.y'], bcc: ['b@x.y'], subject: 'S', body: 'B',
    preview: '', date: '', read: true, starred: false, folder: 'Drafts', hasAttachments: false, size: 1 } as Mail
  vi.spyOn(API, 'getMessage').mockResolvedValue(m)
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const c = document.createElement('div'); document.body.append(c)
  await act(async () => createRoot(c).render(createElement(MemoryRouter, { initialEntries: ['/compose?draft=d1'] }, createElement(ComposePage))))
  await act(async () => { await Promise.resolve() })
  expect(c.textContent).toContain('t@x.y')   // control: to restored
  expect(c.textContent).toContain('c@x.y')
  expect(c.textContent).toContain('b@x.y')
})
