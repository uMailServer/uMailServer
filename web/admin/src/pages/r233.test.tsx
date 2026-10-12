import { it, expect } from 'vitest'
import { formatDate, formatTime, formatDateTime } from '@/lib/utils'

// F7101: one explicit locale for all admin dates (previously bare toLocale*()
// calls picked the browser locale independently per call site).
it('F7101 shared formatters are locale-explicit and consistent', () => {
  const iso = '2026-10-12T14:03:09Z'
  const d = new Date(iso)
  expect(formatDate(iso)).toBe(d.toLocaleDateString('en-GB', { dateStyle: 'medium' }))
  expect(formatTime(iso)).toBe(d.toLocaleTimeString('en-GB', { timeStyle: 'medium' }))
  expect(formatDateTime(iso)).toContain(formatDate(iso))
  expect(formatDate(iso)).toMatch(/Oct 2026/)
})

it('F7101 missing or invalid values render "-"', () => {
  for (const v of [undefined, null, '', 'garbage']) {
    expect(formatDate(v)).toBe('-')
    expect(formatTime(v)).toBe('-')
    expect(formatDateTime(v)).toBe('-')
  }
})
