import { describe, it, expect } from 'vitest'
import { sanitizeHTML, sanitizeText } from './sanitize'

describe('sanitizeHTML', () => {
  it('allows safe HTML tags', () => {
    const input = '<p>Hello <strong>world</strong></p>'
    const result = sanitizeHTML(input)
    expect(result).toContain('<strong>world</strong>')
    expect(result).not.toContain('<script>')
  })

  it('removes script tags', () => {
    const input = '<script>alert("xss")</script><p>Hello</p>'
    const result = sanitizeHTML(input)
    expect(result).not.toContain('<script>')
    expect(result).not.toContain('alert')
    expect(result).toContain('<p>Hello</p>')
  })

  it('removes inline event handlers like onerror', () => {
    const input = '<img src=x onerror=alert(1)>'
    const result = sanitizeHTML(input)
    expect(result).not.toContain('onerror')
  })

  it('forbids iframes', () => {
    const input = '<iframe src="https://evil.com"></iframe><p>content</p>'
    const result = sanitizeHTML(input)
    expect(result).not.toContain('<iframe>')
    expect(result).toContain('<p>content</p>')
  })

  it('allows links with target attribute', () => {
    const input = '<a href="https://example.com" target="_blank">Link</a>'
    const result = sanitizeHTML(input)
    expect(result).toContain('target="_blank"')
    expect(result).toContain('https://example.com')
  })
})

describe('sanitizeText', () => {
  it('strips all HTML tags', () => {
    const input = '<p>Hello <strong>world</strong></p>'
    const result = sanitizeText(input)
    expect(result).toBe('Hello world')
  })

  it('removes script content', () => {
    const input = '<script>doEvil()</script>Safe text'
    const result = sanitizeText(input)
    expect(result).not.toContain('<script>')
    expect(result).not.toContain('doEvil')
    expect(result).toContain('Safe text')
  })

  it('returns plain text unchanged', () => {
    const input = 'Plain text without HTML'
    const result = sanitizeText(input)
    expect(result).toBe('Plain text without HTML')
  })
})
