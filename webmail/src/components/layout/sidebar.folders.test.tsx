import { it, expect, vi, afterEach } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { Sidebar } from './sidebar'
import { FolderPage } from '@/pages/folder'
import api, { type Mail } from '@/utils/api'

// The sidebar showed fixed "Work", "Personal" and "Important" entries and a
// hardcoded Spam count of 5; the user's real mailboxes were unreachable.

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); document.body.innerHTML = '' })

async function render(el: ReturnType<typeof createElement>, path = '/inbox') {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const c = document.createElement('div'); document.body.append(c)
  await act(async () => createRoot(c).render(createElement(MemoryRouter, { initialEntries: [path] }, el)))
  await act(async () => { await Promise.resolve() })
  return c
}

it('lists the real folders with real unread counts', async () => {
  vi.spyOn(api, 'getFolders').mockResolvedValue([
    { name: 'INBOX', role: 'inbox', total: 4, unread: 1 },
    { name: 'Junk', role: 'spam', total: 9, unread: 3 },
    { name: 'Work/Q4', total: 2, unread: 2 },
    { name: 'archive', total: 7, unread: 0 },
  ])
  const c = await render(createElement(Sidebar, { collapsed: false, onToggle: () => {} }))
  const links = Array.from(c.querySelectorAll('a'))
  const spam = links.find((a) => a.textContent?.startsWith('Spam'))!
  expect(spam.textContent).toBe('Spam3')
  const work = links.find((a) => a.textContent?.startsWith('Work/Q4'))!
  expect(work.getAttribute('href')).toBe('/folder/Work%2FQ4')
  expect(work.textContent).toBe('Work/Q42')
  expect(links.find((a) => a.textContent === 'archive')).toBeTruthy()
  for (const fake of ['Personal', 'Important']) {
    expect(c.textContent).not.toContain(fake)
  }
})

it('shows no Spam count when there is no unread spam or the list fails', async () => {
  vi.spyOn(api, 'getFolders').mockRejectedValue(new Error('offline'))
  const c = await render(createElement(Sidebar, { collapsed: false, onToggle: () => {} }))
  const spam = Array.from(c.querySelectorAll('a')).find((a) => a.textContent?.startsWith('Spam'))!
  expect(spam.textContent).toBe('Spam')
})

it('lists the messages of a folder of the user', async () => {
  const get = vi.spyOn(api, 'getMail').mockResolvedValue({ emails: [{ id: 'm1', subject: 'Budget', from: 'a@x.test', read: true, folder: 'Work/Q4' } as Mail], total: 1 })
  const c = await render(createElement(Routes, null, createElement(Route, { path: '/folder/:type', element: createElement(FolderPage) })), '/folder/Work%2FQ4')
  expect(get).toHaveBeenCalledWith('Work/Q4')
  expect(c.textContent).toContain('Budget')
})
