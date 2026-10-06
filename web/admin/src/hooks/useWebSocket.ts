import { useEffect, useRef, useState, useCallback } from "react";
import type { RealtimeMetrics, Activity } from "@/types";

// The server's realtime endpoint (/api/v1/events) is Server-Sent Events
// (internal/websocket/sse.go): the client receives named events
// (connected, heartbeat, new_mail, expunge, flags_changed, folder_update,
// plus the metrics/activity/status/health/error vocabulary below) and has
// no send channel. Authentication is the HttpOnly "jwt" cookie, which
// EventSource sends automatically for same-origin requests — browsers
// cannot set custom headers on EventSource, and the server never performs
// a WebSocket upgrade, which is why the previous WebSocket client could
// never connect. The hook keeps its historical name only for import
// stability.

interface ServerEvent {
  type: string;
  data: unknown;
  timestamp: number;
}

interface UseWebSocketOptions {
  onMetrics?: (metrics: RealtimeMetrics) => void;
  onActivity?: (activity: Activity) => void;
  onStatus?: (status: unknown) => void;
  onHealth?: (health: unknown) => void;
  onError?: (error: Error) => void;
  reconnectInterval?: number;
  maxReconnectAttempts?: number;
}

// Event names the hook listens for: the declared callback vocabulary plus
// the events the Go server actually emits today.
const EVENT_NAMES = [
  "metrics",
  "activity",
  "status",
  "health",
  "connected",
  "error",
  "heartbeat",
  "new_mail",
  "expunge",
  "flags_changed",
  "folder_update",
] as const;

export function useWebSocket(options: UseWebSocketOptions = {}) {
  const [isConnected, setIsConnected] = useState(false);
  const [lastMessage, setLastMessage] = useState<ServerEvent | null>(null);
  const sourceRef = useRef<EventSource | null>(null);
  const reconnectCountRef = useRef(0);
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Keep the latest options in a ref: consumers (App.tsx) pass inline
  // callbacks, so callback identity changes on every render. The event
  // source must survive those re-renders — listeners always read the
  // freshest callbacks without tearing down and re-subscribing.
  const optionsRef = useRef(options);
  useEffect(() => {
    optionsRef.current = options;
  });

  const connect = useCallback(() => {
    const { reconnectInterval: interval = 5000 } = optionsRef.current;
    // SSE endpoint; auth is the HttpOnly cookie handled server-side.
    const url = `${window.location.protocol}//${window.location.host}/api/v1/events`;

    try {
      const source = new EventSource(url);
      sourceRef.current = source;

      source.onopen = () => {
        if (sourceRef.current !== source) return;
        setIsConnected(true);
        reconnectCountRef.current = 0;
      };

      const handleEvent = (type: string) => (event: Event) => {
        if (sourceRef.current !== source) return;
        try {
          const message = event as MessageEvent;
          const data: unknown = JSON.parse(message.data);
          setLastMessage({ type, data, timestamp: Date.now() });

          switch (type) {
            case "metrics":
              optionsRef.current.onMetrics?.(data as RealtimeMetrics);
              break;
            case "activity":
              optionsRef.current.onActivity?.(data as Activity);
              break;
            case "status":
              optionsRef.current.onStatus?.(data);
              break;
            case "health":
              optionsRef.current.onHealth?.(data);
              break;
            case "error":
              optionsRef.current.onError?.(new Error("Server reported an error event"));
              break;
          }
        } catch (err) {
          console.error("Failed to parse SSE message:", err);
        }
      };
      for (const name of EVENT_NAMES) {
        source.addEventListener(name, handleEvent(name));
      }

      source.onerror = () => {
        if (sourceRef.current !== source) return;
        setIsConnected(false);
        source.close();
        sourceRef.current = null;

        // Attempt to reconnect
        const { maxReconnectAttempts: maxAttempts = 5 } = optionsRef.current;
        if (reconnectCountRef.current < maxAttempts) {
          reconnectCountRef.current++;
          reconnectTimerRef.current = setTimeout(() => {
            connect();
          }, interval);
        }
      };
    } catch (err) {
      optionsRef.current.onError?.(err as Error);
    }
  }, []);

  const disconnect = useCallback(() => {
    if (reconnectTimerRef.current) {
      clearTimeout(reconnectTimerRef.current);
      reconnectTimerRef.current = null;
    }
    const source = sourceRef.current;
    sourceRef.current = null;
    source?.close();
    setIsConnected(false);
  }, []);

  // SSE is receive-only and /api/v1/events has no client-to-server channel.
  // Kept for API stability: intentionally a no-op (it was already one in
  // practice — the previous WebSocket transport never connected).
  const sendMessage = useCallback((_message: object) => {
    void _message;
  }, []);

  useEffect(() => {
    connect();
    return () => disconnect();
  }, [connect, disconnect]);

  return {
    isConnected,
    lastMessage,
    sendMessage,
    connect,
    disconnect,
  };
}
