import { it, expect, vi, afterEach } from 'vitest'
import API from '@/utils/api'
import { enablePush, getPushState } from './push'

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals() })

function stubBrowser(permission: NotificationPermission) {
  const sub = {
    endpoint: 'https://push.example/abc',
    toJSON: () => ({ endpoint: 'https://push.example/abc', keys: { p256dh: 'P', auth: 'A' } }),
    unsubscribe: vi.fn(async () => true),
  }
  let current: typeof sub | null = null
  const pushManager = {
    getSubscription: vi.fn(async () => current),
    subscribe: vi.fn(async (opts: PushSubscriptionOptionsInit) => { expect(opts.userVisibleOnly).toBe(true); current = sub; return sub }),
  }
  const reg = { pushManager }
  vi.stubGlobal('Notification', { permission, requestPermission: vi.fn(async () => permission) })
  vi.stubGlobal('PushManager', function PushManager() {})
  Object.defineProperty(navigator, 'serviceWorker', {
    configurable: true,
    value: { register: vi.fn(async () => reg), ready: Promise.resolve(reg), getRegistration: vi.fn(async () => reg) },
  })
  return { sub, pushManager }
}

it('subscribes and registers the subscription with the server', async () => {
  const { pushManager } = stubBrowser('granted')
  vi.spyOn(API, 'getVapidPublicKey').mockResolvedValue({ publicKey: 'BAAA_-A' })
  const subscribe = vi.spyOn(API, 'subscribePush').mockResolvedValue()
  await enablePush()
  expect(subscribe).toHaveBeenCalledWith({ endpoint: 'https://push.example/abc', keys: { p256dh: 'P', auth: 'A' } })
  const key = pushManager.subscribe.mock.calls[0][0].applicationServerKey as Uint8Array
  expect(Array.from(key)).toEqual([4, 0, 0, 255, 224]) // base64url "BAAA_-A"
  expect(await getPushState()).toBe('on')
})

it('drops the browser subscription when the server refuses it', async () => {
  const { sub } = stubBrowser('granted')
  vi.spyOn(API, 'getVapidPublicKey').mockResolvedValue({ publicKey: 'BAAA' })
  vi.spyOn(API, 'subscribePush').mockRejectedValue(new Error('invalid endpoint'))
  await expect(enablePush()).rejects.toThrow('invalid endpoint')
  expect(sub.unsubscribe).toHaveBeenCalled()
})

it('reports a server without push and a blocked permission', async () => {
  stubBrowser('denied')
  vi.spyOn(API, 'getVapidPublicKey').mockResolvedValue({})
  await expect(enablePush()).rejects.toThrow('not configured')
  vi.spyOn(API, 'getVapidPublicKey').mockResolvedValue({ publicKey: 'BAAA' })
  await expect(enablePush()).rejects.toThrow('blocked')
  expect(await getPushState()).toBe('denied')
})
