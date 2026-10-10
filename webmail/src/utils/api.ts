const API_URL = window.location.origin + '/api/v1'

// ============================================================================
// Type Definitions
// ============================================================================

export interface Mail {
  id: string
  from: string
  fromName: string
  to: string[]
  subject: string
  body: string
  preview: string
  date: string
  read: boolean
  starred: boolean
  folder: string
  hasAttachments: boolean
  size: number
}

export interface SendMailRequest {
  to: string[]
  cc?: string[]
  bcc?: string[]
  subject: string
  body: string
}

export interface AuthLoginRequest {
  email: string
  password: string
  totp_code?: string
}

export interface AuthLoginResponse {
  expiresIn?: number
}

// Filter shapes mirror internal/api/filters.go (operators and action types are
// camelCase there; the server rejects nothing but never matches unknown values).
export interface Filter {
  id: string
  name: string
  enabled: boolean
  matchAll: boolean
  conditions: FilterCondition[]
  actions: FilterAction[]
  priority: number
}

export interface FilterCondition {
  field: 'from' | 'to' | 'subject' | 'body' | 'header'
  operator: 'contains' | 'equals' | 'startsWith' | 'endsWith' | 'matches'
  value: string
  headerName?: string
}

export interface FilterAction {
  type: 'move' | 'copy' | 'delete' | 'markRead' | 'markSpam' | 'forward' | 'flag'
  target?: string
  forwardTo?: string
}

// Mirrors internal/api/vacation.go: snake_case keys, `message` (not body), and
// a send_interval in hours that the server requires to be > 0.
export interface VacationAutoReply {
  enabled: boolean
  subject: string
  message: string
  start_date?: string
  end_date?: string
  html_message?: string
  send_interval: number
  exclude_addresses?: string[]
  ignore_lists?: boolean
  ignore_bulk?: boolean
}

// Shape produced by the browser's PushSubscription.toJSON().
export interface PushSubscription {
  endpoint: string
  keys: {
    p256dh: string
    auth: string
  }
}

// What GET /push/subscriptions returns (keys are deliberately not exposed).
export interface PushSubscriptionInfo {
  id: string
  createdAt: string
  updatedAt: string
  deviceInfo?: unknown
}

export interface SearchResponse {
  emails: Mail[]
  total: number
  query: string
}

export interface ThreadsResponse {
  threads: Thread[]
  total: number
  limit: number
  offset: number
}

// internal/api/threads.go: list items use snake_case.
export interface Thread {
  thread_id: string
  subject: string
  participants: string[]
  message_count: number
  unread_count: number
  last_activity: string
  created_at: string
}

export interface ThreadMessage {
  message_id: string
  uid: number
  mailbox: string
  from: string
  to: string
  subject: string
  date: string
  is_read: boolean
  flags: string[]
}

// ============================================================================
// API Client
// ============================================================================

interface RequestOptions extends RequestInit {
  headers?: Record<string, string>
}

interface ApiResponse<T = unknown> {
  data?: T
  [key: string]: unknown
}

// Carries the HTTP status and the server's {"error": "..."} message so callers
// can distinguish e.g. "TOTP code required" from "invalid credentials" instead
// of seeing an opaque "HTTP 401" for every failure.
export class ApiError extends Error {
  status: number
  serverMessage?: string

  constructor(status: number, serverMessage?: string) {
    super(`HTTP ${status}`)
    this.name = 'ApiError'
    this.status = status
    this.serverMessage = serverMessage
  }
}

async function readServerError(response: Response): Promise<string | undefined> {
  try {
    const contentType = response.headers?.get?.('content-type')
    if (contentType && contentType.includes('application/json')) {
      const body = (await response.json()) as { error?: unknown }
      return typeof body?.error === 'string' ? body.error : undefined
    }
  } catch {
    // Non-JSON or unreadable error body: fall back to the status only.
  }
  return undefined
}

class API {
  private token: string | null

  constructor() {
    // Token is now stored in HttpOnly cookie by the server
    // No need to read from localStorage (more secure against XSS)
    this.token = null
  }

  setToken(token: string | null): void {
    this.token = token
  }

  async request<T = unknown>(endpoint: string, options: RequestOptions = {}): Promise<T> {
    const url = API_URL + endpoint

    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      ...options.headers
    }

    // Token is sent automatically via HttpOnly cookie
    // No need to set Authorization header for web clients
    // For API clients that still use Bearer token, we keep the header support
    if (this.token) {
      headers['Authorization'] = `Bearer ${this.token}`
    }

