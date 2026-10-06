import { it, expect, vi } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { Queue } from './Queue'
import type { QueueEntry } from '@/types'

interface DeferredFetch {
  resolve: (value: { ok: boolean; status: number; json: () => Promise<unknown> }) => void
}

function queueEntry(n: number): QueueEntry {
  return {
    id: String(n),
    from: `sender-${n}@example.com`,
    to: `queue-${n}@example.com`,
    status: 'pending',
    retry_count: 0,
    created_at: '2026-10-06T10:00:00Z',
  }
}

function okPayload(entries: QueueEntry[]) {
  return { ok: true, status: 200, json: async () => entries }
}

function findPaginationButtons(container: HTMLElement): HTMLButtonElement[] {
  const pageLabel = Array.from(container.querySelectorAll('p')).find((p) =>
    /Page \d+ of \d+/.test(p.textContent ?? '')
  )
  if (!pageLabel) throw new Error('pagination label not found')
  const bar = pageLabel.closest('div')
  if (!bar) throw new Error('pagination bar not found')
  const buttons = Array.from(bar.querySelectorAll('button'))
  if (buttons.length < 2) throw new Error('pagination buttons not found')
  return buttons
}

it('queue pagination clamps to the last page when the result set shrinks', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const pending: DeferredFetch[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation(
      () =>
        new Promise((resolve) => {
          pending.push({ resolve })
        })
    )
  )

  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  try {
    await act(async () => root.render(createElement(Queue)))
    expect(pending.length, 'CONTROL FAILED: initial queue fetch missing').toBe(1)

    // 25 entries -> 3 pages.
    await act(async () =>
      pending.shift()!.resolve(okPayload(Array.from({ length: 25 }, (_, i) => queueEntry(i + 1))))
    )
    expect(
      container.textContent?.includes('Page 1 of 3'),
      'CONTROL FAILED: pagination not shown for 25 entries'
    ).toBe(true)
    console.log('CONTROL EXPECTED: Page 1 of 3 | ACTUAL: shown')

    // Navigate to the last page via the real next-page button.
    const [, nextBtn] = findPaginationButtons(container)
    await act(async () => nextBtn.click())
    await act(async () => nextBtn.click())
    expect(
      container.textContent?.includes('Page 3 of 3'),
      'CONTROL FAILED: did not reach page 3'
    ).toBe(true)
    expect(
      container.textContent?.includes('queue-21@example.com'),
      'CONTROL FAILED: page 3 entries not rendered'
    ).toBe(true)
    console.log('CONTROL EXPECTED: page 3 entries (21-25) | ACTUAL: shown')

    // The result set shrinks below the stored page (manual refresh returning
    // 5 entries; the 10s polling interval triggers the same path).
    const refresh = Array.from(container.querySelectorAll('button')).find((b) =>
      b.textContent?.includes('Refresh')
    )
    if (!refresh) throw new Error('refresh button not found')
    await act(async () => refresh.click())
    expect(pending.length, 'CONTROL FAILED: refresh issued no fetch').toBe(1)
    await act(async () => pending.shift()!.resolve(okPayload(Array.from({ length: 5 }, (_, i) => queueEntry(i + 1)))))

    // CONTRACT: the list must clamp to the last existing page and render the
    // 5 remaining entries — never a phantom empty page with pagination gone.
    const text = container.textContent ?? ''
    const emptyPhantom = text.includes('No queue entries')
    const entriesShown = text.includes('queue-1@example.com')
    const contradiction = text.includes('5 messages in queue')
    console.log(
      'EXPECTED: clamped page shows 5 entries | ACTUAL: entries shown:',
      entriesShown,
      '| phantom empty state:',
      emptyPhantom,
      '| contradictory count label:',
      contradiction
    )
    if (emptyPhantom || !entriesShown) {
      console.log('PROBLEM CONFIRMED')
      expect(
        entriesShown && !emptyPhantom,
        'PROBLEM CONFIRMED: shrunken result set left the queue on a phantom empty page (pagination hidden, stats still counting entries)'
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
