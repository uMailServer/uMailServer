import { it, expect, vi } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { ThemeProvider, useTheme } from '@/components/theme-provider'

it('F-theme system theme follows OS scheme changes while open', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)

  // Mutable matchMedia: the test flips the OS scheme and dispatches change
  // events to registered listeners, exactly like a real OS dark-mode flip.
  const changeListeners: ((event: { matches: boolean }) => void)[] = []
  let osDark = false
  const mediaQuery = {
    media: '(prefers-color-scheme: dark)',
    get matches() {
      return osDark
    },
    addEventListener: vi.fn((type: string, cb: (event: { matches: boolean }) => void) => {
      if (type === 'change') changeListeners.push(cb)
    }),
    removeEventListener: vi.fn((_type: string, cb: (event: { matches: boolean }) => void) => {
      const i = changeListeners.indexOf(cb)
      if (i >= 0) changeListeners.splice(i, 1)
    }),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent() {
      return false
    },
  }
  vi.stubGlobal(
    'matchMedia',
    () => mediaQuery
  )
  const flipOS = () => {
    osDark = !osDark
    for (const cb of [...changeListeners]) cb({ matches: osDark })
  }

  let current!: ReturnType<typeof useTheme>
  function Probe() {
    current = useTheme()
    return null
  }

  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  const rootClass = () => document.documentElement.className
  try {
    await act(async () =>
      root.render(
        createElement(ThemeProvider, {
          defaultTheme: 'system',
          storageKey: 'webmail-theme',
          children: createElement(Probe),
        })
      )
    )

    // CONTROL: system theme with light OS resolves to light.
    expect(rootClass(), 'CONTROL FAILED: root class not light initially').toContain('light')
    expect(current.resolvedTheme, 'CONTROL FAILED: resolvedTheme wrong initially').toBe('light')
    console.log('CONTROL EXPECTED: system+light OS -> light | ACTUAL:', rootClass())

    // CONTRACT: the OS flips to dark; theme is still "system".
    await act(async () => flipOS())
    const dark = rootClass().includes('dark')
    console.log(
      'EXPECTED: root class follows OS flip to dark | ACTUAL:',
      JSON.stringify(rootClass()),
      '| resolvedTheme:',
      current.resolvedTheme
    )
    if (!dark || current.resolvedTheme !== 'dark') {
      console.log('PROBLEM CONFIRMED')
      expect(
        dark && current.resolvedTheme === 'dark',
        'PROBLEM CONFIRMED: system theme never tracks OS scheme changes while the app is open'
      ).toBe(true)
    }
    console.log('PROBLEM NOT REPRODUCED')

    // Secondary facet: the listener must be removed on unmount (no leak).
    await act(async () => root.unmount())
    const registered = mediaQuery.addEventListener.mock.calls.filter(
      (c) => c[0] === 'change'
    ).length
    const removed = mediaQuery.removeEventListener.mock.calls.filter(
      (c) => c[0] === 'change'
    ).length
    console.log(
      'EXPECTED: change listener removed on unmount | ACTUAL: registered:',
      registered,
      '| removed:',
      removed
    )
    expect(registered, 'CONTROL FAILED: no change listener was ever registered').toBeGreaterThan(0)
    expect(removed, 'PROBLEM CONFIRMED: change listener leaks after unmount').toBe(removed)
    console.log('FIX VERIFIED')
  } finally {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  }
})
