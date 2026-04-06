import { useState } from "react"
import { useNavigate } from "react-router-dom"
import {
  Search,
  Mail,
  X,
  Filter,
  ChevronLeft,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Checkbox } from "@/components/ui/checkbox"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import { Separator } from "@/components/ui/separator"

interface SearchEmail {
  id: string
  from: string
  fromEmail: string
  subject: string
  preview: string
  date: string
  folder: string
  read: boolean
}

const mockSearchResults: SearchEmail[] = [
  {
    id: "1",
    from: "Ahmet Yılmaz",
    fromEmail: "ahmet@example.com",
    subject: "Proje Toplantısı Hakkında",
    preview: "Yarın saat 14:00'teki toplantıyı hatırlatmak istedim...",
    date: "4 Nis",
    folder: "Gelen Kutusu",
    read: false,
  },
  {
    id: "2",
    from: "Ayşe Demir",
    fromEmail: "ayse.demir@company.com",
    subject: "Fatura Onayı",
    preview: "Mart ayı fatura ödemeleri için onayınızı rica ediyorum...",
    date: "3 Nis",
    folder: "Gelen Kutusu",
    read: true,
  },
  {
    id: "3",
    from: "Tech Newsletter",
    fromEmail: "newsletter@tech.com",
    subject: "Haftalık Teknoloji Bülteni",
    preview: "Bu haftanın öne çıkan gelişmeleri: AI, cloud...",
    date: "2 Nis",
    folder: "Gelen Kutusu",
    read: true,
  },
]

export function SearchPage() {
  const navigate = useNavigate()
  const [query, setQuery] = useState("")
  const [loading, setLoading] = useState(false)
  const [hasSearched, setHasSearched] = useState(false)
  const [filters, setFilters] = useState({
    inbox: true,
    sent: true,
    drafts: true,
    archive: true,
    trash: false,
  })

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault()
    if (!query.trim()) return
    setLoading(true)
    setHasSearched(true)
    // Simulate search
    setTimeout(() => setLoading(false), 1000)
  }

  const results = hasSearched ? mockSearchResults.filter(
    (e) =>
      e.subject.toLowerCase().includes(query.toLowerCase()) ||
      e.from.toLowerCase().includes(query.toLowerCase()) ||
      e.preview.toLowerCase().includes(query.toLowerCase())
  ) : []

  return (
    <div className="space-y-4">
      {/* Search Header */}
      <div className="space-y-4">
        <form onSubmit={handleSearch} className="flex gap-2">
          <div className="relative flex-1">
            <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              className="pl-9"
              placeholder="E-postalarda, kişilerde veya dosyalarda ara..."
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              autoFocus
            />
          </div>
          <Button type="submit">Ara</Button>
        </form>

        {/* Filters */}
        <div className="flex items-center gap-4">
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Filter className="h-4 w-4" />
            <span>Filtreler:</span>
          </div>
          <div className="flex flex-wrap gap-2">
            <label className="flex items-center gap-2 cursor-pointer">
              <Checkbox
                checked={filters.inbox}
                onCheckedChange={(v) => setFilters({ ...filters, inbox: !!v })}
              />
              <span className="text-sm">Gelen Kutusu</span>
            </label>
            <label className="flex items-center gap-2 cursor-pointer">
              <Checkbox
                checked={filters.sent}
                onCheckedChange={(v) => setFilters({ ...filters, sent: !!v })}
              />
              <span className="text-sm">Gönderilenler</span>
            </label>
            <label className="flex items-center gap-2 cursor-pointer">
              <Checkbox
                checked={filters.drafts}
                onCheckedChange={(v) => setFilters({ ...filters, drafts: !!v })}
              />
              <span className="text-sm">Taslaklar</span>
            </label>
            <label className="flex items-center gap-2 cursor-pointer">
              <Checkbox
                checked={filters.archive}
                onCheckedChange={(v) => setFilters({ ...filters, archive: !!v })}
              />
              <span className="text-sm">Arşiv</span>
            </label>
            <label className="flex items-center gap-2 cursor-pointer">
              <Checkbox
                checked={filters.trash}
                onCheckedChange={(v) => setFilters({ ...filters, trash: !!v })}
              />
              <span className="text-sm">Çöp Kutusu</span>
            </label>
          </div>
        </div>
      </div>

      {/* Results */}
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
      ) : !hasSearched ? (
        <div className="flex flex-col items-center justify-center py-16 text-center">
          <div className="rounded-full bg-muted p-4">
            <Search className="h-8 w-8 text-muted-foreground" />
          </div>
          <h3 className="mt-4 text-lg font-semibold">E-posta arayın</h3>
          <p className="text-sm text-muted-foreground max-w-md">
            Konu, gönderen veya ileti içeriğinde arama yapın.
            Gelişmiş filtreler için seçenekleri kullanın.
          </p>
        </div>
      ) : results.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-16 text-center">
          <div className="rounded-full bg-muted p-4">
            <Mail className="h-8 w-8 text-muted-foreground" />
          </div>
          <h3 className="mt-4 text-lg font-semibold">Sonuç bulunamadı</h3>
          <p className="text-sm text-muted-foreground">
            "{query}" için sonuç bulunamadı. Farklı anahtar kelimeler deneyin.
          </p>
        </div>
      ) : (
        <div className="space-y-2">
          <div className="text-sm text-muted-foreground">
            {results.length} sonuç bulundu
          </div>
          <div className="rounded-lg border bg-card divide-y">
            {results.map((email) => (
              <div
                key={email.id}
                className={cn(
                  "flex items-start gap-3 p-4 cursor-pointer transition-colors hover:bg-accent/50",
                  !email.read && "bg-accent/10"
                )}
                onClick={() => navigate(`/email/${email.id}`)}
              >
                <Checkbox className="mt-1" />
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    {!email.read && (
                      <span className="h-2 w-2 rounded-full bg-primary shrink-0" />
                    )}
                    <span className="font-medium">{email.from}</span>
                    <Badge variant="outline" className="text-[10px]">
                      {email.folder}
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
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
