import { useCallback, useEffect, useState } from "react"
import { useNavigate } from "react-router-dom"
import { Users, Plus, Search, Mail, Pencil, Trash2, Phone, Building2 } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import api, { ApiError, type Contact, type ContactInput } from "@/utils/api"

const EMPTY: ContactInput = { name: "", emails: [], phones: [], organization: "", note: "" }

// One value per line in the form; blank lines are dropped.
const lines = (s: string) => s.split("\n").map((v) => v.trim()).filter(Boolean)

function errorText(err: unknown, fallback: string): string {
  return err instanceof ApiError && err.serverMessage ? err.serverMessage : fallback
}

function ContactForm({
  initial,
  onCancel,
  onSave,
}: {
  initial: ContactInput
  onCancel: () => void
  onSave: (c: ContactInput) => Promise<void>
}) {
  const [name, setName] = useState(initial.name)
  const [emails, setEmails] = useState(initial.emails.join("\n"))
  const [phones, setPhones] = useState(initial.phones.join("\n"))
  const [organization, setOrganization] = useState(initial.organization)
  const [note, setNote] = useState(initial.note)
  const [saving, setSaving] = useState(false)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setSaving(true)
    try {
      await onSave({ name, emails: lines(emails), phones: lines(phones), organization, note })
    } finally {
      setSaving(false)
    }
  }

  return (
    <form onSubmit={submit} className="space-y-4">
      <div className="space-y-2">
        <Label htmlFor="contact-name">Name</Label>
        <Input id="contact-name" value={name} onChange={(e) => setName(e.target.value)} autoFocus />
      </div>
      <div className="space-y-2">
        <Label htmlFor="contact-emails">Email addresses (one per line)</Label>
        <Textarea id="contact-emails" rows={2} value={emails} onChange={(e) => setEmails(e.target.value)} />
      </div>
      <div className="space-y-2">
        <Label htmlFor="contact-phones">Phone numbers (one per line)</Label>
        <Textarea id="contact-phones" rows={2} value={phones} onChange={(e) => setPhones(e.target.value)} />
      </div>
      <div className="space-y-2">
        <Label htmlFor="contact-org">Organization</Label>
        <Input id="contact-org" value={organization} onChange={(e) => setOrganization(e.target.value)} />
      </div>
      <div className="space-y-2">
        <Label htmlFor="contact-note">Note</Label>
        <Textarea id="contact-note" rows={3} value={note} onChange={(e) => setNote(e.target.value)} />
      </div>
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={saving || (!name.trim() && lines(emails).length === 0)}>
          {saving ? "Saving..." : "Save"}
        </Button>
      </DialogFooter>
    </form>
  )
}

/**
 * The user's address book. Contacts live in the server's CardDAV storage, so
 * the ones synced from a phone or desktop client show up here too.
 */
