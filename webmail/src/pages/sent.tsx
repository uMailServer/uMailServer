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

interface Email {
  id: string
  to: string
  toEmail: string
  subject: string
  preview: string
  date: string
  read: boolean
  starred: boolean
  hasAttachments: boolean
}

const mockSentEmails: Email[] = [
  {
    id: "s1",
    to: "Ahmet Yılmaz",
    toEmail: "ahmet@example.com",
    subject: "Re: Proje Toplantısı",
    preview: "Toplantıya katılabilirim, saat 14:00 uygun...",
    date: "11:30",
    read: true,
    starred: false,
    hasAttachments: false,
  },
  {
    id: "s2",
    to: "Tech Newsletter",
    toEmail: "newsletter@tech.com",
    subject: "Re: Abonelik",
    preview: "Bülteni takip etmeye devam etmek istiyorum...",
    date: "Dün",
    read: true,
    starred: false,
    hasAttachments: true,
  },
  {
    id: "s3",
    to: "Ayşe Demir",
    toEmail: "ayse.demir@company.com",
    subject: "Fatura Detayları",
    preview: "Mart ayı fatura bilgilerini ekte bulabilirsiniz...",
    date: "2 gün önce",
    read: true,
    starred: true,
    hasAttachments: false,
  },
]

export function SentPage() {
  const navigate = useNavigate()
  const [emails] = useState<Email[]>(mockSentEmails)
  const [selectedEmails, setSelectedEmails] = useState<Set<string>>(new Set())
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

  return (
    <div className="space-y-4">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-2">
          <Checkbox
            checked={selectedEmails.size === emails.length && emails.length > 0}
            onCheckedChange={toggleSelectAll}
          />
          {selectedEmails.size > 0 && (
            <>
              <span className="text-sm text-muted-foreground">
                {selectedEmails.size} seçildi
              </span>
              <Separator orientation="vertical" className="h-4" />
              <Button variant="ghost" size="icon" className="h-8 w-8">
                <Trash2 className="h-4 w-4" />
              </Button>
            </>
          )}
        </div>
        <Button
          variant="ghost"
          size="icon"
          className="h-8 w-8"
          onClick={() => setLoading(true)}
        >
          <RefreshCw className={cn("h-4 w-4", loading && "animate-spin")} />
        </Button>
      </div>

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
        ) : emails.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-16 text-center">
            <div className="rounded-full bg-muted p-4">
              <Mail className="h-8 w-8 text-muted-foreground" />
            </div>
            <h3 className="mt-4 text-lg font-semibold">Gönderilen ileti yok</h3>
            <p className="text-sm text-muted-foreground">
              Gönderdiğiniz e-postalar burada görünür.
            </p>
          </div>
        ) : (
          <div className="divide-y">
            {emails.map((email) => (
              <div
                key={email.id}
                className={cn(
                  "group flex cursor-pointer items-start gap-3 p-4 transition-colors hover:bg-accent/50"
                )}
                onClick={() => navigate(`/email/${email.id}`)}
              >
                <div className="flex items-center gap-2 pt-1">
                  <Checkbox
                    checked={selectedEmails.has(email.id)}
                    onCheckedChange={() => toggleSelect(email.id)}
                    onClick={(e) => e.stopPropagation()}
                  />
                </div>

                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="truncate font-medium">{email.to}</span>
                  </div>
                  <div className="flex items-center gap-2 text-sm text-muted-foreground">
                    <span className="truncate font-medium">{email.subject}</span>
                    <span className="truncate">— {email.preview}</span>
                  </div>
                </div>

                <div className="flex items-center gap-2">
                  {email.hasAttachments && (
                    <Paperclip className="h-4 w-4 shrink-0 text-muted-foreground" />
                  )}
                  <span className="whitespace-nowrap text-sm text-muted-foreground">
                    {email.date}
                  </span>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      <div className="flex items-center justify-between">
        <span className="text-sm text-muted-foreground">
          {emails.length} ileti
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
    </div>
  )
}
