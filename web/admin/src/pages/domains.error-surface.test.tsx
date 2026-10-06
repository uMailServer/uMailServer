import { it, expect, vi } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { useDomains } from '../hooks/useApi'
import { Domains } from './Domains'

function setInputValue(input: HTMLInputElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(
    HTMLInputElement.prototype,
    'value'
  )?.set
  if (!setter) throw new Error('input value setter unavailable')
  setter.call(input, value)
  input.dispatchEvent(new Event('input', { bubbles: true }))
}

it('apiRequest failures are Error instances carrying the server message and status', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValueOnce({
      ok: false,
      status: 503,
      json: async () => ({ error: 'temporary failure' }),
    })
  )

  let current!: ReturnType<typeof useDomains>
  function Probe() {
    current = useDomains()
    return null
  }
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  try {
    await act(async () => root.render(createElement(Probe)))
    let caught: unknown = null
    await act(async () => {
      try {
        await current.fetchDomains()
      } catch (err) {
        caught = err
      }
    })
    console.log(
      'THROWN:',
      caught instanceof Error
        ? `Error(message=${JSON.stringify((caught as Error).message)}, status=${(caught as { status?: number }).status})`
        : `NOT-AN-ERROR: ${JSON.stringify(caught)}`
    )
    expect(caught, 'CONTROL FAILED: failing fetch did not reject').not.toBeNull()
    expect(
      caught instanceof Error,
      'PROBLEM CONFIRMED: apiRequest throws a non-Error value, so every "err instanceof Error" catch site discards the server message'
    ).toBe(true)
    expect((caught as Error).message).toBe('temporary failure')
    expect((caught as { status?: number }).status).toBe(503)
    expect(current.error?.message).toBe('temporary failure')
    console.log('FIX VERIFIED: hook-level contract holds')
  } finally {
    await act(async () => root.unmount())
    container.remove()
    vi.unstubAllGlobals()
  }
})

it('domain create failure surfaces the server error message, not a generic fallback', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal(
    'fetch',
    vi
      .fn()
      .mockResolvedValueOnce({ ok: true, status: 200, json: async () => [] })
      .mockResolvedValueOnce({
        ok: false,
        status: 400,
        json: async () => ({
          error: 'domain quota exceeded: maximum 2 domains per server',
        }),
      })
  )

  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  try {
    await act(async () => root.render(createElement(Domains)))
    await act(async () => {
      await Promise.resolve()
    })
    expect(
      container.textContent?.includes('No domains found'),
      'CONTROL FAILED: page did not render the empty domain list'
    ).toBe(true)
    console.log('CONTROL EXPECTED: empty domain list rendered | ACTUAL: shown')

    const addTrigger = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Add Domain'
    )
    if (!addTrigger) throw new Error('Add Domain trigger not found')
    await act(async () => addTrigger.click())
    const nameInput = document.getElementById('domain') as HTMLInputElement | null
    if (!nameInput) throw new Error('domain name input not found in dialog')
    const submit = Array.from(document.querySelectorAll('button'))
      .filter((b) => b.textContent?.trim() === 'Add Domain')
      .pop()
    if (!submit) throw new Error('dialog submit button not found')

    await act(async () => {
      setInputValue(nameInput, 'staging.example.com')
      submit.click()
      await Promise.resolve()
    })
    await act(async () => {
      await Promise.resolve()
    })

    const text = document.body.textContent ?? ''
    const serverShown = text.includes(
      'domain quota exceeded: maximum 2 domains per server'
    )
    const genericShown = text.includes('Failed to create domain')
    console.log(
      'EXPECTED: server message shown in dialog | ACTUAL: server shown:',
      serverShown,
      '| generic shown:',
      genericShown
    )
    if (!serverShown || genericShown) {
      console.log('PROBLEM CONFIRMED')
      expect(
        serverShown && !genericShown,
        'PROBLEM CONFIRMED: server error message replaced by generic fallback'
      ).toBe(true)
    }
    console.log('PROBLEM NOT REPRODUCED')
    console.log('FIX VERIFIED')
  } finally {
    await act(async () => root.unmount())
    container.remove()
    vi.unstubAllGlobals()
  }
})
