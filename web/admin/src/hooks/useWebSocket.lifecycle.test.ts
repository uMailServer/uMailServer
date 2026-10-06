import { it, expect, vi } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { useWebSocket } from './useWebSocket'

// The realtime endpoint is SSE (/api/v1/events), so the hook connects via
// EventSource; this fixture stands in for the platform EventSource the same
// way the previous FakeAuditSocket stood in for WebSocket.
class FakeAuditEventSource {
  static instances: FakeAuditEventSource[] = []
  onopen: ((event: Event) => void) | null = null
  onerror: ((event: Event) => void) | null = null
  close = vi.fn()
  private listeners = new Map<string, ((event: MessageEvent) => void)[]>()
  constructor(public url: string) {
    FakeAuditEventSource.instances.push(this)
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

it('F4737 explicit disconnect suppresses delayed reconnect', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('EventSource', FakeAuditEventSource as unknown as typeof EventSource)
  vi.useFakeTimers()
  FakeAuditEventSource.instances = []
  const metrics = vi.fn()
  let current!: ReturnType<typeof useWebSocket>
  function Probe() {
    current = useWebSocket({ reconnectInterval: 10, onMetrics: metrics })
    return null
  }
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  try {
    await act(async () => root.render(createElement(Probe)))
    const original = FakeAuditEventSource.instances[0]
    await act(async () => original.onopen?.(new Event('open')))
    expect(current.isConnected, 'CONTROL FAILED').toBe(true)
    console.log('CONTROL EXPECTED: live event source connects | ACTUAL: connected')
    const delayedError = original.onerror
    await act(async () => current.disconnect())
    expect(current.isConnected).toBe(false)
    // Release a queued error after disconnect.
    await act(async () => delayedError?.(new Event('error')))
    await act(async () => vi.advanceTimersByTime(10))
    const actual = FakeAuditEventSource.instances.length
    console.log('EXPECTED: one retired source, no reconnect | ACTUAL:', actual)
    if (actual !== 1) {
      console.log('PROBLEM CONFIRMED')
      expect(actual).toBe(1)
    }
    console.log('PROBLEM NOT REPRODUCED')
    // Stale open after disconnect must not flip the flag back on.
    await act(async () => original.onopen?.(new Event('open')))
    expect(current.isConnected).toBe(false)
    // Stale events on the retired source must not be delivered.
    await act(async () => original.emit('metrics', { fixture: true }))
    expect(metrics).not.toHaveBeenCalled()
    expect(current.lastMessage).toBeNull()
    // Explicit reconnect creates exactly one replacement.
    await act(async () => current.connect())
    const replacement = FakeAuditEventSource.instances[1]
    await act(async () => replacement.onopen?.(new Event('open')))
    expect(current.isConnected).toBe(true)
    // The retired source's queued error must not disturb the replacement.
    await act(async () => delayedError?.(new Event('error')))
    expect(current.isConnected).toBe(true)
    // sendMessage is a documented no-op (SSE is receive-only): it must not
    // throw and must not disturb the live connection.
    await act(async () => current.sendMessage({ fixture: 'current' }))
    expect(replacement.close).not.toHaveBeenCalled()
    expect(current.isConnected).toBe(true)
    // A real error on the live source schedules one bounded reconnect.
    await act(async () => replacement.onerror?.(new Event('error')))
    await act(async () => vi.advanceTimersByTime(10))
    expect(FakeAuditEventSource.instances).toHaveLength(3)
    console.log('FIX VERIFIED')
  } finally {
    await act(async () => root.unmount())
    container.remove()
    vi.useRealTimers()
    vi.unstubAllGlobals()
  }
})
