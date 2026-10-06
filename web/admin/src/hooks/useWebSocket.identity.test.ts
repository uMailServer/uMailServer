import { it, expect, vi } from 'vitest'
import { act, createElement, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { useWebSocket } from './useWebSocket'

// The realtime endpoint is SSE (/api/v1/events), so the hook connects via
// EventSource; this fixture stands in for the platform EventSource the same
// way the previous FakeSocket stood in for WebSocket.
class FakeEventSource {
  static instances: FakeEventSource[] = []
  onopen: ((event: Event) => void) | null = null
  onerror: ((event: Event) => void) | null = null
  close = vi.fn()
  private listeners = new Map<string, ((event: MessageEvent) => void)[]>()
  constructor(public url: string) {
    FakeEventSource.instances.push(this)
  }
  addEventListener(type: string, listener: (event: MessageEvent) => void) {
    const list = this.listeners.get(type) ?? []
    list.push(listener)
    this.listeners.set(type, list)
  }
  emit(type: string, data: unknown) {
    for (const listener of this.listeners.get(type) ?? []) {
      listener(new MessageEvent(type, { data: JSON.stringify(data) }))
    }
  }
}

it('F-ws-identity a consumer re-render must not re-subscribe the event source', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('EventSource', FakeEventSource as unknown as typeof EventSource)
  FakeEventSource.instances = []

  const activitySpy = vi.fn()
  let current!: ReturnType<typeof useWebSocket>
  let forceRender: () => void = () => {}

  // App.tsx passes INLINE callbacks — a new identity on every render. Every
  // state update in the consumer (e.g. activity arriving) re-renders the Probe
  // and produces a fresh callback identity, exactly like the real dashboard.
  function Probe() {
    const [, setTick] = useState(0)
    forceRender = () => setTick((t) => t + 1)
    current = useWebSocket({
      reconnectInterval: 10,
      onActivity: (activity) => activitySpy(activity),
    })
    return null
  }

  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  try {
    await act(async () => root.render(createElement(Probe)))

    // CONTROL (stable pre- and post-fix): mounting creates exactly one source.
    expect(FakeEventSource.instances.length, 'CONTROL FAILED: mount did not create one event source').toBe(1)
    console.log('CONTROL EXPECTED: one event source created on mount | ACTUAL: shown')

    // CONTRACT: after the source opens, the open-state re-render (fresh
    // callback identities) must NOT tear down and re-subscribe.
    const original = FakeEventSource.instances[0]
    await act(async () => original.onopen?.(new Event('open')))
    const count = FakeEventSource.instances.length
    const originalClosed = (original.close as ReturnType<typeof vi.fn>).mock.calls.length > 0
    console.log(
      'EXPECTED: open source survives its own state re-render | ACTUAL: instances =',
      count,
      '| original closed:',
      originalClosed,
      '| isConnected:',
      current.isConnected
    )
    if (count !== 1 || originalClosed || !current.isConnected) {
      console.log('PROBLEM CONFIRMED')
      expect(
        count === 1 && !originalClosed && current.isConnected,
        'PROBLEM CONFIRMED: re-renders tear down and re-subscribe the event source (connection churn, open never sticks)'
      ).toBe(true)
    }
    console.log('PROBLEM NOT REPRODUCED')

    // Post-fix behavior: handlers stay live across further re-renders on the
    // SAME source, and forced re-renders do not churn it.
    await act(async () =>
      original.emit('activity', { fixture: 1 })
    )
    expect(activitySpy, 'CONTROL FAILED: first activity not delivered').toHaveBeenCalledTimes(1)
    await act(async () => forceRender())
    expect(FakeEventSource.instances.length, 'PROBLEM CONFIRMED: forced re-render churned the event source').toBe(1)
    await act(async () =>
      original.emit('activity', { fixture: 2 })
    )
    expect(activitySpy).toHaveBeenCalledTimes(2)
    expect(current.isConnected).toBe(true)
    console.log('FIX VERIFIED: handlers live, single source across re-renders')
  } finally {
    await act(async () => root.unmount())
    container.remove()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  }
})
