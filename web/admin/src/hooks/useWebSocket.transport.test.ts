import { it, expect, vi } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { useWebSocket } from './useWebSocket'

// Regression (admin realtime): /api/v1/events is an SSE endpoint
// (internal/websocket/sse.go — no WebSocket upgrade handling exists anywhere in
// the Go codebase; live probes against the server binary: an authenticated
// WebSocket upgrade gets 200 text/event-stream, never 101 Switching
// Protocols). The hook previously constructed new WebSocket("ws://..."), a
// handshake the server can never answer, so the admin dashboard stayed
// disconnected forever. The hook must connect via EventSource with an
// http(s) URL — the HttpOnly "jwt" cookie flows automatically same-origin —
// and must never construct a WebSocket.

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

it('F-sse-transport the realtime hook connects via EventSource to /api/v1/events and never via WebSocket', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const eventSourceCtor = vi.fn((url: string) => new FakeEventSource(url))
  const webSocketCtor = vi.fn()
  vi.stubGlobal('EventSource', eventSourceCtor as unknown as typeof EventSource)
  vi.stubGlobal('WebSocket', webSocketCtor as unknown as typeof WebSocket)

  const activitySpy = vi.fn()
  let current!: ReturnType<typeof useWebSocket>
  function Probe() {
    current = useWebSocket({ onActivity: activitySpy })
    return null
  }

  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  try {
    await act(async () => root.render(createElement(Probe)))

    // TRANSPORT CONTRACT: exactly one EventSource to the SSE endpoint; no WebSocket.
    expect(eventSourceCtor, 'the hook must connect via EventSource (the server endpoint is SSE)').toHaveBeenCalledTimes(1)
    expect(FakeEventSource.instances.length).toBe(1)
    const es = FakeEventSource.instances[0]
    expect(es.url.endsWith('/api/v1/events')).toBe(true)
    expect(es.url.startsWith('ws:') || es.url.startsWith('wss:'), `EventSource needs an http(s) URL, got ${es.url}`).toBe(false)
    expect(webSocketCtor, 'the hook must never construct a WebSocket against the SSE endpoint').not.toHaveBeenCalled()

    await act(async () => es.onopen?.(new Event('open')))
    expect(current.isConnected).toBe(true)

    // DECLARED routing: server activity events reach onActivity.
    await act(async () => es.emit('activity', { fixture: 'act-1' }))
    expect(activitySpy).toHaveBeenCalledWith({ fixture: 'act-1' })

    // SERVER vocabulary: native events land in lastMessage even though no
    // callback is declared for them (the server emits connected/heartbeat/
    // new_mail/expunge/flags_changed/folder_update today).
    await act(async () => es.emit('new_mail', { folder: 'INBOX', uid: 7 }))
    expect(current.lastMessage?.type).toBe('new_mail')

    // Cleanup on unmount.
    await act(async () => root.unmount())
    expect(es.close).toHaveBeenCalled()
  } finally {
    container.remove()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  }
})
