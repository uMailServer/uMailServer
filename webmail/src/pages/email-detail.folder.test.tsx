import { it, expect, vi, afterEach } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { EmailDetailPage } from './email-detail'
import api, { type Mail } from '@/utils/api'

// The detail page lower-cased the message's folder, so flags and moves for a
// message in a folder such as "Work" addressed "work", which does not exist.

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); document.body.innerHTML = '' })

it('keeps the case of a user folder and lower-cases standard ones', async () => {
  for (const [stored, want] of [['Work', 'Work'], ['Spam', 'spam']]) {
    document.body.innerHTML = ''
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    vi.spyOn(api, 'getMessage').mockResolvedValue({ id: 'm1', subject: 's', from: 'a@x.test', to: [], body: 'hi', read: false, folder: stored } as unknown as Mail)
    const flags = vi.spyOn(api, 'setFlags').mockResolvedValue(['m1'])
    const c = document.createElement('div'); document.body.append(c)
    await act(async () => createRoot(c).render(createElement(MemoryRouter, { initialEntries: ['/email/m1'] },
      createElement(Routes, null, createElement(Route, { path: '/email/:id', element: createElement(EmailDetailPage) })))))
    await act(async () => { await Promise.resolve() })
    expect(flags).toHaveBeenCalledWith('m1', expect.objectContaining({ folder: want }))
    vi.restoreAllMocks()
  }
})
