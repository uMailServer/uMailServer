import { describe, it, expect } from 'vitest'
import { sanitizeHTML } from './sanitize'

describe('F6090 style attribute remote/overlay leakage', () => {
  it('strips url() from inline style', () => {
    const out = sanitizeHTML('<div style="background:url(https://t.example/p.gif);color:red">x</div>')
    expect(out).not.toMatch(/url\(/i)
    expect(out).toContain('color:red')
  })
  it('strips position:fixed overlays and expression()', () => {
    const out = sanitizeHTML('<div style="position:fixed;top:0;left:0;width:100vw;height:100vh;color:red">x</div>')
    expect(out).not.toMatch(/position\s*:\s*fixed/i)
    const o2 = sanitizeHTML('<div style="width:expression(alert(1))">x</div>')
    expect(o2).not.toMatch(/expression/i)
  })
})

describe('F6091 remote images blocked by default', () => {
  it('removes remote src (tracking pixel)', () => {
    const out = sanitizeHTML('<img src="https://t.example/p.gif" width="1" height="1">')
    expect(out).not.toContain('t.example')
  })
  it('removes protocol-relative remote src', () => {
    expect(sanitizeHTML('<img src="//t.example/p.gif">')).not.toContain('t.example')
  })
  it('can be allowed explicitly', () => {
    expect(sanitizeHTML('<img src="https://t.example/p.gif">', { allowRemoteContent: true })).toContain('t.example')
  })
})

describe('sanitizer regression', () => {
  it('keeps noopener on target=_blank', () => {
    expect(sanitizeHTML('<a href="https://a.example" target="_blank">a</a>')).toContain('noopener noreferrer')
  })
  it('drops javascript: and data: hrefs, svg', () => {
    expect(sanitizeHTML('<a href="javascript:alert(1)">a</a>')).not.toContain('javascript')
    expect(sanitizeHTML('<svg onload=alert(1)><script>1</script></svg>')).not.toContain('svg')
  })
})
