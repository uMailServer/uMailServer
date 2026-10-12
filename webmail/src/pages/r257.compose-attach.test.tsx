import { it, expect, vi, afterEach } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import API from '@/utils/api'
import { attachmentError, MAX_ATTACHMENT_BYTES } from '@/utils/attachments'
import { ComposePage } from './compose'

afterEach(() => { vi.restoreAllMocks(); document.body.innerHTML = ''; sessionStorage.clear() })

it('attachmentError enforces count, per-file and total caps', () => {
  expect(attachmentError([], { name: 'a', size: MAX_ATTACHMENT_BYTES + 1 })).toMatch(/larger/)
  expect(attachmentError([{ size: 10 << 20 }], { name: 'a', size: 6 << 20 })).toMatch(/total/)
  expect(attachmentError(Array(20).fill({ size: 1 }), { name: 'a', size: 1 })).toMatch(/At most/)
  expect(attachmentError([], { name: 'a', size: 5 })).toBeNull()
})

it('compose adds, removes and sends attachments', async () => {
  const send = vi.spyOn(API, 'sendMail').mockResolvedValue(undefined)
  vi.spyOn(API, 'saveDraft').mockResolvedValue({ id: 'd' } as never)
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const c = document.createElement('div'); document.body.append(c)
  await act(async () => createRoot(c).render(createElement(MemoryRouter, { initialEntries: ['/compose?replyTo=a@b.co&subject=S'] }, createElement(ComposePage))))
  const input = c.querySelector('input[type=file]') as HTMLInputElement
  const pick = async (...files: File[]) => {
    Object.defineProperty(input, 'files', { value: files, configurable: true })
    await act(async () => { input.dispatchEvent(new Event('change', { bubbles: true })); await new Promise((r) => setTimeout(r, 50)) })
  }
  await pick(new File(['hello'], 'a.txt', { type: 'text/plain' }), new File(['bye'], 'b.txt', { type: 'text/plain' }))
  expect(c.textContent).toContain('a.txt')
  expect(c.textContent).toContain('b.txt')
  await act(async () => { (c.querySelector('button[aria-label="Remove attachment b.txt"]') as HTMLButtonElement).click() })
  expect(c.textContent).not.toContain('b.txt')
  const sendBtn = Array.from(c.querySelectorAll('button')).find((b) => b.textContent?.includes('Send')) as HTMLButtonElement
  await act(async () => { sendBtn.click(); await Promise.resolve() })
  expect(send).toHaveBeenCalledTimes(1)
  expect(send.mock.calls[0][0].attachments).toEqual([{ filename: 'a.txt', contentType: 'text/plain', data: btoa('hello') }])
})
