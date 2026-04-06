import { useState } from "react"
import { useNavigate } from "react-router-dom"
import {
  AlertCircle,
  Mail,
  Trash2,
  Archive,
  RefreshCw,
  MoreHorizontal,
  Star,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"

interface SpamEmail {
  id: string
  from: string
  fromEmail: string
  subject: string
  preview: string
  date: string
  read: boolean
  spamScore: number
}

const mockSpamEmails: SpamEmail[] = [
  {
    id: "s1",
    from: "Casino Winner",
    fromEmail: "winner@casino-spam.com",
    subject: "Tebrikler! 1 Milyon Euro Kazandınız!",
    preview: "Bu epostayı aldığınız için çok şanslısınız...",
    date: "2 Nis",
    read: false,
    spamScore: 95,
  },
  {
    id: "s2",
    from: "Nigerian Prince",
    fromEmail: "prince@nigeria-fund.com",
    subject: " yardım",
    preview: "Sevgili dostum, büyük bir miras...",
    date: "1 Nis",
    read: false,
    spamScore: 88,
  },
  {
    id: "s3",
    from: "Fake Store",
    fromEmail: "deals@fake-store99.com",
    subject: "Saatlerde %90 İndirim!",
    preview: "Sadece bugün, tüm ürünlerde büyük indirim...",
    date: "31 Mar",
    read: true,
    spamScore: 72,
  },
]

export function SpamPage() {
  const navigate = useNavigate()
  const [loading, setLoading] = useState(false)
  const [emails] = useState<SpamEmail[]>(mockSpamEmails)
  const [selected, setSelected] = useState<Set<string>>(new Set())

  const toggleSelect = (id: string) => {
    const newSelected = new Set(selected)
    if (newSelected.has(id)) {
      newSelected.delete(id)
    } else {
      newSelected.add(id)
    }
    setSelected(newSelected)
  }

  const handleDelete = () => {
    // Simulate delete
    setLoading(true)
    setTimeout(() => setLoading(false), 500)
  }

  const handleNotSpam = () => {
    // Simulate mark as not spam
    setLoading(true)
    setTimeout(() => setLoading(false), 500)
  }

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <AlertCircle className="h-5 w-5 text-red-500" />
          <h1 className="text-xl font-semibold">Spam</h1>
          <Badge variant="destructive">{emails.length}</Badge>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={handleNotSpam}
            disabled={selected.size === 0 || loading}
          >
            <Archive className="h-4 w-4 mr-1" />
            Spam Değil
          </Button>
          <Button
            variant="outline"
            size="sm"
            className="text-destructive"
            onClick={handleDelete}
            disabled={selected.size === 0 || loading}
          >
            <Trash2 className="h-4 w-4 mr-1" />
            Sil
          </Button>
        </div>
      </div>

      {/* Info Banner */}
      <div className="rounded-lg border border-destructive/20 bg-destructive/10 p-4">
        <p className="text-sm text-destructive">
          Spam klasöründeki mesajlar otomatik olarak 30 gün sonra silinir.
          Gerçek spam olmayan mesajları "Spam Değil" butonuyla kurtarabilirsiniz.
        </p>
      </div>

      {/* Email List */}
      {loading ? (
        <div className="space-y-4">
          {[1, 2, 3].map((i) => (
            <div key={i} className="flex items-start gap-4 p-4 rounded-lg border">
              <Skeleton className="h-4 w-4" />
              <div className="flex-1 space-y-2">
                <Skeleton className="h-4 w-64" />
                <Skeleton className="h-3 w-full" />
              </div>
            </div>
          ))}
        </div>
      ) : emails.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-16 text-center">
          <div className="rounded-full bg-muted p-4">
            <AlertCircle className="h-8 w-8 text-muted-foreground" />
          </div>
          <h3 className="mt-4 text-lg font-semibold">Spam yok</h3>
          <p className="text-sm text-muted-foreground">
            Spam klasörünüz boş. Tüm spam mesajları burada görünür.
          </p>
        </div>
      ) : (
        <div className="rounded-lg border bg-card divide-y">
          {emails.map((email) => (
            <div
              key={email.id}
              className={cn(
                "flex items-start gap-3 p-4 cursor-pointer transition-colors hover:bg-accent/50",
                !email.read && "bg-accent/10"
              )}
              onClick={() => navigate(`/email/${email.id}`)}
            >
              <Checkbox
                className="mt-1"
                checked={selected.has(email.id)}
                onCheckedChange={() => toggleSelect(email.id)}
              />
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2">
                  {!email.read && (
                    <span className="h-2 w-2 rounded-full bg-red-500 shrink-0" />
                  )}
                  <span className="font-medium">{email.from}</span>
                  <Badge variant="destructive" className="text-[10px]">
                    %{email.spamScore}
                  </Badge>
                </div>
                <div className="text-sm">
                  <span className="font-medium">{email.subject}</span>
                  <span className="text-muted-foreground"> — {email.preview}</span>
                </div>
                <div className="text-xs text-muted-foreground mt-1">
                  {email.date}
                </div>
              </div>
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-8 w-8"
                    onClick={(e) => e.stopPropagation()}
                  >
                    <MoreHorizontal className="h-4 w-4" />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem onClick={() => handleNotSpam()}>
                    <Archive className="h-4 w-4 mr-2" />
                    Spam Değil
                  </DropdownMenuItem>
                  <DropdownMenuItem onClick={handleDelete} className="text-destructive">
                    <Trash2 className="h-4 w-4 mr-2" />
                    Sil
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
