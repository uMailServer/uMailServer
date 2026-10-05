import { it, expect, vi } from 'vitest'
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { AuthProvider, useAuth } from './AuthContext'
import api from '../utils/api'
it('logout and unmount invalidate earlier login completions', async () => {
 vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
 const requests: { resolve: (value: object) => void; reject: (error: Error) => void }[] = []
 vi.spyOn(api, 'post').mockImplementation(() => new Promise((resolve, reject) => requests.push({ resolve, reject })) as never)
 vi.spyOn(api, 'setToken').mockImplementation(() => {})
 let current!: ReturnType<typeof useAuth>
 function Probe() { current = useAuth(); return null }
 const container = document.createElement('div'); document.body.append(container)
 const root = createRoot(container)
 let mounted = true
 try {
  await act(async () => root.render(createElement(AuthProvider, { children: createElement(Probe) })))
  let control!: Promise<boolean>
  act(() => { control = current.login('control@example.test', 'fixture') })
  await act(async () => requests[0].resolve({}))
  expect(await control, 'INVALID CONTROL').toBe(true)
  expect(current.user?.email, 'INVALID CONTROL').toBe('control@example.test')
  console.log('CONTROL EXPECTED: completed login sets user ACTUAL:', current.user?.email)
  act(() => current.logout())
  let pending!: Promise<boolean>
  act(() => { pending = current.login('stale@example.test', 'fixture') })
  act(() => current.logout())
  await act(async () => requests[1].resolve({}))
  const result = await pending
  console.log('EXPECTED: logged out, stale login returns false ACTUAL:', current.isAuthenticated, result)
  if (current.isAuthenticated || result || current.user !== null) {
   console.log('PROBLEM CONFIRMED')
   expect(current.isAuthenticated).toBe(false)
   expect(result).toBe(false)
   expect(current.user).toBeNull()
  }
  console.log('PROBLEM NOT REPRODUCED')
  let old!: Promise<boolean>; let latest!: Promise<boolean>
  act(() => { old = current.login('older@example.test', 'fixture') })
  act(() => { latest = current.login('latest@example.test', 'fixture') })
  await act(async () => requests[3].resolve({}))
  expect(await latest).toBe(true)
  await act(async () => requests[2].reject(new Error('stale error')))
  expect(await old).toBe(false)
  expect(current.user?.email).toBe('latest@example.test')
  expect(current.error).toBeNull(); expect(current.loading).toBe(false)
  let obsolete!: Promise<boolean>
  act(() => { obsolete = current.login('obsolete@example.test', 'fixture') })
  act(() => current.logout())
  expect(current.loading).toBe(false)
  await act(async () => requests[4].reject(new Error('logged-out error')))
  expect(await obsolete).toBe(false)
  expect(current.error).toBeNull(); expect(current.isAuthenticated).toBe(false)
  let unmounted!: Promise<boolean>
  act(() => { unmounted = current.login('unmounted@example.test', 'fixture') })
  await act(async () => root.unmount()); mounted = false
  requests[5].resolve({})
  expect(await unmounted).toBe(false)
  console.log('FIX VERIFIED')
 } finally {
  if (mounted) await act(async () => root.unmount())
  container.remove(); vi.restoreAllMocks(); vi.unstubAllGlobals()
 }
})