export function ContactsPage() {
  const navigate = useNavigate()
  const [contacts, setContacts] = useState<Contact[] | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [query, setQuery] = useState("")
  // null: dialog closed; "new": creating; a contact: editing it.
  const [editing, setEditing] = useState<Contact | "new" | null>(null)

  const load = useCallback(async () => {
    try {
      setContacts(await api.getContacts())
      setLoadError(null)
    } catch (err) {
      setLoadError(errorText(err, "Could not load contacts"))
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  const save = async (input: ContactInput) => {
    try {
      if (editing === "new") {
        await api.createContact(input)
        toast.success("Contact added")
      } else if (editing) {
        await api.updateContact(editing, input)
        toast.success("Contact saved")
      }
      setEditing(null)
      await load()
    } catch (err) {
      toast.error(errorText(err, "Could not save contact"))
    }
  }

  const remove = async (c: Contact) => {
    if (!window.confirm(`Delete ${c.name || c.emails?.[0] || "this contact"}?`)) return
    try {
      await api.deleteContact(c)
      toast.success("Contact deleted")
      await load()
    } catch (err) {
      toast.error(errorText(err, "Could not delete contact"))
    }
  }

  const q = query.trim().toLowerCase()
  const visible = (contacts ?? []).filter(
    (c) =>
      !q ||
      [c.name, c.organization ?? "", ...(c.emails ?? []), ...(c.phones ?? [])].some((f) =>
        f.toLowerCase().includes(q)
      )
  )

  const initial: ContactInput =
    editing && editing !== "new"
      ? {
          name: editing.name,
          emails: editing.emails ?? [],
          phones: editing.phones ?? [],
          organization: editing.organization ?? "",
          note: editing.note ?? "",
        }
      : EMPTY

  return (
    <div className="space-y-4 p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h1 className="text-2xl font-semibold">Contacts</h1>
        <Button onClick={() => setEditing("new")}>
          <Plus className="mr-2 h-4 w-4" />
          New contact
        </Button>
      </div>

      <div className="relative max-w-md">
        <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          aria-label="Search contacts"
          placeholder="Search contacts"
          className="pl-9"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
      </div>

      {loadError && (
        <div role="alert" className="rounded-md border border-destructive/50 p-3 text-sm text-destructive">
          {loadError}{" "}
          <Button variant="link" className="h-auto p-0" onClick={load}>
            Retry
          </Button>
        </div>
      )}

      {contacts !== null && visible.length === 0 && !loadError && (
        <div className="flex flex-col items-center justify-center py-16 text-center">
          <div className="rounded-full bg-muted p-4">
            <Users className="h-8 w-8 text-muted-foreground" />
          </div>
          <h3 className="mt-4 text-lg font-semibold">{q ? "No matching contacts" : "No contacts yet"}</h3>
          <p className="text-sm text-muted-foreground">
            {q
              ? "Try a different search."
              : "Add a contact, or sync your phone's address book over CardDAV."}
          </p>
        </div>
      )}

      <ul className="divide-y rounded-md border">
        {visible.map((c) => (
          <li key={`${c.addressbook}/${c.uid}`} className="flex flex-wrap items-start justify-between gap-2 p-3">
            <div className="min-w-0 space-y-1">
              <div className="font-medium">{c.name || c.emails?.[0]}</div>
              {(c.emails ?? []).map((e) => (
                <div key={e} className="flex items-center gap-1 text-sm text-muted-foreground break-all">
                  <Mail className="h-3 w-3 shrink-0" />
                  {e}
                </div>
              ))}
              {(c.phones ?? []).map((p) => (
                <div key={p} className="flex items-center gap-1 text-sm text-muted-foreground">
                  <Phone className="h-3 w-3 shrink-0" />
                  {p}
                </div>
              ))}
              {c.organization && (
                <div className="flex items-center gap-1 text-sm text-muted-foreground">
                  <Building2 className="h-3 w-3 shrink-0" />
                  {c.organization}
                </div>
              )}
            </div>
            <div className="flex gap-1">
              {c.emails?.[0] && (
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={`Email ${c.name || c.emails[0]}`}
                  onClick={() => navigate(`/compose?to=${encodeURIComponent(c.emails![0])}`)}
                >
                  <Mail className="h-4 w-4" />
                </Button>
              )}
              <Button variant="ghost" size="icon" aria-label={`Edit ${c.name || c.emails?.[0]}`} onClick={() => setEditing(c)}>
                <Pencil className="h-4 w-4" />
              </Button>
              <Button variant="ghost" size="icon" aria-label={`Delete ${c.name || c.emails?.[0]}`} onClick={() => remove(c)}>
                <Trash2 className="h-4 w-4" />
              </Button>
            </div>
          </li>
        ))}
      </ul>

      <Dialog open={editing !== null} onOpenChange={(open) => !open && setEditing(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editing === "new" ? "New contact" : "Edit contact"}</DialogTitle>
            <DialogDescription>Changes sync to your CardDAV clients.</DialogDescription>
          </DialogHeader>
          {editing !== null && (
            <ContactForm
              key={editing === "new" ? "new" : `${editing.addressbook}/${editing.uid}`}
              initial={initial}
              onCancel={() => setEditing(null)}
              onSave={save}
            />
          )}
        </DialogContent>
      </Dialog>
    </div>
  )
}
