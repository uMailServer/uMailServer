import { it, expect, vi } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { ThemeProvider, useTheme } from '@/components/theme-provider'

const STORAGE_KEY = 'umail-admin-theme' // the key App.tsx actually passes
let current!: ReturnType<typeof useTheme>

function Probe() {
  current = useTheme()
  return null
}

function stubEnv() {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal(
    'matchMedia',
    (query: string) => ({
      matches: false,
      media: query,
      addListener() {},
      removeListener() {},
      addEventListener() {},
      removeEventListener() {},
      dispatchEvent() {
        return false
      },
    })
  )
}

async function renderProvider() {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  await act(async () =>
    root.render(
      createElement(ThemeProvider, {
        defaultTheme: 'system',
        storageKey: STORAGE_KEY,
        children: createElement(Probe),
      })
    )
  )
  return { container, root }
}

function cleanup(container: HTMLElement, root: { unmount: () => void }) {
  root.unmount()
  container.remove()
  localStorage.removeItem(STORAGE_KEY)
}

it('F-theme an invalid stored theme falls back to the default', async () => {
  stubEnv()
  localStorage.setItem(STORAGE_KEY, 'purple')
  const { container, root } = await renderProvider()
  try {
    const rootClassName = document.documentElement.className
    console.log(
      'EXPECTED: invalid stored value falls back to default (system -> light) | ACTUAL: root class =',
      JSON.stringify(rootClassName),
      '| theme:',
      JSON.stringify(current.theme),
      '| resolvedTheme:',
      JSON.stringify(current.resolvedTheme)
    )
    const validRootClass =
      rootClassName === 'light' || rootClassName === 'dark' || rootClassName === 'light dark'
    if (!validRootClass || current.theme !== 'system') {
      console.log('PROBLEM CONFIRMED')
      expect(
        validRootClass && current.theme === 'system',
        'PROBLEM CONFIRMED: an invalid stored theme is trusted and corrupts the root class'
      ).toBe(true)
    }
    console.log('PROBLEM NOT REPRODUCED')
    console.log('FIX VERIFIED')
  } finally {
    cleanup(container, root)
  }
})

it('F-theme a valid stored theme is still honored', async () => {
  stubEnv()
  localStorage.setItem(STORAGE_KEY, 'dark')
  const { container, root } = await renderProvider()
  try {
    expect(document.documentElement.className).toContain('dark')
    expect(current.theme).toBe('dark')
    expect(current.resolvedTheme).toBe('dark')
    console.log('CONTROL EXPECTED: valid stored dark honored | ACTUAL: honored')
  } finally {
    cleanup(container, root)
  }
})
