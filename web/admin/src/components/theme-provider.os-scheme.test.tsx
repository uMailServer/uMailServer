import { it, expect, vi } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { ThemeProvider, useTheme } from './theme-provider'

// F-theme-os sibling of the round-14 webmail defect (web/admin variant):
// theme='system' must track OS scheme changes while the app is open.
// The effect (theme-provider.tsx, deps [theme]) resolves matchMedia once per
// theme change with no 'change' listener, so a live OS flip leaves the root
// class and resolvedTheme stale until reload — for the DEFAULT theme.

let current!: ReturnType<typeof useTheme>
const registered: Array<() => void> = []

let flipOS: (dark: boolean) => void

function setupMatchMedia(initialDark: boolean) {
  const mq = {
    matches: initialDark,
    addEventListener(_type: string, cb: () => void) {
      registered.push(cb)
    },
    removeEventListener(_type: string, cb: () => void) {
      const i = registered.indexOf(cb)
      if (i >= 0) registered.splice(i, 1)
    },
  }
  flipOS = (dark: boolean) => {
    mq.matches = dark
    registered.slice().forEach((cb) => cb())
  }
  vi.stubGlobal('matchMedia', vi.fn().mockReturnValue(mq))
}

function Probe() {
  current = useTheme()
  return null
}

it('F-theme-os system theme tracks OS scheme changes while open (web/admin)', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  setupMatchMedia(false)
  localStorage.setItem('umail-admin-theme', 'system')

  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  try {
    await act(async () =>
      root.render(
        createElement(
          ThemeProvider,
          { defaultTheme: 'system', storageKey: 'umail-admin-theme' },
          createElement(Probe),
        ),
      ),
    )

    // CONTROL: system + light OS -> light root class (mount resolution works)
    const rootClass = document.documentElement.className
    const controlOk = rootClass.includes('light') && !rootClass.includes('dark')
    console.log('CONTROL EXPECTED: system+light OS -> light root | ACTUAL:', JSON.stringify(rootClass))
    if (!controlOk) {
      console.log('CONTROL FAILED: mount resolution broken — harness invalid')
      expect(controlOk).toBe(true)
    }

    // CONTRACT: OS flips to dark while theme stays 'system'
    act(() => flipOS(true))
    const afterFlip = document.documentElement.className
    const tracked = afterFlip.includes('dark') && !afterFlip.includes('light')
    console.log(
      'EXPECTED: root class follows OS flip to dark | ACTUAL:',
      JSON.stringify(afterFlip),
      '| resolvedTheme:',
      current.resolvedTheme,
    )
    if (!tracked || current.resolvedTheme !== 'dark') {
      console.log('PROBLEM CONFIRMED: system theme never tracks OS scheme changes while open (web/admin)')
      expect(tracked).toBe(true)
      expect(current.resolvedTheme).toBe('dark')
    }

    // POST-FIX ONLY: the listener must be removed on unmount (no leak)
    await act(async () => root.unmount())
    expect(registered).toHaveLength(0)

    console.log('PROBLEM NOT REPRODUCED')
    console.log('FIX VERIFIED')
  } finally {
    container.remove()
    vi.unstubAllGlobals()
    localStorage.removeItem('umail-admin-theme')
  }
})
