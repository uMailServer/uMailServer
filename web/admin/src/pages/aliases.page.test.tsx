import { it, expect, vi, afterEach } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { Aliases } from './Aliases'

const flush = () => act(async () => { await new Promise((r) => setTimeout(r, 0)) })
const btn = (root: ParentNode, label: string) =>
  Array.from(root.querySelectorAll('button')).filter((b) => b.textContent?.trim() === label)
function setInputValue(input: HTMLInputElement, value: string) {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
  input.dispatchEvent(new Event('input', { bubbles: true }))
}
afterEach(() => { vi.unstubAllGlobals() })

const row = { alias: 'info@example.com', target: 'bob@example.com', domain: 'example.com', is_active: true, created_at: '' }

type Call = { url: string; method: string; body?: unknown }

function setup(handler: (c: Call) => { status: number; json?: unknown } | undefined = () => undefined) {
  const calls: Call[] = []
  let list = [row]
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  // jsdom has no PointerEvent, which the Base UI Switch reads on click.
  vi.stubGlobal('PointerEvent', MouseEvent)
  vi.stubGlobal('fetch', vi.fn(async (u: string, o: RequestInit = {}) => {
    const call: Call = { url: u, method: o.method ?? 'GET', body: o.body ? JSON.parse(o.body as string) : undefined }
    calls.push(call)
    const custom = handler(call)
    if (custom) {
      return { ok: custom.status < 400, status: custom.status, json: async () => custom.json, headers: { get: () => null } }
    }
    if (call.method === 'DELETE') { list = []; return { ok: true, status: 204, json: async () => undefined } }
    if (call.method === 'POST') { list = [...list, { ...row, ...(call.body as object), alias: (call.body as { alias: string }).alias }]; return { ok: true, status: 201, json: async () => row } }
    if (call.method === 'PUT') { list = list.map((r) => ({ ...r, ...(call.body as object) })); return { ok: true, status: 200, json: async () => row } }
    return { ok: true, status: 200, json: async () => list }
  }))
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  return { calls, container, root, cleanup: async () => { await act(async () => root.unmount()); container.remove() } }
}

it('lists aliases and creates one with the typed addresses', async () => {
  const t = setup()
  try {
    await act(async () => t.root.render(createElement(Aliases)))
    await flush()
    expect(t.container.textContent).toContain('info@example.com')
    expect(t.container.textContent).toContain('bob@example.com')
    await act(async () => {
      setInputValue(t.container.querySelector('#alias-address') as HTMLInputElement, 'sales@example.com')
      setInputValue(t.container.querySelector('#alias-target') as HTMLInputElement, 'bob@example.com')
    })
    await act(async () => btn(t.container, 'Add Alias')[0].click())
    await flush()
    const post = t.calls.find((c) => c.method === 'POST')!
    expect(post.url).toBe('/api/v1/aliases')
    expect(post.body).toEqual({ alias: 'sales@example.com', target: 'bob@example.com' })
    expect(t.container.textContent).toContain('sales@example.com')
  } finally { await t.cleanup() }
})

it('rejects a malformed address without calling the API', async () => {
  const t = setup()
  try {
    await act(async () => t.root.render(createElement(Aliases)))
    await flush()
    await act(async () => {
      setInputValue(t.container.querySelector('#alias-address') as HTMLInputElement, 'not-an-address')
      setInputValue(t.container.querySelector('#alias-target') as HTMLInputElement, 'bob@example.com')
    })
    await act(async () => btn(t.container, 'Add Alias')[0].click())
    await flush()
    expect(t.calls.filter((c) => c.method === 'POST')).toHaveLength(0)
    expect(t.container.querySelector('[role="alert"]')).not.toBeNull()
  } finally { await t.cleanup() }
})

it('shows the server error when the target account does not exist', async () => {
  const t = setup((c) => (c.method === 'POST' ? { status: 400, json: { error: 'target account not found' } } : undefined))
  try {
    await act(async () => t.root.render(createElement(Aliases)))
    await flush()
    await act(async () => {
      setInputValue(t.container.querySelector('#alias-address') as HTMLInputElement, 'sales@example.com')
      setInputValue(t.container.querySelector('#alias-target') as HTMLInputElement, 'ghost@example.com')
    })
    await act(async () => btn(t.container, 'Add Alias')[0].click())
    await flush()
    expect(t.container.textContent).toContain('target account not found')
  } finally { await t.cleanup() }
})

it('disables an alias with a PUT and deletes only after confirmation', async () => {
  const t = setup()
  try {
    await act(async () => t.root.render(createElement(Aliases)))
    await flush()
    await act(async () => (t.container.querySelector('[aria-label="Toggle info@example.com"]') as HTMLElement).click())
    await flush()
    const put = t.calls.find((c) => c.method === 'PUT')!
    expect(put.url).toBe('/api/v1/aliases/info%40example.com')
    expect(put.body).toEqual({ is_active: false })

    await act(async () => (t.container.querySelector('[aria-label="Delete info@example.com"]') as HTMLElement).click())
    expect(t.calls.filter((c) => c.method === 'DELETE'), 'first click only asks').toHaveLength(0)
    await act(async () => btn(t.container, 'Cancel')[0].click())
    expect(t.calls.filter((c) => c.method === 'DELETE')).toHaveLength(0)

    await act(async () => (t.container.querySelector('[aria-label="Delete info@example.com"]') as HTMLElement).click())
    await act(async () => btn(t.container, 'Confirm delete')[0].click())
    await flush()
    expect(t.calls.filter((c) => c.method === 'DELETE')).toHaveLength(1)
    expect(t.container.textContent).toContain('No aliases yet')
  } finally { await t.cleanup() }
})
