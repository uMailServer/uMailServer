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

export function useWebSocket(token: string | null, options: UseWebSocketOptions = {}) {
  const [isConnected, setIsConnected] = useState(false);
  const [lastMessage, setLastMessage] = useState<WebSocketMessage | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectCountRef = useRef(0);
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const {
    onMetrics,
    onActivity,
    onStatus,
    onHealth,
    onError,
    reconnectInterval = 5000,
    maxReconnectAttempts = 5,
  } = options;

  const connect = useCallback(() => {
    if (!token) return;

    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const wsUrl = `${protocol}//${window.location.host}/api/v1/events?token=${token}`;

    try {
      const ws = new WebSocket(wsUrl);
      wsRef.current = ws;

      ws.onopen = () => {
        setIsConnected(true);
        reconnectCountRef.current = 0;
      };

      ws.onmessage = (event) => {
        try {
          const message: WebSocketMessage = JSON.parse(event.data);
          setLastMessage(message);

          switch (message.type) {
            case "metrics":
              onMetrics?.(message.data as RealtimeMetrics);
              break;
            case "activity":
              onActivity?.(message.data as Activity);
              break;
            case "status":
              onStatus?.(message.data);
              break;
            case "health":
              onHealth?.(message.data);
              break;
          }
        } catch (err) {
          console.error("Failed to parse WebSocket message:", err);
        }
      };

      ws.onclose = () => {
        setIsConnected(false);
        wsRef.current = null;

        // Attempt to reconnect
        if (reconnectCountRef.current < maxReconnectAttempts) {
          reconnectCountRef.current++;
          reconnectTimerRef.current = setTimeout(() => {
            connect();
          }, reconnectInterval);
        }
      };

      ws.onerror = () => {
        onError?.(new Error("WebSocket error"));
      };
    } catch (err) {
      onError?.(err as Error);
    }
  }, [token, maxReconnectAttempts, reconnectInterval, onMetrics, onActivity, onStatus, onHealth, onError]);

  const disconnect = useCallback(() => {
    if (reconnectTimerRef.current) {
      clearTimeout(reconnectTimerRef.current);
    }
    if (wsRef.current) {
      wsRef.current.close();
      wsRef.current = null;
    }
    setIsConnected(false);
  }, []);

  const sendMessage = useCallback((message: object) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify(message));
    }
  }, []);

  useEffect(() => {
    if (token) {
      connect();
    }
    return () => disconnect();
  }, [token, connect, disconnect]);

  return {
    isConnected,
    lastMessage,
    sendMessage,
    connect,
    disconnect,
  };
}
