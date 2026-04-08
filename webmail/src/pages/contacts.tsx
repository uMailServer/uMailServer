import { useState } from "react"
import {
  Plus,
  Search,
  Mail,
  Phone,
  Edit,
  Trash2,
  ChevronLeft,
  ChevronRight,
  MoreHorizontal,
  User,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { Badge } from "@/components/ui/badge"
import { Separator } from "@/components/ui/separator"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { toast } from "sonner"

interface Contact {
  id: string
  name: string
  email: string
  phone?: string
  company?: string
  labels: string[]
}

const mockContacts: Contact[] = [
  {
    id: "1",
    name: "John Smith",
    email: "john@example.com",
    phone: "+1 555 123 4567",
    company: "ABC Corp",
    labels: ["work"],
  },
  {
    id: "2",
    name: "Sarah Johnson",
    email: "sarah.johnson@company.com",
    company: "XYZ Ltd",
    labels: ["work"],
  },
  {
    id: "3",
    name: "Mike Wilson",
    email: "mike.wilson@gmail.com",
    labels: ["personal"],
  },
  {
    id: "4",
    name: "Emily Brown",
    email: "emily.brown@outlook.com",
    phone: "+1 532 987 6543",
    labels: ["family"],
  },
  {
    id: "5",
    name: "Tech Newsletter",
    email: "newsletter@tech.com",
    labels: ["newsletter"],
  },
]

export function ContactsPage() {
  const [contacts, setContacts] = useState<Contact[]>(mockContacts)
  const [searchQuery, setSearchQuery] = useState("")
  const [showAddDialog, setShowAddDialog] = useState(false)
  const [editingContact, setEditingContact] = useState<Contact | null>(null)
  const [formData, setFormData] = useState({
    name: "",
    email: "",
    phone: "",
    company: "",
  })

  const filteredContacts = contacts.filter(
    (c) =>
      c.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
      c.email.toLowerCase().includes(searchQuery.toLowerCase())
  )

  const handleAdd = () => {
    setFormData({ name: "", email: "", phone: "", company: "" })
    setEditingContact(null)
    setShowAddDialog(true)
  }

  const handleEdit = (contact: Contact) => {
    setFormData({
      name: contact.name,
      email: contact.email,
      phone: contact.phone || "",
      company: contact.company || "",
    })
    setEditingContact(contact)
    setShowAddDialog(true)
  }

  const handleSave = () => {
    if (!formData.name || !formData.email) {
      toast.error("Name and email are required")
      return
    }

    if (editingContact) {
      setContacts(contacts.map((c) =>
        c.id === editingContact.id
          ? { ...c, ...formData }
          : c
      ))
      toast.success("Contact updated")
    } else {
      const newContact: Contact = {
        id: Math.random().toString(36).substr(2, 9),
        ...formData,
        labels: [],
      }
      setContacts([...contacts, newContact])
      toast.success("Contact added")
    }
    setShowAddDialog(false)
  }

  const handleDelete = (id: string) => {
    setContacts(contacts.filter((c) => c.id !== id))
    toast.success("Contact deleted")
  }

  const getInitials = (name: string) => {
    return name
      .split(" ")
      .map((n) => n[0])
      .join("")
      .toUpperCase()
      .slice(0, 2)
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="relative max-w-md flex-1">
          <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            placeholder="Search contacts..."
            className="pl-9"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
          />
        </div>
        <Button onClick={handleAdd}>
          <Plus className="h-4 w-4 mr-1" />
          Add Contact
        </Button>
      </div>

      {filteredContacts.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-16 text-center">
          <div className="rounded-full bg-muted p-4">
            <User className="h-8 w-8 text-muted-foreground" />
          </div>
          <h3 className="mt-4 text-lg font-semibold">No contacts</h3>
          <p className="text-sm text-muted-foreground">
            {searchQuery ? "No contacts match your search." : "Add your first contact to get started."}
          </p>
        </div>
      ) : (
        <div className="rounded-lg border bg-card">
          {filteredContacts.map((contact, index) => (
            <div key={contact.id}>
              {index > 0 && <Separator />}
              <div className="flex items-center gap-4 p-4 hover:bg-accent/50 transition-colors">
                <Avatar className="h-10 w-10">
                  <AvatarFallback className="bg-gradient-to-br from-primary to-primary/80 text-primary-foreground font-semibold">
                    {getInitials(contact.name)}
                  </AvatarFallback>
                </Avatar>
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="font-medium">{contact.name}</span>
                    {contact.labels.map((label) => (
                      <Badge key={label} variant="secondary" className="text-[10px]">
                        {label}
                      </Badge>
                    ))}
                  </div>
                  <div className="flex items-center gap-4 text-sm text-muted-foreground">
                    <span className="flex items-center gap-1">
                      <Mail className="h-3 w-3" />
                      {contact.email}
                    </span>
                    {contact.phone && (
                      <span className="flex items-center gap-1">
                        <Phone className="h-3 w-3" />
                        {contact.phone}
                      </span>
                    )}
                    {contact.company && (
                      <span className="text-xs">{contact.company}</span>
                    )}
                  </div>
                </div>
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button variant="ghost" size="icon" className="h-8 w-8">
                      <MoreHorizontal className="h-4 w-4" />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end">
                    <DropdownMenuItem onClick={() => handleEdit(contact)}>
                      <Edit className="h-4 w-4 mr-2" />
                      Edit
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      className="text-destructive"
                      onClick={() => handleDelete(contact.id)}
                    >
                      <Trash2 className="h-4 w-4 mr-2" />
                      Delete
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              </div>
            </div>
          ))}
        </div>
      )}

      <div className="flex items-center justify-between">
        <span className="text-sm text-muted-foreground">
          {filteredContacts.length} contact{filteredContacts.length !== 1 ? "s" : ""}
        </span>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="icon" disabled>
            <ChevronLeft className="h-4 w-4" />
          </Button>
          <Button variant="outline" size="icon" disabled>
            <ChevronRight className="h-4 w-4" />
          </Button>
        </div>
      </div>

      <Dialog open={showAddDialog} onOpenChange={setShowAddDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {editingContact ? "Edit Contact" : "Add Contact"}
            </DialogTitle>
          </DialogHeader>
          <div className="space-y-4">
            <div>
              <label className="text-sm font-medium">Name</label>
              <Input
                className="mt-1"
                placeholder="John Smith"
                value={formData.name}
                onChange={(e) => setFormData({ ...formData, name: e.target.value })}
              />
            </div>
            <div>
              <label className="text-sm font-medium">Email</label>
              <Input
                className="mt-1"
                type="email"
                placeholder="john@example.com"
                value={formData.email}
                onChange={(e) => setFormData({ ...formData, email: e.target.value })}
              />
            </div>
            <div>
              <label className="text-sm font-medium">Phone (optional)</label>
              <Input
                className="mt-1"
                placeholder="+1 555 123 4567"
                value={formData.phone}
                onChange={(e) => setFormData({ ...formData, phone: e.target.value })}
              />
            </div>
            <div>
              <label className="text-sm font-medium">Company (optional)</label>
              <Input
                className="mt-1"
                placeholder="ABC Corp"
                value={formData.company}
                onChange={(e) => setFormData({ ...formData, company: e.target.value })}
              />
            </div>
            <div className="flex justify-end gap-2">
              <Button variant="outline" onClick={() => setShowAddDialog(false)}>
                Cancel
              </Button>
              <Button onClick={handleSave}>
                {editingContact ? "Update" : "Add"}
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}