    try {
      const response = await fetch(url, {
        ...options,
        headers,
        credentials: 'include' // Send HttpOnly cookies with requests
      })

      if (!response.ok) {
        if (response.status === 401) {
          // Token is managed by HttpOnly cookie, server will clear it on logout
          // Already on the login page: surface the error instead of reloading.
          if (window.location.pathname === '/login') {
            throw new ApiError(401, await readServerError(response))
          }
          window.location.href = '/login'
          return null as T
        }
        throw new ApiError(response.status, await readServerError(response))
      }

      const contentType = response.headers.get('content-type')
      if (contentType && contentType.includes('application/json')) {
        return await response.json() as T
      }
      return await response.text() as unknown as T
    } catch (error) {
      console.error('API error:', error)
      throw error
    }
  }

  // Auth
  async login(credentials: AuthLoginRequest): Promise<AuthLoginResponse> {
    return this.post<AuthLoginResponse>('/auth/login', credentials)
  }

  // Mail
  async getMail(folder: string): Promise<{ emails?: Mail[] }> {
    return this.get<{ emails?: Mail[] }>(`/mail/${encodeURIComponent(folder)}`)
  }

  async sendMail(mail: SendMailRequest): Promise<void> {
    await this.post('/mail/send', mail)
  }

  async deleteMail(id: string): Promise<void> {
    await this.delete(`/mail/delete?id=${encodeURIComponent(id)}`)
  }

  // Filters
  async getFilters(): Promise<{ filters?: Filter[] }> {
    return this.get<{ filters?: Filter[] }>('/filters')
  }

  // The server answers create/update with the bare filter object, not {filter}.
  async createFilter(filter: Omit<Filter, 'id' | 'priority'>): Promise<Filter> {
    return this.post<Filter>('/filters', filter)
  }

  async updateFilter(id: string, filter: Partial<Filter>): Promise<Filter> {
    return this.put<Filter>(`/filters/${encodeURIComponent(id)}`, filter)
  }

  async deleteFilter(id: string): Promise<void> {
    await this.delete(`/filters/${encodeURIComponent(id)}`)
  }

  // Vacation/Auto-reply
  async getVacation(): Promise<VacationAutoReply> {
    return this.get<VacationAutoReply>('/vacation')
  }

  // handleVacation routes GET/PUT/DELETE; POST is rejected with 405.
  async setVacation(vacation: VacationAutoReply): Promise<void> {
    await this.put('/vacation', vacation)
  }

  async deleteVacation(): Promise<void> {
    await this.delete('/vacation')
  }

  // Search
  async search(query: string): Promise<SearchResponse> {
    return this.get<SearchResponse>(`/search?q=${encodeURIComponent(query)}`)
  }

  // Threads
  async getThreads(): Promise<ThreadsResponse> {
    return this.get<ThreadsResponse>('/threads')
  }

  // The server answers with the bare message array of the thread.
  async getThread(id: string): Promise<ThreadMessage[]> {
    return this.get<ThreadMessage[]>(`/threads/${encodeURIComponent(id)}`)
  }

  // Push notifications
  async getVapidPublicKey(): Promise<{ publicKey?: string }> {
    return this.get<{ publicKey?: string }>('/push/vapid-public-key')
  }

  // The server expects flat p256dh/auth fields, not the browser's nested keys.
  async subscribePush(subscription: PushSubscription): Promise<void> {
    await this.post('/push/subscribe', {
      endpoint: subscription.endpoint,
      p256dh: subscription.keys.p256dh,
      auth: subscription.keys.auth,
    })
  }

  // DELETE /push/unsubscribe reads the endpoint from the JSON body (or ?id=).
  async unsubscribePush(endpoint: string): Promise<void> {
    await this.delete('/push/unsubscribe', { endpoint })
  }

  async getPushSubscriptions(): Promise<{ subscriptions?: PushSubscriptionInfo[] }> {
    return this.get<{ subscriptions?: PushSubscriptionInfo[] }>('/push/subscriptions')
  }

  // Generic methods
  get<T = ApiResponse>(endpoint: string): Promise<T> {
    return this.request<T>(endpoint, { method: 'GET' })
  }

  post<T = unknown>(endpoint: string, data?: unknown): Promise<T> {
    return this.request<T>(endpoint, {
      method: 'POST',
      body: data !== undefined ? JSON.stringify(data) : undefined
    })
  }

  put<T = unknown>(endpoint: string, data?: unknown): Promise<T> {
    return this.request<T>(endpoint, {
      method: 'PUT',
      body: data !== undefined ? JSON.stringify(data) : undefined
    })
  }

  delete<T = ApiResponse>(endpoint: string, data?: unknown): Promise<T> {
    return this.request<T>(endpoint, {
      method: 'DELETE',
      body: data !== undefined ? JSON.stringify(data) : undefined
    })
  }
}

export default new API()
