import { useEffect, useRef, useState, useCallback } from "react";
import type { RealtimeMetrics, Activity } from "@/types";

interface WebSocketMessage {
  type: "metrics" | "activity" | "status" | "health" | "connected" | "error";
  data: RealtimeMetrics | Activity | unknown;
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

export function useWebSocket(options: UseWebSocketOptions = {}) {
  const [isConnected, setIsConnected] = useState(false);
  const [lastMessage, setLastMessage] = useState<WebSocketMessage | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectCountRef = useRef(0);
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const {
    reconnectInterval = 5000,
    maxReconnectAttempts = 5,
  } = options;

  // Keep the latest options in a ref: consumers (App.tsx) pass inline
  // callbacks, so callback identity changes on every render. The socket must
  // survive those re-renders — handlers always read the freshest callbacks
  // without tearing down and re-subscribing.
  const optionsRef = useRef(options);
  useEffect(() => {
    optionsRef.current = options;
  });

  const connect = useCallback(() => {
    const {
      onMetrics,
      onActivity,
      onStatus,
      onHealth,
      onError,
      reconnectInterval: interval = 5000,
    } = optionsRef.current;
    // SSE endpoint uses HttpOnly cookie for auth on the server side
    // The browser automatically sends cookies with requests to the same origin
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const wsUrl = `${protocol}//${window.location.host}/api/v1/events`;

    try {
      const ws = new WebSocket(wsUrl);
      wsRef.current = ws;

      ws.onopen = () => {
        if (wsRef.current !== ws) return;
        setIsConnected(true);
        reconnectCountRef.current = 0;
        // Auth is handled via HttpOnly cookie on the server side
        // No token needed in message body
      };

      ws.onmessage = (event) => {
        if (wsRef.current !== ws) return;
        try {
          const message: WebSocketMessage = JSON.parse(event.data);
          setLastMessage(message);

          switch (message.type) {
            case "metrics":
              optionsRef.current.onMetrics?.(message.data as RealtimeMetrics);
              break;
            case "activity":
              optionsRef.current.onActivity?.(message.data as Activity);
              break;
            case "status":
              optionsRef.current.onStatus?.(message.data);
              break;
            case "health":
              optionsRef.current.onHealth?.(message.data);
              break;
          }
        } catch (err) {
          console.error("Failed to parse WebSocket message:", err);
        }
      };

      ws.onclose = () => {
        if (wsRef.current !== ws) return;
        setIsConnected(false);
        wsRef.current = null;

        // Attempt to reconnect
        const { maxReconnectAttempts: maxAttempts = 5, reconnectInterval: retryInterval = 5000 } =
          optionsRef.current;
        if (reconnectCountRef.current < maxAttempts) {
          reconnectCountRef.current++;
          reconnectTimerRef.current = setTimeout(() => {
            connect();
          }, interval);
        }
      };

      ws.onerror = () => {
        if (wsRef.current !== ws) return;
        optionsRef.current.onError?.(new Error("WebSocket error"));
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
    const ws = wsRef.current;
    wsRef.current = null;
    ws?.close();
    setIsConnected(false);
  }, []);

  const sendMessage = useCallback((message: object) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify(message));
    }
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
