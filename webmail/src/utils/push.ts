import api from "@/utils/api"

// Web Push for new-mail notifications: the service worker (public/sw.js)
// shows what the server sends to the subscription registered here.

export type PushState = "unsupported" | "denied" | "off" | "on"

const SW_URL = "/sw.js"

export function pushSupported(): boolean {
  return typeof window !== "undefined" && "serviceWorker" in navigator && "PushManager" in window && "Notification" in window
}

function base64UrlToBytes(value: string): Uint8Array<ArrayBuffer> {
  const b64 = value.replace(/-/g, "+").replace(/_/g, "/") + "=".repeat((4 - (value.length % 4)) % 4)
  const raw = atob(b64)
  const out = new Uint8Array(new ArrayBuffer(raw.length))
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i)
  return out
}

async function currentSubscription(): Promise<PushSubscription | null> {
  const reg = await navigator.serviceWorker.getRegistration("/")
  return reg ? reg.pushManager.getSubscription() : null
}

export async function getPushState(): Promise<PushState> {
  if (!pushSupported()) return "unsupported"
  if (Notification.permission === "denied") return "denied"
  return (await currentSubscription()) ? "on" : "off"
}

// enablePush asks for permission, subscribes this browser and registers the
// subscription with the server. It throws with a message to show the user.
export async function enablePush(): Promise<void> {
  if (!pushSupported()) throw new Error("This browser does not support push notifications")
  const { publicKey } = await api.getVapidPublicKey()
  if (!publicKey) throw new Error("Push notifications are not configured on this server")
  if ((await Notification.requestPermission()) !== "granted") {
    throw new Error("Notifications are blocked for this site in the browser")
  }
  const reg = await navigator.serviceWorker.register(SW_URL, { scope: "/" })
  await navigator.serviceWorker.ready
  const sub = (await reg.pushManager.getSubscription()) ??
    (await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: base64UrlToBytes(publicKey) }))
  const json = sub.toJSON()
  if (!json.endpoint || !json.keys?.p256dh || !json.keys?.auth) {
    await sub.unsubscribe()
    throw new Error("The browser returned an incomplete push subscription")
  }
  try {
    await api.subscribePush({ endpoint: json.endpoint, keys: { p256dh: json.keys.p256dh, auth: json.keys.auth } })
  } catch (err) {
    await sub.unsubscribe()
    throw err
  }
}

// disablePush removes the subscription from the server, then the browser.
export async function disablePush(): Promise<void> {
  if (!pushSupported()) return
  const sub = await currentSubscription()
  if (!sub) return
  await api.unsubscribePush(sub.endpoint)
  await sub.unsubscribe()
}
