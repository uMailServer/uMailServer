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
}

export interface AuthLoginResponse {
  token?: string
}

export interface Filter {
  id: string
  name: string
  conditions: FilterCondition[]
  actions: FilterAction[]
  enabled: boolean
  priority: number
}

export interface FilterCondition {
  field: 'from' | 'to' | 'subject' | 'body' | 'header'
  operator: 'contains' | 'equals' | 'starts_with' | 'ends_with' | 'exists' | 'not_exists'
  value: string
  headerName?: string
}

export interface FilterAction {
  type: 'move' | 'copy' | 'label' | 'star' | 'mark_read' | 'forward' | 'reject' | 'discard'
  destination?: string
  label?: string
}

export interface VacationAutoReply {
  enabled: boolean
  subject: string
  body: string
  startDate?: string
  endDate?: string
  contactsOnly: boolean
}

export interface PushSubscription {
  endpoint: string
  keys: {
    p256dh: string
    auth: string
  }
}

export interface SearchResponse {
  emails: Mail[]
  total: number
  query: string
}

export interface ThreadsResponse {
  threads: Thread[]
}

export interface Thread {
  id: string
  subject: string
  emails: Mail[]
  participants: string[]
  lastDate: string
  unread: boolean
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
          window.location.href = '/login'
          return null as T
        }
        throw new Error(`HTTP ${response.status}`)
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
    return this.get<{ emails?: Mail[] }>(`/mail/${folder}`)
  }

  async sendMail(mail: SendMailRequest): Promise<void> {
    await this.post('/mail/send', mail)
  }

  async deleteMail(id: string): Promise<void> {
    await this.delete(`/mail/delete?id=${id}`)
  }

  // Filters
  async getFilters(): Promise<{ filters?: Filter[] }> {
    return this.get<{ filters?: Filter[] }>('/filters')
  }

  async createFilter(filter: Omit<Filter, 'id'>): Promise<{ filter?: Filter }> {
    return this.post<{ filter?: Filter }>('/filters', filter)
  }

  async updateFilter(id: string, filter: Partial<Filter>): Promise<{ filter?: Filter }> {
    return this.put<{ filter?: Filter }>(`/filters/${id}`, filter)
  }

  async deleteFilter(id: string): Promise<void> {
    await this.delete(`/filters/${id}`)
  }

  // Vacation/Auto-reply
  async getVacation(): Promise<VacationAutoReply> {
    return this.get<VacationAutoReply>('/vacation')
  }

  async setVacation(vacation: VacationAutoReply): Promise<void> {
    await this.post('/vacation', vacation)
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

  async getThread(id: string): Promise<{ thread?: Thread }> {
    return this.get<{ thread?: Thread }>(`/threads/${id}`)
  }

  // Push notifications
  async getVapidPublicKey(): Promise<{ key?: string }> {
    return this.get<{ key?: string }>('/push/vapid-public-key')
  }

  async subscribePush(subscription: PushSubscription): Promise<void> {
    await this.post('/push/subscribe', subscription)
  }

  async unsubscribePush(endpoint: string): Promise<void> {
    await this.delete(`/push/unsubscribe?endpoint=${encodeURIComponent(endpoint)}`)
  }

  async getPushSubscriptions(): Promise<{ subscriptions?: PushSubscription[] }> {
    return this.get<{ subscriptions?: PushSubscription[] }>('/push/subscriptions')
  }

  // Generic methods
  get<T = ApiResponse>(endpoint: string): Promise<T> {
    return this.request<T>(endpoint, { method: 'GET' })
  }

  post<T = unknown>(endpoint: string, data?: unknown): Promise<T> {
    return this.request<T>(endpoint, {
      method: 'POST',
      body: data ? JSON.stringify(data) : undefined
    })
  }

  put<T = unknown>(endpoint: string, data?: unknown): Promise<T> {
    return this.request<T>(endpoint, {
      method: 'PUT',
      body: data ? JSON.stringify(data) : undefined
    })
  }

  delete<T = ApiResponse>(endpoint: string): Promise<T> {
    return this.request<T>(endpoint, { method: 'DELETE' })
  }
}

export default new API()
