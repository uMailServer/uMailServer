import { describe, it, expect } from 'vitest'
import { formatDate, formatFullDate } from './date'

describe('formatDate', () => {
  it('returns time for dates less than 24 hours old', () => {
    const now = new Date()
    const oneHourAgo = new Date(now.getTime() - 3600000).toISOString()
    const result = formatDate(oneHourAgo)
    // Should return a time string with colon
    expect(result).toMatch(/^\d{1,2}:\d{2}/)
  })

  it('returns short string for dates less than 7 days old', () => {
    const now = new Date()
    const twoDaysAgo = new Date(now.getTime() - 2 * 86400000).toISOString()
    const result = formatDate(twoDaysAgo)
    // Should return a short string (weekday name)
    expect(result.length).toBeLessThanOrEqual(4)
  })

  it('returns month and day for dates older than 7 days', () => {
    const tenDaysAgo = new Date(Date.now() - 10 * 86400000).toISOString()
    const result = formatDate(tenDaysAgo)
    // Should contain a space and a number (some locale format)
    expect(result).toMatch(/\s/)
    expect(result).toMatch(/\d/)
  })
})

describe('formatFullDate', () => {
  it('returns full date and time string', () => {
    const dateStr = new Date(2024, 3, 15, 14, 30).toISOString()
    const result = formatFullDate(dateStr)
    // Should contain the year
    expect(result).toContain('2024')
  })

  it('handles dates correctly', () => {
    const date = new Date(2024, 0, 1, 0, 0).toISOString() // Jan 1, 2024
    const result = formatFullDate(date)
    // Should contain year and day
    expect(result).toContain('2024')
    expect(result).toContain('1')
  })
})
