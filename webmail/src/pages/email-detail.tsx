import { useParams, useNavigate } from "react-router-dom"
import {
  ArrowLeft,
  Reply,
  Forward,
  Archive,
  Trash2,
  MoreHorizontal,
  Paperclip,
  Star,
  Printer,
  Download,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { Separator } from "@/components/ui/separator"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Badge } from "@/components/ui/badge"

interface Attachment {
  name: string
  size: string
  type: string
}

interface EmailDetail {
  id: string
  from: string
  fromEmail: string
  to: string[]
  cc?: string[]
  subject: string
  date: string
  content: string
  starred: boolean
  attachments?: Attachment[]
}

const mockEmailDetail: EmailDetail = {
  id: "1",
  from: "Ahmet Yılmaz",
  fromEmail: "ahmet@example.com",
  to: ["user@example.com"],
  cc: ["mehmet@example.com"],
  subject: "Proje Toplantısı Hakkında",
  date: "4 Nisan 2025, 10:30",
  starred: true,
  content: `
    <p>Merhaba,</p>

    <p>Yarın saat 14:00'teki toplantıyı hatırlatmak istedim. Gündemde önemli konular var:</p>

    <ul>
      <li>Q1 raporunun değerlendirilmesi</li>
      <li>Yeni proje planlaması</li>
      <li>Bütçe revizyonu</li>
    </ul>

    <p>Lütfen hazırlıklı gelin. Ekte gerekli dosyaları bulabilirsiniz.</p>

    <p>İyi çalışmalar,<br>Ahmet</p>
  `,
  attachments: [
    { name: "Q1_Raporu.pdf", size: "2.4 MB", type: "pdf" },
    { name: "Proje_Plani.xlsx", size: "156 KB", type: "xlsx" },
  ],
}

export function EmailDetailPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const email = mockEmailDetail

  const handleArchive = () => {
    // Simulate archive action
    console.log("Archive email:", id)
  }

  const handleDelete = () => {
    // Simulate delete action - go to trash
    navigate("/trash")
  }

  return (
    <div className="space-y-4">
      {/* Toolbar */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="icon" onClick={() => navigate(-1)}>
            <ArrowLeft className="h-5 w-5" />
          </Button>
          <Button variant="ghost" size="icon" onClick={handleArchive}>
            <Archive className="h-5 w-5" />
          </Button>
          <Button variant="ghost" size="icon" className="text-destructive" onClick={handleDelete}>
            <Trash2 className="h-5 w-5" />
          </Button>
        </div>

        <div className="flex items-center gap-2">
          <Button variant="ghost" size="icon">
            <Printer className="h-5 w-5" />
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="icon">
                <MoreHorizontal className="h-5 w-5" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem>Kaynak kodunu göster</DropdownMenuItem>
              <DropdownMenuItem>Yeni sekmede aç</DropdownMenuItem>
              <DropdownMenuItem>İleti başlığını görüntüle</DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>

      {/* Email Content */}
      <div className="rounded-lg border bg-card p-6">
        {/* Header */}
        <div className="space-y-4">
          <div className="flex items-start justify-between gap-4">
            <h1 className="text-xl font-semibold">{email.subject}</h1>
            <Button variant="ghost" size="icon">
              <Star
                className={`h-5 w-5 ${
                  email.starred
                    ? "fill-amber-400 text-amber-400"
                    : "text-muted-foreground"
                }`}
              />
            </Button>
          </div>

          <div className="flex items-start gap-4">
            <Avatar className="h-10 w-10">
              <AvatarFallback className="bg-gradient-to-br from-primary to-primary/80 text-primary-foreground">
                {email.from
                  .split(" ")
                  .map((n) => n[0])
                  .join("")}
              </AvatarFallback>
            </Avatar>

            <div className="flex-1 min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <span className="font-semibold">{email.from}</span>
                <span className="text-sm text-muted-foreground">
                  &lt;{email.fromEmail}&gt;
                </span>
              </div>

              <div className="mt-1 text-sm text-muted-foreground">
                <span>Kime: {email.to.join(", ")}</span>
                {email.cc && (
                  <span className="ml-2">Cc: {email.cc.join(", ")}</span>
                )}
              </div>

              <div className="mt-1 text-sm text-muted-foreground">
                {email.date}
              </div>
            </div>
          </div>
        </div>

        <Separator className="my-6" />

        {/* Body */}
        <div
          className="prose prose-sm max-w-none dark:prose-invert"
          dangerouslySetInnerHTML={{ __html: email.content }}
        />

        {/* Attachments */}
        {email.attachments && email.attachments.length > 0 && (
          <>
            <Separator className="my-6" />
            <div className="space-y-3">
              <h3 className="text-sm font-semibold">
                Ekler ({email.attachments.length})
              </h3>
              <div className="flex flex-wrap gap-3">
                {email.attachments.map((attachment) => (
                  <div
                    key={attachment.name}
                    className="flex items-center gap-3 rounded-lg border p-3 hover:bg-accent cursor-pointer"
                  >
                    <Paperclip className="h-5 w-5 text-muted-foreground" />
                    <div>
                      <p className="text-sm font-medium">{attachment.name}</p>
                      <p className="text-xs text-muted-foreground">
                        {attachment.size}
                      </p>
                    </div>
                    <Button variant="ghost" size="icon" className="h-8 w-8">
                      <Download className="h-4 w-4" />
                    </Button>
                  </div>
                ))}
              </div>
            </div>
          </>
        )}
      </div>

      {/* Reply Actions */}
      <div className="flex items-center gap-2">
        <Button
          className="gap-2"
          onClick={() =>
            navigate(`/compose?replyTo=${encodeURIComponent(email.fromEmail)}&subject=${encodeURIComponent("Re: " + email.subject)}`)
          }
        >
          <Reply className="h-4 w-4" />
          Yanıtla
        </Button>
        <Button
          variant="outline"
          className="gap-2"
          onClick={() =>
            navigate(`/compose?forward=true&subject=${encodeURIComponent("Fwd: " + email.subject)}`)
          }
        >
          <Forward className="h-4 w-4" />
          İlet
        </Button>
      </div>
    </div>
  )
}
