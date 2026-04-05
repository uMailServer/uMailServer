export interface User {
  email: string;
  isAdmin: boolean;
}

export interface Domain {
  name: string;
  max_accounts: number;
  is_active: boolean;
  created_at: string;
  updated_at: string;
  dkim_selector?: string;
  dkim_public_key?: string;
}

export interface Account {
  email: string;
  is_admin: boolean;
  is_active: boolean;
  quota_used: number;
  quota_limit: number;
  forward_to?: string;
  forward_keep_copy?: boolean;
  created_at: string;
  updated_at: string;
  last_login?: string;
  vacation_settings?: string;
  totp_enabled?: boolean;
}

export interface QueueEntry {
  id: string;
  from: string;
  to: string;
  status: 'pending' | 'sending' | 'failed' | 'delivered';
  retry_count: number;
  last_error?: string;
  created_at: string;
  next_retry?: string;
}

export interface ServerStats {
  domains: number;
  accounts: number;
  messages: number;
  queue_size: number;
}

export interface HealthStatus {
  status: 'healthy' | 'unhealthy' | 'warning';
  database?: string;
  queue?: string;
  storage?: string;
}

export interface ServiceStatus {
  name: string;
  status: 'operational' | 'degraded' | 'down';
  port?: number;
  latency?: number;
}

export interface RealtimeMetrics {
  timestamp: number;
  cpu_usage: number;
  memory_usage: number;
  disk_usage: number;
  network_in: number;
  network_out: number;
  smtp_connections: number;
  imap_connections: number;
  messages_sent: number;
  messages_received: number;
}

export interface Activity {
  id: string;
  type: 'message' | 'account' | 'domain' | 'queue' | 'system';
  message: string;
  details?: string;
  timestamp: string;
  severity?: 'info' | 'warning' | 'error' | 'success';
}
