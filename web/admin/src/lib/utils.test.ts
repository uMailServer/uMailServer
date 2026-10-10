import { describe, it, expect } from 'vitest'
import { cn } from './utils'

describe('cn (class name merger)', () => {
  it('merges class names correctly', () => {
    const result = cn('foo', 'bar')
    expect(result).toBe('foo bar')
  })

  it('handles empty strings', () => {
    const result = cn('foo', '', 'bar')
    expect(result).toBe('foo bar')
  })

  it('merges multiple class names', () => {
    const result = cn('a', 'b', 'c')
    expect(result).toBe('a b c')
  })

  it('handles conditional classes', () => {
    const isActive = true
    const result = cn('base', isActive && 'active')
    expect(result).toContain('base')
    expect(result).toContain('active')
  })
})

describe('F6223/F6224 API shape helpers', () => {
  it('formatRecipients joins the array the Go API returns', async () => {
    const { formatRecipients } = await import('./utils')
    expect(formatRecipients(['a@x.com', 'b@y.com'])).toBe('a@x.com, b@y.com')
    expect(formatRecipients('a@x.com')).toBe('a@x.com')
    expect(formatRecipients(undefined)).toBe('')
  })
  it('hasLoggedIn treats Go zero time as never', async () => {
    const { hasLoggedIn } = await import('./utils')
    expect(hasLoggedIn('0001-01-01T00:00:00Z')).toBe(false)
    expect(hasLoggedIn(undefined)).toBe(false)
    expect(hasLoggedIn('2026-10-01T12:00:00Z')).toBe(true)
  })
})
