import { useState, useEffect } from "react"
import { useNavigate, useSearchParams } from "react-router-dom"
import {
  Search,
  Mail,
  X,
  Filter,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Checkbox } from "@/components/ui/checkbox"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"

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
    from: "John Smith",
    fromEmail: "john@example.com",
    subject: "Project Meeting Discussion",
    preview: "I wanted to remind you about the meeting tomorrow at 2pm...",
    date: "Apr 4",
    folder: "Inbox",
    read: false,
  },
  {
    id: "2",
    from: "Sarah Johnson",
    fromEmail: "sarah.johnson@company.com",
    subject: "Invoice Approval",
    preview: "Please approve the invoice payment for March...",
    date: "Apr 3",
    folder: "Inbox",
    read: true,
  },
  {
    id: "3",
    from: "Tech Newsletter",
    fromEmail: "newsletter@tech.com",
    subject: "Weekly Tech Digest",
    preview: "This week's top stories: AI, cloud computing...",
    date: "Apr 2",
    folder: "Inbox",
    read: true,
  },
]

export function SearchPage() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const [query, setQuery] = useState(searchParams.get("q") || "")
  const [loading, setLoading] = useState(false)
  const [hasSearched, setHasSearched] = useState(false)
  const [filters, setFilters] = useState({
    inbox: true,
    sent: true,
    drafts: true,
    archive: true,
    trash: false,
  })

  useEffect(() => {
    const q = searchParams.get("q")
    if (q && q !== query) {
      setQuery(q)
      setHasSearched(true)
      setLoading(true)
      setTimeout(() => setLoading(false), 500)
    }
  }, [searchParams])

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault()
    if (!query.trim()) return
    setLoading(true)
    setHasSearched(true)
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
      <div className="space-y-4">
        <form onSubmit={handleSearch} className="flex gap-2">
          <div className="relative flex-1">
            <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              className="pl-9"
              placeholder="Search emails, contacts, or files..."
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              autoFocus
            />
          </div>
          <Button type="submit">Search</Button>
        </form>

        <div className="flex items-center gap-4">
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Filter className="h-4 w-4" />
            <span>Filters:</span>
          </div>
          <div className="flex flex-wrap gap-2">
            <label className="flex items-center gap-2 cursor-pointer">
              <Checkbox
                checked={filters.inbox}
                onCheckedChange={(v) => setFilters({ ...filters, inbox: !!v })}
              />
              <span className="text-sm">Inbox</span>
            </label>
            <label className="flex items-center gap-2 cursor-pointer">
              <Checkbox
                checked={filters.sent}
                onCheckedChange={(v) => setFilters({ ...filters, sent: !!v })}
              />
              <span className="text-sm">Sent</span>
            </label>
            <label className="flex items-center gap-2 cursor-pointer">
              <Checkbox
                checked={filters.drafts}
                onCheckedChange={(v) => setFilters({ ...filters, drafts: !!v })}
              />
              <span className="text-sm">Drafts</span>
            </label>
            <label className="flex items-center gap-2 cursor-pointer">
              <Checkbox
                checked={filters.archive}
                onCheckedChange={(v) => setFilters({ ...filters, archive: !!v })}
              />
              <span className="text-sm">Archive</span>
            </label>
            <label className="flex items-center gap-2 cursor-pointer">
              <Checkbox
                checked={filters.trash}
                onCheckedChange={(v) => setFilters({ ...filters, trash: !!v })}
              />
              <span className="text-sm">Trash</span>
            </label>
          </div>
        </div>
      </div>

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
          <h3 className="mt-4 text-lg font-semibold">Search emails</h3>
          <p className="text-sm text-muted-foreground max-w-md">
            Search by subject, sender, or message content.
            Use filters to narrow down results.
          </p>
        </div>
      ) : results.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-16 text-center">
          <div className="rounded-full bg-muted p-4">
            <Mail className="h-8 w-8 text-muted-foreground" />
          </div>
          <h3 className="mt-4 text-lg font-semibold">No results found</h3>
          <p className="text-sm text-muted-foreground">
            No results for "{query}". Try different keywords.
          </p>
        </div>
      ) : (
        <div className="space-y-2">
          <div className="text-sm text-muted-foreground">
            {results.length} result{results.length !== 1 ? "s" : ""} found
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
