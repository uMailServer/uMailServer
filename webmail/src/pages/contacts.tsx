import { Users } from "lucide-react"

/**
 * The server exposes no contacts endpoint to the webmail API, so there is
 * nothing real to list. Show that honestly instead of fabricated contacts.
 */
export function ContactsPage() {
  return (
    <div className="flex flex-col items-center justify-center py-16 text-center">
      <div className="rounded-full bg-muted p-4">
        <Users className="h-8 w-8 text-muted-foreground" />
      </div>
      <h3 className="mt-4 text-lg font-semibold">Contacts are not available</h3>
      <p className="text-sm text-muted-foreground">
        Address book management is not exposed by this server's webmail API. Use a CardDAV client to manage contacts.
      </p>
    </div>
  )
}
