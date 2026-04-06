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
} from "lucide-react"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
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
    name: "Ahmet Yılmaz",
    email: "ahmet@example.com",
    phone: "+90 555 123 4567",
    company: "ABC Corp",
    labels: ["iş"],
  },
  {
    id: "2",
    name: "Ayşe Demir",
    email: "ayse.demir@company.com",
    company: "XYZ Ltd",
    labels: ["iş"],
  },
  {
    id: "3",
    name: "Mehmet Kaya",
    email: "mehmet.k@gmail.com",
    labels: ["kişisel"],
  },
  {
    id: "4",
    name: "Zeynep Ak",
    email: "zeynep@outlook.com",
    phone: "+90 532 987 6543",
    labels: ["aile"],
  },
  {
    id: "5",
    name: "Tech Newsletter",
    email: "newsletter@tech.com",
    labels: ["haberler"],
  },
]

export function ContactsPage() {
  const [contacts, setContacts] = useState<Contact[]>(mockContacts)
  const [searchQuery, setSearchQuery] = useState("")
  const [loading, setLoading] = useState(false)
  const [showAddDialog, setShowAddDialog] = useState(false)
  const [newContact, setNewContact] = useState({ name: "", email: "", phone: "", company: "" })

  const filteredContacts = contacts.filter(
    (c) =>
      c.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
      c.email.toLowerCase().includes(searchQuery.toLowerCase()) ||
      c.company?.toLowerCase().includes(searchQuery.toLowerCase())
  )

  const handleDelete = (id: string) => {
    setContacts(contacts.filter((c) => c.id !== id))
  }

  const handleAddContact = () => {
    if (newContact.name && newContact.email) {
      setContacts([
        ...contacts,
        {
          id: Math.random().toString(36).substr(2, 9),
          ...newContact,
          labels: [],
        },
      ])
      setNewContact({ name: "", email: "", phone: "", company: "" })
      setShowAddDialog(false)
    }
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
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <div className="relative">
            <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              className="w-64 pl-9"
              placeholder="Kişi ara..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
            />
          </div>
        </div>
        <Button onClick={() => setShowAddDialog(true)} className="gap-2">
          <Plus className="h-4 w-4" />
          Yeni Kişi
        </Button>
      </div>

      {/* Contacts List */}
      <div className="rounded-lg border bg-card">
        {loading ? (
          <div className="divide-y">
            {[1, 2, 3].map((i) => (
              <div key={i} className="flex items-center gap-4 p-4">
                <Skeleton className="h-10 w-10 rounded-full" />
                <div className="flex-1 space-y-2">
                  <Skeleton className="h-4 w-32" />
                  <Skeleton className="h-3 w-48" />
                </div>
              </div>
            ))}
          </div>
        ) : filteredContacts.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-16 text-center">
            <div className="rounded-full bg-muted p-4">
              <Mail className="h-8 w-8 text-muted-foreground" />
            </div>
            <h3 className="mt-4 text-lg font-semibold">Kişi bulunamadı</h3>
            <p className="text-sm text-muted-foreground">
              Arama kriterlerine uygun kişi yok.
            </p>
          </div>
        ) : (
          <div className="divide-y">
            {filteredContacts.map((contact) => (
              <div
                key={contact.id}
                className="group flex items-center gap-4 p-4 transition-colors hover:bg-accent/50"
              >
                <Avatar className="h-10 w-10">
                  <AvatarFallback className="bg-gradient-to-br from-primary to-primary/80 text-primary-foreground">
                    {getInitials(contact.name)}
                  </AvatarFallback>
                </Avatar>

                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="font-medium">{contact.name}</span>
                    {contact.labels.map((label) => (
                      <Badge
                        key={label}
                        variant="outline"
                        className="h-5 px-1.5 text-[10px]"
                      >
                        {label}
                      </Badge>
                    ))}
                  </div>
                  <div className="flex items-center gap-4 text-sm text-muted-foreground">
                    <span>{contact.email}</span>
                    {contact.phone && <span>{contact.phone}</span>}
                    {contact.company && <span>{contact.company}</span>}
                  </div>
                </div>

                <div className="flex items-center gap-2 opacity-0 group-hover:opacity-100 transition-opacity">
                  <Button variant="ghost" size="icon" className="h-8 w-8">
                    <Mail className="h-4 w-4" />
                  </Button>
                  <Button variant="ghost" size="icon" className="h-8 w-8">
                    <Edit className="h-4 w-4" />
                  </Button>
                  <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                      <Button variant="ghost" size="icon" className="h-8 w-8">
                        <MoreHorizontal className="h-4 w-4" />
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end">
                      <DropdownMenuItem
                        className="text-destructive"
                        onClick={() => handleDelete(contact.id)}
                      >
                        <Trash2 className="mr-2 h-4 w-4" />
                        Sil
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Pagination */}
      <div className="flex items-center justify-between">
        <span className="text-sm text-muted-foreground">
          {filteredContacts.length} kişi
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

      {/* Add Contact Dialog */}
      <Dialog open={showAddDialog} onOpenChange={setShowAddDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Yeni Kişi Ekle</DialogTitle>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <label className="text-sm font-medium">İsim</label>
              <Input
                placeholder="Ad Soyad"
                value={newContact.name}
                onChange={(e) =>
                  setNewContact({ ...newContact, name: e.target.value })
                }
              />
            </div>
            <div className="space-y-2">
              <label className="text-sm font-medium">E-posta</label>
              <Input
                type="email"
                placeholder="ornek@email.com"
                value={newContact.email}
                onChange={(e) =>
                  setNewContact({ ...newContact, email: e.target.value })
                }
              />
            </div>
            <div className="space-y-2">
              <label className="text-sm font-medium">Telefon</label>
              <Input
                placeholder="+90 555 123 4567"
                value={newContact.phone}
                onChange={(e) =>
                  setNewContact({ ...newContact, phone: e.target.value })
                }
              />
            </div>
            <div className="space-y-2">
              <label className="text-sm font-medium">Şirket</label>
              <Input
                placeholder="Şirket adı"
                value={newContact.company}
                onChange={(e) =>
                  setNewContact({ ...newContact, company: e.target.value })
                }
              />
            </div>
            <div className="flex justify-end gap-2">
              <Button variant="outline" onClick={() => setShowAddDialog(false)}>
                İptal
              </Button>
              <Button onClick={handleAddContact}>Ekle</Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}
