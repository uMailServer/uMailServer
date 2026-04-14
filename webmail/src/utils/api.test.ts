import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import API from './api'

describe('API Error Handling', () => {
  beforeEach(() => {
    // Reset API token before each test
    API.setToken(null)
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  describe('HTTP Error Responses', () => {
    it('handles 401 Unauthorized by redirecting to login', async () => {
      global.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 401,
        statusText: 'Unauthorized',
      })

      // Mock window.location.href
      const originalLocation = window.location
      // @ts-expect-error - mocking window.location
      delete window.location
      // @ts-expect-error - mocking window.location
      window.location = { href: '' }

      await API.get('/test')

      expect(window.location.href).toBe('/login')

      // Restore window.location
      window.location = originalLocation
    })

    it('throws error for 500 Internal Server Error', async () => {
      global.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
        statusText: 'Internal Server Error',
      })

      await expect(API.get('/test')).rejects.toThrow('HTTP 500')
    })

    it('throws error for 404 Not Found', async () => {
      global.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 404,
        statusText: 'Not Found',
      })

      await expect(API.get('/test')).rejects.toThrow('HTTP 404')
    })

    it('throws error for 403 Forbidden', async () => {
      global.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 403,
        statusText: 'Forbidden',
      })

      await expect(API.get('/test')).rejects.toThrow('HTTP 403')
    })

    it('throws error for 400 Bad Request', async () => {
      global.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 400,
        statusText: 'Bad Request',
      })

      await expect(API.get('/test')).rejects.toThrow('HTTP 400')
    })

    it('throws error for 429 Too Many Requests', async () => {
      global.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 429,
        statusText: 'Too Many Requests',
      })

      await expect(API.get('/test')).rejects.toThrow('HTTP 429')
    })

    it('throws error for 503 Service Unavailable', async () => {
      global.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 503,
        statusText: 'Service Unavailable',
      })

      await expect(API.get('/test')).rejects.toThrow('HTTP 503')
    })
  })

  describe('Network Errors', () => {
    it('throws error on network failure', async () => {
      global.fetch = vi.fn().mockRejectedValue(new Error('Network error'))

      await expect(API.get('/test')).rejects.toThrow('Network error')
    })

    it('throws error on timeout', async () => {
      global.fetch = vi.fn().mockRejectedValue(new Error('Timeout'))

      await expect(API.get('/test')).rejects.toThrow('Timeout')
    })

    it('throws error on DNS failure', async () => {
      global.fetch = vi.fn().mockRejectedValue(new TypeError('Failed to fetch'))

      await expect(API.get('/test')).rejects.toThrow('Failed to fetch')
    })

    it('throws error on CORS failure', async () => {
      global.fetch = vi.fn().mockRejectedValue(new TypeError('CORS error'))

      await expect(API.get('/test')).rejects.toThrow('CORS error')
    })
  })

  describe('Authentication', () => {
    it('includes Authorization header when token is set', async () => {
      API.setToken('test-token-123')

      global.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'content-type': 'application/json' }),
        json: async () => ({ data: 'test' }),
      })

      await API.get('/test')

      expect(global.fetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          headers: expect.objectContaining({
            'Authorization': 'Bearer test-token-123',
          }),
        })
      )
    })

    it('does not include Authorization header when token is null', async () => {
      API.setToken(null)

      global.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'content-type': 'application/json' }),
        json: async () => ({ data: 'test' }),
      })

      await API.get('/test')

      const callArgs = (global.fetch as ReturnType<typeof vi.fn>).mock.calls[0][1]
      expect(callArgs.headers['Authorization']).toBeUndefined()
    })
  })

  describe('Response Parsing', () => {
    it('parses JSON response correctly', async () => {
      const mockData = { emails: [{ id: '1', subject: 'Test' }] }

      global.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'content-type': 'application/json' }),
        json: async () => mockData,
      })

      const result = await API.get('/test')
      expect(result).toEqual(mockData)
    })

    it('handles empty JSON response', async () => {
      global.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'content-type': 'application/json' }),
        json: async () => ({}),
      })

      const result = await API.get('/test')
      expect(result).toEqual({})
    })

    it('handles non-JSON response', async () => {
      global.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'content-type': 'text/plain' }),
        text: async () => 'Plain text response',
      })

      const result = await API.get('/test')
      expect(result).toBe('Plain text response')
    })

    it('handles response without content-type header', async () => {
      global.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers(),
        text: async () => 'Response without content-type',
      })

      const result = await API.get('/test')
      expect(result).toBe('Response without content-type')
    })

    it('handles null response body', async () => {
      global.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 204,
        headers: new Headers(),
        text: async () => '',
      })

      const result = await API.get('/test')
      expect(result).toBe('')
    })
  })

  describe('Request Methods', () => {
    it('sends GET request correctly', async () => {
      global.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'content-type': 'application/json' }),
        json: async () => ({ data: 'get' }),
      })

      await API.get('/test')

      expect(global.fetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          method: 'GET',
        })
      )
    })

    it('sends POST request with body correctly', async () => {
      global.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'content-type': 'application/json' }),
        json: async () => ({ data: 'post' }),
      })

      const body = { name: 'test', value: 123 }
      await API.post('/test', body)

      expect(global.fetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify(body),
        })
      )
    })

    it('sends PUT request with body correctly', async () => {
      global.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'content-type': 'application/json' }),
        json: async () => ({ data: 'put' }),
      })

      const body = { name: 'updated', value: 456 }
      await API.put('/test', body)

      expect(global.fetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          method: 'PUT',
          body: JSON.stringify(body),
        })
      )
    })

    it('sends DELETE request correctly', async () => {
      global.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'content-type': 'application/json' }),
        json: async () => ({ data: 'delete' }),
      })

      await API.delete('/test')

      expect(global.fetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          method: 'DELETE',
        })
      )
    })

    it('sends POST request without body correctly', async () => {
      global.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'content-type': 'application/json' }),
        json: async () => ({ data: 'post' }),
      })

      await API.post('/test')

      expect(global.fetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          method: 'POST',
          body: undefined,
        })
      )
    })
  })

  describe('Request Headers', () => {
    it('includes default Content-Type header', async () => {
      global.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'content-type': 'application/json' }),
        json: async () => ({ data: 'test' }),
      })

      await API.get('/test')

      expect(global.fetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          headers: expect.objectContaining({
            'Content-Type': 'application/json',
          }),
        })
      )
    })

    it('allows custom headers to be passed', async () => {
      global.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'content-type': 'application/json' }),
        json: async () => ({ data: 'test' }),
      })

      await API.request('/test', {
        headers: { 'X-Custom-Header': 'custom-value' },
      })

      expect(global.fetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          headers: expect.objectContaining({
            'Content-Type': 'application/json',
            'X-Custom-Header': 'custom-value',
          }),
        })
      )
    })

    it('includes credentials: include for cookie handling', async () => {
      global.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'content-type': 'application/json' }),
        json: async () => ({ data: 'test' }),
      })

      await API.get('/test')

      expect(global.fetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          credentials: 'include',
        })
      )
    })
  })

  describe('Console Error Logging', () => {
    it('logs API errors to console', async () => {
      const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {})

      global.fetch = vi.fn().mockRejectedValue(new Error('Network error'))

      try {
        await API.get('/test')
      } catch {
        // Expected to throw
      }

      expect(consoleSpy).toHaveBeenCalledWith('API error:', expect.any(Error))

      consoleSpy.mockRestore()
    })
  })

  describe('API Specific Endpoints', () => {
    it('login endpoint constructs correct request', async () => {
      global.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'content-type': 'application/json' }),
        json: async () => ({ token: 'new-token' }),
      })

      await API.login({ email: 'user@test.com', password: 'pass123' })

      expect(global.fetch).toHaveBeenCalledWith(
        expect.stringContaining('/auth/login'),
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({ email: 'user@test.com', password: 'pass123' }),
        })
      )
    })

    it('getMail endpoint constructs correct URL', async () => {
      global.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'content-type': 'application/json' }),
        json: async () => ({ emails: [] }),
      })

      await API.getMail('inbox')

      expect(global.fetch).toHaveBeenCalledWith(
        expect.stringContaining('/mail/inbox'),
        expect.objectContaining({
          method: 'GET',
        })
      )
    })

    it('search endpoint encodes query parameter', async () => {
      global.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'content-type': 'application/json' }),
        json: async () => ({ emails: [], total: 0, query: '' }),
      })

      await API.search('hello world')

      expect(global.fetch).toHaveBeenCalledWith(
        expect.stringContaining('/search?q=hello%20world'),
        expect.any(Object)
      )
    })

    it('deleteMail endpoint constructs correct URL with query parameter', async () => {
      global.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers(),
        text: async () => '',
      })

      await API.deleteMail('msg-123')

      expect(global.fetch).toHaveBeenCalledWith(
        expect.stringContaining('/mail/delete?id=msg-123'),
        expect.objectContaining({
          method: 'DELETE',
        })
      )
    })
  })
})
