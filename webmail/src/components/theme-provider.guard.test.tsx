import { it, expect, vi } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { useTheme } from './theme-provider'

// F-theme-guard: the useTheme guard is dead code. The context is created with
// a non-undefined initialState, so `context === undefined` can never hold and
// a consumer rendered OUTSIDE a ThemeProvider silently receives the initial
// state (no-op setTheme, 'system'/'light') instead of the error the code
// promises: "useTheme must be used within a ThemeProvider".
// Contract basis: the guard's own message + EmailContext's working null pattern.

let probeError: Error | null = null
let captured: ReturnType<typeof useTheme> | null = null

it('F-theme-guard useTheme outside a provider throws, not silent defaults', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)

  function Probe() {
    try {
      captured = useTheme()
    } catch (err) {
      probeError = err as Error
    }
    return null
  }

  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  try {
    await act(async () => root.render(createElement(Probe)))

    console.log(
      'EXPECTED: useTheme outside a provider throws "useTheme must be used within a ThemeProvider"',
      '| ACTUAL:',
      probeError ? `"${probeError.message}"` : `silent defaults (theme=${captured?.theme}, resolvedTheme=${captured?.resolvedTheme}, setTheme no-op)`,
    )
    if (!probeError || probeError.message !== 'useTheme must be used within a ThemeProvider') {
      console.log('PROBLEM CONFIRMED: the useTheme guard is dead code — misuse returns silent defaults')
      expect(probeError).not.toBeNull()
      expect(probeError?.message).toBe('useTheme must be used within a ThemeProvider')
    }

    console.log('PROBLEM NOT REPRODUCED')
    console.log('FIX VERIFIED')
  } finally {
    await act(async () => root.unmount())
    container.remove()
    vi.unstubAllGlobals()
  }
})
