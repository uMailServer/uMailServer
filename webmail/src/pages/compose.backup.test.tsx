import { it, expect, vi, afterEach, beforeEach } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { ComposePage } from './compose'

beforeEach(() => sessionStorage.clear())
afterEach(() => { vi.restoreAllMocks(); document.body.innerHTML = '' })

// F6934: a 401 hard-redirects to /login; unsent text must be restored afterwards.
it('compose restores unsent text from the session backup', async () => {
  sessionStorage.setItem('umail.compose.backup', JSON.stringify({ to: ['a@b.co'], cc: [], subject: 'Subj', body: 'unsent text' }))
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const c = document.createElement('div'); document.body.append(c)
  await act(async () => createRoot(c).render(createElement(MemoryRouter, { initialEntries: ['/compose'] }, createElement(ComposePage))))
  await act(async () => { await Promise.resolve() })
  expect((c.querySelector('textarea') as HTMLTextAreaElement).value).toBe('unsent text')
  expect(c.textContent).toContain('a@b.co')
  expect(sessionStorage.getItem('umail.compose.backup')).toContain('unsent text')
})
