import { it, expect, vi } from 'vitest'
import { act, createElement, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { useWebSocket } from './useWebSocket'

class FakeSocket {
  static OPEN = 1
  static instances: FakeSocket[] = []
  readyState = FakeSocket.OPEN
  onopen: ((event: Event) => void) | null = null
  onclose: ((event: CloseEvent) => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null
  onerror: ((event: Event) => void) | null = null
  close = vi.fn()
  send = vi.fn()
  constructor(public url: string) {
    FakeSocket.instances.push(this)
  }
}

it('F-ws-identity a consumer re-render must not re-subscribe the socket', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('WebSocket', FakeSocket)
  FakeSocket.instances = []

  const activitySpy = vi.fn()
  let current!: ReturnType<typeof useWebSocket>
  let forceRender: () => void = () => {}

  // App.tsx passes INLINE callbacks — a new identity on every render. Every
  // state update in the consumer (e.g. metrics arriving) re-renders the Probe
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

    // CONTROL (stable pre- and post-fix): mounting creates exactly one socket.
    expect(FakeSocket.instances.length, 'CONTROL FAILED: mount did not create one socket').toBe(1)
    console.log('CONTROL EXPECTED: one socket created on mount | ACTUAL: shown')

    // CONTRACT: after the socket opens, the open-state re-render (fresh
    // callback identities) must NOT tear down and re-subscribe the socket.
    const original = FakeSocket.instances[0]
    await act(async () => original.onopen?.(new Event('open')))
    const count = FakeSocket.instances.length
    const originalClosed = (original.close as ReturnType<typeof vi.fn>).mock.calls.length > 0
    console.log(
      'EXPECTED: open socket survives its own state re-render | ACTUAL: instances =',
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
        'PROBLEM CONFIRMED: re-renders tear down and re-subscribe the socket (connection churn, open never sticks)'
      ).toBe(true)
    }
    console.log('PROBLEM NOT REPRODUCED')

    // Post-fix behavior: handlers stay live across further re-renders on the
    // SAME socket, and forced re-renders do not churn it.
    await act(async () =>
      original.onmessage?.(
        new MessageEvent('message', {
          data: JSON.stringify({ type: 'activity', data: { fixture: 1 }, timestamp: 0 }),
        })
      )
    )
    expect(activitySpy, 'CONTROL FAILED: first activity not delivered').toHaveBeenCalledTimes(1)
    await act(async () => forceRender())
    expect(FakeSocket.instances.length, 'PROBLEM CONFIRMED: forced re-render churned the socket').toBe(1)
    await act(async () =>
      original.onmessage?.(
        new MessageEvent('message', {
          data: JSON.stringify({ type: 'activity', data: { fixture: 2 }, timestamp: 1 }),
        })
      )
    )
    expect(activitySpy).toHaveBeenCalledTimes(2)
    expect(current.isConnected).toBe(true)
    console.log('FIX VERIFIED: handlers live, single socket across re-renders')
  } finally {
    await act(async () => root.unmount())
    container.remove()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  }
})
