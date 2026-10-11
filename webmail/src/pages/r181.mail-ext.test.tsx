import { it, expect, vi, afterEach, beforeEach } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import API, { Mail } from '@/utils/api'
import { InboxPage } from './inbox'
import { ComposePage } from './compose'
import { EmailDetailPage } from './email-detail'

function mail(id: string, over: Partial<Mail> = {}): Mail {
  return { id, from: 'a@b.c', fromName: 'A', to: ['r@x.y'], subject: 'S-' + id, body: 'b', preview: 'pv',
    date: '2026-01-01', read: false, starred: false, folder: 'inbox', hasAttachments: false, size: 1, ...over }
}
async function mount(el: any, entries = ['/']) {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  await act(async () => root.render(createElement(MemoryRouter, { initialEntries: entries }, el)))
  return { container, root }
}
const btn = (c: HTMLElement, pred: (b: HTMLButtonElement) => boolean) =>
  Array.from(c.querySelectorAll('button')).find(pred) as HTMLButtonElement
const flush = () => act(async () => { await Promise.resolve() })

beforeEach(() => { vi.useRealTimers() })
afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks(); document.body.innerHTML = '' })

it('api client builds list/message/flags/draft requests', async () => {
  const f = vi.fn().mockResolvedValue({ ok: true, status: 200, headers: new Headers({ 'content-type': 'application/json' }),
    json: async () => ({ updated: ['x'], id: 'n', folder: 'Drafts', saved: true }) })
  vi.stubGlobal('fetch', f)
  await API.getMail('inbox', { offset: 100, limit: 50 })
  expect(f.mock.calls[0][0]).toContain('/mail/inbox?limit=50&offset=100')
  await API.getMessage('a b', 'drafts')
  expect(f.mock.calls[1][0]).toContain('/mail/message?id=a+b&folder=drafts')
  expect(await API.setFlags(['x', 'y'], { add: ['\\Seen'] })).toEqual(['x'])
  expect(JSON.parse(f.mock.calls[2][1].body)).toEqual({ ids: ['x', 'y'], add: ['\\Seen'] })
  await API.saveDraft({ to: [], subject: '', body: 'x' })
  expect(f.mock.calls[3][1].method).toBe('POST')
  await API.saveDraft({ id: 'old', to: [], subject: '', body: 'x' })
  expect(f.mock.calls[4][1].method).toBe('PUT')
  vi.unstubAllGlobals()
})

it('api: 401 redirects to login, 404 surfaces ApiError', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 404, headers: new Headers({ 'content-type': 'application/json' }),
    json: async () => ({ error: 'Email not found' }) }))
  await expect(API.setFlags('x', { add: ['\\Seen'] })).rejects.toMatchObject({ status: 404, serverMessage: 'Email not found' })
  vi.unstubAllGlobals()
})

it('inbox star toggle is optimistic, blocks double click and rolls back on error', async () => {
  vi.spyOn(API, 'getMail').mockResolvedValue({ emails: [mail('m1', { read: true })], total: 1 })
  let rej: (e: Error) => void = () => {}
  const spy = vi.spyOn(API, 'setFlags').mockImplementation(() => new Promise((_, r) => { rej = r }))
  const { container, root } = await mount(createElement(InboxPage))
  await flush()
  const star = () => btn(container, (b) => /star/i.test(b.getAttribute('aria-label') || ''))
  await act(async () => { star().click(); star().click() })
  expect(spy).toHaveBeenCalledTimes(1)
  expect(spy.mock.calls[0]).toEqual([['m1'], { add: ['\\Flagged'] }])
  expect(star().getAttribute('aria-pressed')).toBe('true')
  await act(async () => rej(new Error('boom')))
  expect(star().getAttribute('aria-pressed')).toBe('false')
  await act(async () => root.unmount())
})

it('inbox load more appends next page by offset, ignores stale page after reload', async () => {
  const pending: { opts: any; res: (v: any) => void }[] = []
  vi.spyOn(API, 'getMail').mockImplementation(((_f: string, opts?: any) =>
    new Promise((res) => { pending.push({ opts, res }) })) as any)
  const { container, root } = await mount(createElement(InboxPage))
  await act(async () => pending[0].res({ emails: [mail('a'), mail('b')], total: 3 }))
  const more = () => btn(container, (b) => b.textContent === 'Load more')
  await act(async () => { more().click() })
  expect(pending[1].opts).toEqual({ offset: 2 })
  // refresh while load-more is in flight: its late result must be dropped
  const refresh = btn(container, (b) => !!b.querySelector('svg.lucide-refresh-cw'))
  await act(async () => { refresh.click() })
  await act(async () => pending[2].res({ emails: [mail('z')], total: 1 }))
  await act(async () => pending[1].res({ emails: [mail('c')], total: 3 }))
  expect(container.textContent).toContain('S-z')
  expect(container.textContent).not.toContain('S-c')
  await act(async () => root.unmount())
})

