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
