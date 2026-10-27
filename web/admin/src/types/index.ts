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
  // The API serialises the recipient list as an array; older fixtures use a string.
  to: string | string[];
  status: 'pending' | 'sending' | 'failed' | 'delivered' | 'bounced';
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
  status: 'operational' | 'degraded' | 'down' | 'unknown';
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

// GET /api/v1/admin/settings: the effective, secret-free server configuration.
// Read-only: settings change in the config file and apply on restart.
export interface ListenerSetting {
  name: string;
  enabled: boolean;
  bind: string;
  port: number;
}

export interface ServerSettings {
  config_file: string;
  hostname: string;
  data_dir: string;
  listeners: ListenerSetting[];
  max_message_size: number;
  max_recipients: number;
  tls: {
    min_version: string;
    acme: boolean;
    manual_cert: boolean;
    client_auth: boolean;
  };
  security: {
    max_login_attempts: number;
    lockout_duration: string;
    submission_require_tls: boolean;
    ip_per_hour: number;
    user_per_hour: number;
    http_requests_per_minute: number;
    audit_log: boolean;
  };
  spam: {
    enabled: boolean;
    reject_threshold: number;
    junk_threshold: number;
    greylisting: boolean;
    bayesian: boolean;
    auto_train: boolean;
    rbl_servers: string[] | null;
    antivirus: boolean;
    dmarc_reports: boolean;
  };
  notifications: {
    alerts: boolean;
    alert_email: boolean;
    alert_webhook: boolean;
    push: boolean;
  };
}
