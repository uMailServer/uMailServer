import { it, expect, vi, afterEach } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { InboxPage } from './inbox'
import api, { type Mail } from '@/utils/api'

// Messages could only be deleted: there was no way to file one into a folder
// or to report spam (which also trains the spam filter) from the list.

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); document.body.innerHTML = '' })

async function mount() {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.spyOn(api, 'getMail').mockResolvedValue({ emails: [{ id: 'm1', subject: 'Offer', from: 'x@y.test', read: true, folder: 'inbox' } as Mail], total: 1 })
  vi.spyOn(api, 'getFolders').mockResolvedValue([
    { name: 'INBOX', role: 'inbox', total: 1, unread: 0 },
    { name: 'Junk', role: 'spam', total: 0, unread: 0 },
    { name: 'Trash', role: 'trash', total: 0, unread: 0 },
    { name: 'Work', total: 0, unread: 0 },
  ])
  const move = vi.spyOn(api, 'moveMail').mockResolvedValue()
  const c = document.createElement('div'); document.body.append(c)
  await act(async () => createRoot(c).render(createElement(MemoryRouter, null, createElement(InboxPage, { folder: 'inbox' }))))
  await act(async () => { await Promise.resolve() })
  const box = c.querySelector('[role="checkbox"]:not([aria-label])') ?? c.querySelectorAll('[role="checkbox"]')[1]
  await act(async () => { (box as HTMLElement).click() })
  return { c, move }
}
const button = (label: string) => Array.from(document.querySelectorAll('button')).find((b) => b.textContent?.trim() === label) as HTMLButtonElement

it('reports spam from the list', async () => {
  const { move } = await mount()
  await act(async () => { button('Spam').click() })
  expect(move).toHaveBeenCalledWith(['m1'], 'inbox', 'spam')
})

it('moves to a folder of the user', async () => {
  const { move } = await mount()
  const trigger = button('Move to')
  await act(async () => {
    trigger.dispatchEvent(new MouseEvent('pointerdown', { bubbles: true, button: 0, ctrlKey: false }))
  })
  await act(async () => { await Promise.resolve() })
  const items = Array.from(document.querySelectorAll('[role="menuitem"]')).map((i) => i.textContent)
  expect(items).toEqual(['Spam', 'Work'])
  const work = Array.from(document.querySelectorAll('[role="menuitem"]')).find((i) => i.textContent === 'Work') as HTMLElement
  await act(async () => { work.click() })
  expect(move).toHaveBeenCalledWith(['m1'], 'inbox', 'Work')
})
