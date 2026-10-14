import { describe, it, expect } from 'vitest'
import { sanitizeHTML } from './sanitize'

const hasSrc = (h: string) => /\ssrc=/.test(h)

describe('r284 sanitizeHTML', () => {
  // F7580: obfuscated remote URLs must not defeat the tracking-pixel block.
  it.each(['/&#9;/evil.com/x.gif', '/\\evil.com/x.gif', '\\\\evil.com/x.gif', 'http:evil.com/x.gif', 'https:&#9;//evil.com/x.gif'])(
    'blocks remote img src %s',
    (s) => {
      expect(hasSrc(sanitizeHTML(`<img src="${s}">`))).toBe(false)
      expect(hasSrc(sanitizeHTML(`<img src="${s}">`, { allowRemoteContent: true }))).toBe(true)
    },
  )
  it('keeps same-origin img src', () => {
    expect(hasSrc(sanitizeHTML('<img src="/api/v1/mail/attachment?id=1">'))).toBe(true)
  })
  // F7581: app utility classes must not reach the DOM (UI overlay / phishing).
  it('drops class attributes but keeps title', () => {
    const out = sanitizeHTML('<div class="fixed inset-0 z-50" title="t">x</div>')
    expect(out).not.toMatch(/class=/)
    expect(out).toContain('title="t"')
  })
})
