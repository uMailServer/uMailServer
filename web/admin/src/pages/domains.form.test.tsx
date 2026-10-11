import { it, expect, vi, afterEach } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { Domains } from './Domains'

function setInputValue(input: HTMLInputElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
  setter!.call(input, value)
  input.dispatchEvent(new Event('input', { bubbles: true }))
}
const flush = () => act(async () => { await new Promise((r) => setTimeout(r, 0)) })
const btn = (root: ParentNode, label: string) =>
  Array.from(root.querySelectorAll('button')).filter((b) => b.textContent?.trim() === label)

afterEach(() => { vi.unstubAllGlobals() })

async function mount(fetchMock: ReturnType<typeof vi.fn>) {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('fetch', fetchMock)
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  await act(async () => root.render(createElement(Domains)))
  await flush()
  return { container, root }
}

// F6862: max_accounts 0 means "unlimited" server-side; the input coerced 0
// (and an emptied field) back to 100.
it('F6862 max accounts 0 is sent as 0 (unlimited), not coerced to 100', async () => {
  const fetchMock = vi.fn(async (_u: string, o: RequestInit = {}) => {
    if (o.method === 'POST') return { ok: true, status: 201, json: async () => ({}) }
    return { ok: true, status: 200, json: async () => [] }
  })
  const { container, root } = await mount(fetchMock)
  try {
    await act(async () => btn(container, 'Add Domain')[0].click())
    await act(async () => {
      setInputValue(document.getElementById('domain') as HTMLInputElement, 'example.com')
      setInputValue(document.getElementById('max-accounts') as HTMLInputElement, '0')
    })
    await act(async () => btn(document.body, 'Add Domain').pop()!.click())
    await flush()
    const post = fetchMock.mock.calls.find((c) => (c[1] as RequestInit | undefined)?.method === 'POST')
    expect(post, 'no POST issued').toBeTruthy()
    expect(JSON.parse((post![1] as RequestInit).body as string).max_accounts,
      'PROBLEM CONFIRMED: 0 coerced to 100').toBe(0)
  } finally {
    await act(async () => root.unmount()); container.remove()
  }
})

it('F6862 negative or non-integer max accounts is rejected client-side', async () => {
  const fetchMock = vi.fn(async () => ({ ok: true, status: 200, json: async () => [] }))
  const { container, root } = await mount(fetchMock)
  try {
    await act(async () => btn(container, 'Add Domain')[0].click())
    await act(async () => {
      setInputValue(document.getElementById('domain') as HTMLInputElement, 'example.com')
      setInputValue(document.getElementById('max-accounts') as HTMLInputElement, '-5')
    })
    await act(async () => btn(document.body, 'Add Domain').pop()!.click())
    await flush()
    expect(document.body.textContent).toMatch(/max accounts/i)
    expect(fetchMock.mock.calls.some((c) => (c as unknown[])[1] && ((c as unknown[])[1] as RequestInit).method === 'POST')).toBe(false)
  } finally {
    await act(async () => root.unmount()); container.remove()
  }
})

// F6863: a failed create left its error in the shared formError; cancel and
// reopen showed the stale error (and stale typed name) on a fresh dialog.
it('F6863 reopening the add dialog clears the previous error', async () => {
  const fetchMock = vi.fn(async (_u: string, o: RequestInit = {}) => {
    if (o.method === 'POST') return { ok: false, status: 409, json: async () => ({ error: 'domain already exists' }) }
    return { ok: true, status: 200, json: async () => [] }
  })
  const { container, root } = await mount(fetchMock)
  try {
    await act(async () => btn(container, 'Add Domain')[0].click())
    await act(async () => setInputValue(document.getElementById('domain') as HTMLInputElement, 'dup.example.com'))
    await act(async () => btn(document.body, 'Add Domain').pop()!.click())
    await flush()
    expect(document.body.textContent).toContain('domain already exists')
    await act(async () => btn(document.body, 'Cancel')[0].click())
    await flush()
    await act(async () => btn(container, 'Add Domain')[0].click())
    await flush()
    expect(document.body.textContent, 'PROBLEM CONFIRMED: stale error on reopen').not.toContain('domain already exists')
  } finally {
    await act(async () => root.unmount()); container.remove()
  }
})