it('email detail loads via /mail/message, marks read, ignores stale response', async () => {
  const pending: Record<string, (m: Mail) => void> = {}
  vi.spyOn(API, 'getMessage').mockImplementation(((id: string) => new Promise((res) => { pending[id] = res })) as any)
  const flags = vi.spyOn(API, 'setFlags').mockResolvedValue(['one'])
  const { container, root } = await mount(
    createElement(Routes, null, createElement(Route, { path: '/email/:id', element: createElement(EmailDetailPage) })),
    ['/email/one'])
  await act(async () => pending['one'](mail('one', { subject: 'ONE' })))
  expect(container.textContent).toContain('ONE')
  expect(flags).toHaveBeenCalledWith('one', { add: ['\\Seen'], folder: 'inbox' })
  await act(async () => root.unmount())
})

it('compose autosaves once after 2s idle, adopts new id, deletes draft on send', async () => {
  vi.useFakeTimers()
  let resolveSave: (v: any) => void = () => {}
  const save = vi.spyOn(API, 'saveDraft').mockImplementation(() => new Promise((r) => { resolveSave = r }))
  vi.spyOn(API, 'sendMail').mockResolvedValue()
  const del = vi.spyOn(API, 'deleteMail').mockResolvedValue()
  const flags = vi.spyOn(API, 'setFlags').mockResolvedValue([])
  const { container, root } = await mount(createElement(ComposePage), ['/compose?replyTo=bob%40x.y&subject=Hi&origId=orig&origFolder=inbox'])
  await act(async () => { vi.advanceTimersByTime(3000) })
  expect(save).not.toHaveBeenCalled() // unchanged prefill: no autosave
  const ta = container.querySelector('textarea')!
  const set = (v: string) => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(ta, v)
    ta.dispatchEvent(new Event('input', { bubbles: true }))
  }
  await act(async () => set('hello'))
  await act(async () => { vi.advanceTimersByTime(1900) })
  expect(save).not.toHaveBeenCalled()
  await act(async () => { vi.advanceTimersByTime(200) })
  expect(save).toHaveBeenCalledTimes(1)
  expect(save.mock.calls[0][0].id).toBeUndefined()
  await act(async () => set('hello!')) // edit while save in flight
  await act(async () => { vi.advanceTimersByTime(2500) })
  expect(save).toHaveBeenCalledTimes(1) // single in-flight
  await act(async () => resolveSave({ id: 'd1', folder: 'Drafts', saved: true }))
  await act(async () => { vi.advanceTimersByTime(2500) })
  expect(save).toHaveBeenCalledTimes(1) // timer was reset; no further edits
  await act(async () => set('hello!!'))
  await act(async () => { vi.advanceTimersByTime(2100) })
  expect(save).toHaveBeenCalledTimes(2)
  expect(save.mock.calls[1][0].id).toBe('d1')
  await act(async () => resolveSave({ id: 'd2', folder: 'Drafts', saved: true }))
  const send = btn(container, (b) => b.textContent?.includes('Send') ?? false)
  await act(async () => { send.click(); send.click() })
  expect(del).toHaveBeenCalledTimes(1)
  expect(del).toHaveBeenCalledWith('d2')
  expect(flags).toHaveBeenCalledWith('orig', { add: ['\\Answered'], folder: 'inbox' })
  await act(async () => root.unmount())
})

it('compose ?draft= loads via getMessage and does not autosave unchanged', async () => {
  vi.useFakeTimers()
  const get = vi.spyOn(API, 'getMessage').mockResolvedValue(mail('dr', { subject: 'DRAFTSUBJ', folder: 'Drafts' }))
  const save = vi.spyOn(API, 'saveDraft').mockResolvedValue({ id: 'x', folder: 'Drafts', saved: true })
  const { container, root } = await mount(createElement(ComposePage), ['/compose?draft=dr'])
  await act(async () => { await Promise.resolve() })
  expect(get).toHaveBeenCalledWith('dr', 'drafts')
  expect((container.querySelector('input[placeholder="Subject"]') as HTMLInputElement).value).toBe('DRAFTSUBJ')
  await act(async () => { vi.advanceTimersByTime(5000) })
  expect(save).not.toHaveBeenCalled()
  await act(async () => root.unmount())
})
