import { useState } from "react"
import { useNavigate } from "react-router-dom"
import {
  Star,
  Archive,
  Trash2,
  MailOpen,
  Mail,
  Paperclip,
  RefreshCw,
  ChevronLeft,
  ChevronRight,
  Filter,
  MoreHorizontal,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import { Separator } from "@/components/ui/separator"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"

interface Email {
  id: string
  from: string
  fromEmail: string
  subject: string
  preview: string
  date: string
  read: boolean
  starred: boolean
  hasAttachments: boolean
  folder: string
  labels: string[]
}

const mockEmails: Email[] = [
  {
    id: "1",
    from: "Ahmet Yılmaz",
    fromEmail: "ahmet@example.com",
    subject: "Proje Toplantısı Hakkında",
    preview: "Yarın saat 14:00'teki toplantıyı hatırlatmak istedim. Gündemde önemli konular var...",
    date: "10:30",
    read: false,
    starred: true,
    hasAttachments: true,
    folder: "inbox",
    labels: ["iş", "önemli"],
  },
  {
    id: "2",
    from: "Tech Newsletter",
    fromEmail: "newsletter@tech.com",
    subject: "Haftalık Teknoloji Bülteni",
    preview: "Bu haftanın öne çıkan gelişmeleri: AI, cloud computing ve daha fazlası...",
    date: "09:15",
    read: true,
    starred: false,
    hasAttachments: false,
    folder: "inbox",
    labels: [],
  },
  {
    id: "3",
    from: "Ayşe Demir",
    fromEmail: "ayse.demir@company.com",
    subject: "Fatura Onayı",
    preview: "Mart ayı fatura ödemeleri için onayınızı rica ediyorum. Ekte detaylar...",
    date: "Dün",
    read: false,
    starred: false,
    hasAttachments: true,
    folder: "inbox",
    labels: ["fatura"],
  },
  {
    id: "4",
    from: "Sistem",
    fromEmail: "noreply@umailserver.com",
    subject: "Güvenlik Uyarısı",
    preview: "Hesabınıza yeni bir cihazdan erişim sağlandı. Bu siz değilseniz lütfen...",
    date: "Dün",
    read: true,
    starred: false,
    hasAttachments: false,
    folder: "inbox",
    labels: ["güvenlik"],
  },
  {
    id: "5",
    from: "Mehmet Kaya",
    fromEmail: "mehmet.k@gmail.com",
    subject: "Yılbaşı Partisi",
    preview: "Herkesi cumartesi akşamı evimdeki partiye davet ediyorum. Katılabilir misiniz?",
    date: "2 gün önce",
    read: true,
    starred: true,
    hasAttachments: false,
    folder: "inbox",
    labels: ["kişisel"],
  },
]

interface InboxPageProps {
  folder?: string
}

export function InboxPage({ folder = "inbox" }: InboxPageProps) {
  const navigate = useNavigate()
  const [emails, setEmails] = useState<Email[]>(
    mockEmails.filter((e) => e.folder === folder || (folder === "starred" && e.starred))
  )
  const [selectedEmails, setSelectedEmails] = useState<Set<string>>(new Set())
  const [activeFilter, setActiveFilter] = useState("all")
  const [loading, setLoading] = useState(false)

  const toggleSelectAll = () => {
    if (selectedEmails.size === emails.length) {
      setSelectedEmails(new Set())
    } else {
      setSelectedEmails(new Set(emails.map((e) => e.id)))
    }
  }

  const toggleSelect = (id: string) => {
    const newSelected = new Set(selectedEmails)
    if (newSelected.has(id)) {
      newSelected.delete(id)
    } else {
      newSelected.add(id)
    }
    setSelectedEmails(newSelected)
  }

  const toggleStar = (id: string, e: React.MouseEvent) => {
    e.stopPropagation()
    setEmails(emails.map((email) =>
      email.id === id ? { ...email, starred: !email.starred } : email
    ))
  }

  const markAsRead = (id: string, e: React.MouseEvent) => {
    e.stopPropagation()
    setEmails(emails.map((email) =>
      email.id === id ? { ...email, read: true } : email
    ))
  }

  const handleRefresh = () => {
    setLoading(true)
    setTimeout(() => setLoading(false), 1000)
  }

  const handleArchive = () => {
    // Simulate archive
    setSelectedEmails(new Set())
  }

  const handleDelete = () => {
    // Simulate delete - move to trash
    setEmails(emails.filter((e) => !selectedEmails.has(e.id)))
    setSelectedEmails(new Set())
  }

  const handleMarkRead = () => {
    setEmails(emails.map((e) =>
      selectedEmails.has(e.id) ? { ...e, read: true } : e
    ))
    setSelectedEmails(new Set())
  }

  const handleMarkUnread = () => {
    setEmails(emails.map((e) =>
      selectedEmails.has(e.id) ? { ...e, read: false } : e
    ))
    setSelectedEmails(new Set())
  }

  const filteredEmails = emails.filter((email) => {
    if (activeFilter === "unread") return !email.read
    if (activeFilter === "starred") return email.starred
    return true
  })

  return (
    <div className="space-y-4">
      {/* Toolbar */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-2">
          <Checkbox
            checked={selectedEmails.size === emails.length && emails.length > 0}
            onCheckedChange={toggleSelectAll}
          />

          {selectedEmails.size > 0 ? (
            <div className="flex items-center gap-2">
              <span className="text-sm text-muted-foreground">
                {selectedEmails.size} seçildi
              </span>
              <Separator orientation="vertical" className="h-4" />
              <Button variant="ghost" size="icon" className="h-8 w-8" onClick={handleArchive} title="Arşivle">
                <Archive className="h-4 w-4" />
              </Button>
              <Button variant="ghost" size="icon" className="h-8 w-8 text-destructive" onClick={handleDelete} title="Sil">
                <Trash2 className="h-4 w-4" />
              </Button>
              <Button variant="ghost" size="icon" className="h-8 w-8" onClick={handleMarkRead} title="Okundu işaretle">
                <MailOpen className="h-4 w-4" />
              </Button>
            </div>
          ) : (
            <Button variant="ghost" size="icon" className="h-8 w-8" onClick={handleRefresh}>
              <RefreshCw className={cn("h-4 w-4", loading && "animate-spin")} />
            </Button>
          )}
        </div>

        <div className="flex items-center gap-2">
          <Tabs value={activeFilter} onValueChange={setActiveFilter}>
            <TabsList className="h-8">
              <TabsTrigger value="all" className="text-xs">Tümü</TabsTrigger>
              <TabsTrigger value="unread" className="text-xs">
                Okunmamış
                <Badge variant="secondary" className="ml-1 h-4 px-1 text-[10px]">
                  {emails.filter((e) => !e.read).length}
                </Badge>
              </TabsTrigger>
              <TabsTrigger value="starred" className="text-xs">Yıldızlı</TabsTrigger>
            </TabsList>
          </Tabs>

          <Button variant="ghost" size="icon" className="h-8 w-8">
            <Filter className="h-4 w-4" />
          </Button>

          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="icon" className="h-8 w-8">
                <MoreHorizontal className="h-4 w-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem>Tümünü okundu işaretle</DropdownMenuItem>
              <DropdownMenuItem>Tümünü arşivle</DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>

      {/* Email List */}
      <div className="rounded-lg border bg-card">
        {loading ? (
          <div className="divide-y">
            {[1, 2, 3].map((i) => (
              <div key={i} className="flex items-center gap-4 p-4">
                <Skeleton className="h-4 w-4" />
                <Skeleton className="h-4 w-4" />
                <Skeleton className="h-8 w-8 rounded-full" />
                <div className="flex-1 space-y-2">
                  <Skeleton className="h-4 w-32" />
                  <Skeleton className="h-3 w-full" />
                </div>
              </div>
            ))}
          </div>
        ) : filteredEmails.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-16 text-center">
            <div className="rounded-full bg-muted p-4">
              <Mail className="h-8 w-8 text-muted-foreground" />
            </div>
            <h3 className="mt-4 text-lg font-semibold">E-posta yok</h3>
            <p className="text-sm text-muted-foreground">
              Bu klasörde e-posta bulunmuyor.
            </p>
          </div>
        ) : (
          <div className="divide-y">
            {filteredEmails.map((email) => (
              <div
                key={email.id}
                className={cn(
                  "group flex cursor-pointer items-start gap-3 p-4 transition-colors hover:bg-accent/50",
                  !email.read && "bg-accent/20",
                  selectedEmails.has(email.id) && "bg-accent"
                )}
                onClick={() => navigate(`/email/${email.id}`)}
              >
                <div className="flex items-center gap-2 pt-1">
                  <Checkbox
                    checked={selectedEmails.has(email.id)}
                    onCheckedChange={() => toggleSelect(email.id)}
                    onClick={(e) => e.stopPropagation()}
                  />
                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-6 w-6"
                    onClick={(e) => toggleStar(email.id, e)}
                  >
                    <Star
                      className={cn(
                        "h-4 w-4",
                        email.starred
                          ? "fill-amber-400 text-amber-400"
                          : "text-muted-foreground opacity-0 group-hover:opacity-100"
                      )}
                    />
                  </Button>
                </div>

                <div className="flex-1 min-w-0 space-y-1">
                  <div className="flex items-center gap-2">
                    <span
                      className={cn(
                        "truncate font-medium",
                        !email.read && "font-semibold"
                      )}
                    >
                      {email.from}
                    </span>
                    {email.labels.length > 0 && (
                      <div className="flex items-center gap-1">
                        {email.labels.map((label) => (
                          <Badge
                            key={label}
                            variant="outline"
                            className="h-4 px-1 text-[10px] capitalize"
                          >
                            {label}
                          </Badge>
                        ))}
                      </div>
                    )}
                  </div>
                  <div className="flex items-center gap-2">
                    {!email.read && (
                      <span className="h-2 w-2 shrink-0 rounded-full bg-primary" />
                    )}
                    <span
                      className={cn(
                        "truncate",
                        !email.read && "font-medium"
                      )}
                    >
                      {email.subject}
                    </span>
                    <span className="truncate text-muted-foreground">
                      — {email.preview}
                    </span>
                  </div>
                </div>

                <div className="flex items-center gap-2">
                  {email.hasAttachments && (
                    <Paperclip className="h-4 w-4 shrink-0 text-muted-foreground" />
                  )}
                  <span className="whitespace-nowrap text-sm text-muted-foreground">
                    {email.date}
                  </span>
                  <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-8 w-8 opacity-0 group-hover:opacity-100"
                        onClick={(e) => e.stopPropagation()}
                      >
                        <MoreHorizontal className="h-4 w-4" />
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end">
                      <DropdownMenuItem onClick={(e) => markAsRead(email.id, e)}>
                        <MailOpen className="mr-2 h-4 w-4" />
                        Okundu işaretle
                      </DropdownMenuItem>
                      <DropdownMenuItem>
                        <Archive className="mr-2 h-4 w-4" />
                        Arşivle
                      </DropdownMenuItem>
                      <DropdownMenuItem className="text-destructive">
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
          1-5 / 128 e-posta
        </span>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="icon" disabled>
            <ChevronLeft className="h-4 w-4" />
          </Button>
          <Button variant="outline" size="icon">
            <ChevronRight className="h-4 w-4" />
          </Button>
        </div>
      </div>
    </div>
  )
}
