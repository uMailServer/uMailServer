import { it, expect, vi } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, useNavigate } from 'react-router-dom'
import { SearchPage } from './search'
import API from '@/utils/api'

interface DeferredSearch {
  q: string
  resolve: (value: { emails: unknown[]; total: number }) => void
  reject: (error: Error) => void
}

function setInputValue(input: HTMLInputElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(
    HTMLInputElement.prototype,
    'value'
  )?.set
  if (!setter) throw new Error('input value setter unavailable')
  setter.call(input, value)
  input.dispatchEvent(new Event('input', { bubbles: true }))
}

function submitForm(container: HTMLElement) {
  const form = container.querySelector('form')
  if (!form) throw new Error('search form not found')
  form.dispatchEvent(
    new Event('submit', { bubbles: true, cancelable: true })
  )
}

function searchEmail(id: string, subject: string) {
  return {
    id,
    from: `sender-${id}`,
    fromName: `sender-${id}`,
    subject,
    preview: `${subject} body`,
    date: '10:00',
    folder: 'inbox',
    read: false,
  }
}

it('F-race older search completion cannot replace the latest query results', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const requests: DeferredSearch[] = []
  vi.spyOn(API, 'search').mockImplementation(
    (q) =>
      new Promise((resolve, reject) => {
        requests.push({ q, resolve, reject })
      })
  )

  let navigate: (to: string) => void = () => {}
  function NavProbe() {
    navigate = useNavigate()
    return null
  }

  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  try {
    await act(async () =>
      root.render(
        createElement(
          MemoryRouter,
          { initialEntries: ['/search'] },
          createElement(NavProbe),
          createElement(SearchPage)
        )
      )
    )
    expect(
      container.textContent?.includes('Search your mail'),
      'CONTROL FAILED: initial search page did not render'
    ).toBe(true)

    const input = container.querySelector('input')
    if (!input) throw new Error('search input not found')

    // Search 1 for AAA — resolves normally, control of the wired path.
    await act(async () => {
      setInputValue(input, 'AAA')
      submitForm(container)
    })
    expect(requests.length, 'CONTROL FAILED: first submit issued no search').toBe(1)
    expect(requests[0].q, 'CONTROL FAILED: first search has wrong query').toBe('AAA')
    await act(async () =>
      requests[0].resolve({
        emails: [searchEmail('id-a1', 'AAA unique subject line')],
        total: 1,
      })
    )
    expect(
      container.textContent?.includes('AAA unique subject line'),
      'CONTROL FAILED: AAA results not shown after resolving search 1'
    ).toBe(true)
    console.log('CONTROL EXPECTED: AAA results visible | ACTUAL: shown')

    // Search 2 for BBB — kept pending (slow query), so it is in flight while
    // the next trigger arrives.
    await act(async () => {
      setInputValue(input, 'BBB')
      submitForm(container)
    })
    expect(requests.length).toBe(2)
    expect(requests[1].q).toBe('BBB')

    // Overlap trigger: navigate to /search?q=CCC while search 2 is in
    // flight — the URL-param effect has no loading gate, so search 3 starts.
    await act(async () => navigate('/search?q=CCC'))
    expect(requests.length, 'CONTROL FAILED: navigation issued no search').toBe(3)
    expect(requests[2].q).toBe('CCC')

    // Search 3 (CCC, the latest trigger) completes first.
    await act(async () =>
      requests[2].resolve({
        emails: [searchEmail('id-c1', 'CCC unique subject line')],
        total: 1,
      })
    )
    expect(
      container.textContent?.includes('CCC unique subject line'),
      'CONTROL FAILED: CCC results not shown after the latest search resolved'
    ).toBe(true)
    console.log('CONTROL EXPECTED: latest (CCC) results visible | ACTUAL: shown')

    // The stale search 2 (BBB, issued BEFORE search 3) completes last.
    await act(async () =>
      requests[1].resolve({
        emails: [searchEmail('id-b1', 'BBB unique subject line')],
        total: 1,
      })
    )

    // CONTRACT: results must still reflect the latest submitted query (CCC).
    // Basis: EmailContext.loadEmails guards with requestIdRef — "older folder
    // completion cannot replace current contents" (EmailContext.race.test.ts).
    const stillCcc = container.textContent?.includes('CCC unique subject line')
    const staleBbb = container.textContent?.includes('BBB unique subject line')
    console.log(
      'EXPECTED: CCC results retained after stale BBB completion | ACTUAL:',
      stillCcc ? 'CCC retained' : 'CCC lost',
      '| stale BBB shown:',
      staleBbb
    )
    if (!stillCcc || staleBbb) {
      console.log('PROBLEM CONFIRMED')
      expect(
        stillCcc && !staleBbb,
        'PROBLEM CONFIRMED: stale search response replaced the latest query results'
      ).toBe(true)
    }
    console.log('PROBLEM NOT REPRODUCED')

    // Secondary branch: a stale FAILURE must not clear fresh results either.
    await act(async () => {
      setInputValue(input, 'EEE')
      submitForm(container)
    })
    expect(requests.length).toBe(4)
    await act(async () => {
      setInputValue(input, 'FFF')
      submitForm(container)
    })
    expect(requests.length).toBe(5)
    await act(async () =>
      requests[4].resolve({
        emails: [searchEmail('id-f1', 'FFF unique subject line')],
        total: 1,
      })
    )
    expect(
      container.textContent?.includes('FFF unique subject line'),
      'CONTROL FAILED: FFF results not shown'
    ).toBe(true)
    await act(async () => requests[3].reject(new Error('stale failure')))
    const fffRetained = container.textContent?.includes('FFF unique subject line')
    const errorShown = container.textContent?.includes('Search failed')
    console.log(
      'EXPECTED: stale failure leaves FFF results and no error | ACTUAL:',
      fffRetained ? 'FFF retained' : 'FFF wiped',
      '| error banner:',
      errorShown
    )
    expect(
      fffRetained && !errorShown,
      'PROBLEM CONFIRMED: stale failed search cleared fresh results'
    ).toBe(true)
    console.log('FIX VERIFIED')
  } finally {
    await act(async () => root.unmount())
    container.remove()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  }
})
