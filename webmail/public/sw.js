// Service worker for new-mail push notifications (see src/utils/push.ts).
// The server sends a JSON Notification: {title, body, tag, data, actions}.

self.addEventListener("push", (event) => {
  let n = {}
  if (event.data) {
    try {
      n = event.data.json()
    } catch {
      n = { body: event.data.text() }
    }
  }
  event.waitUntil(
    self.registration.showNotification(n.title || "New Email", {
      body: n.body || "",
      tag: n.tag || undefined,
      data: n.data || {},
      actions: Array.isArray(n.actions) ? n.actions.slice(0, 2) : [],
    }),
  )
})

self.addEventListener("notificationclick", (event) => {
  event.notification.close()
  if (event.action === "dismiss") return
  event.waitUntil(
    (async () => {
      const windows = await self.clients.matchAll({ type: "window", includeUncontrolled: true })
      for (const client of windows) {
        if (new URL(client.url).origin === self.location.origin) {
          await client.focus()
          if ("navigate" in client) await client.navigate("/inbox")
          return
        }
      }
      await self.clients.openWindow("/inbox")
    })(),
  )
})
