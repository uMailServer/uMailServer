import { useEffect, useRef, useState } from "react"
import { useNavigate } from "react-router-dom"
import {
  Star,
  Trash2,
  Paperclip,
  RefreshCw,
  ChevronLeft,
  ChevronRight,
  Filter,
  MoreHorizontal,
  List,
  LayoutGrid,
  ArrowUpDown,
} from "lucide-react"
import { WelcomeBanner } from "@/components/welcome-banner"
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
import { toast } from "sonner"
import api, { Mail } from "@/utils/api"
import { useMailFolder } from "@/hooks/useMailFolder"

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
  labels: string[]
}

function toRow(m: Mail): Email {
  return {
    id: m.id,
    from: m.fromName || m.from,
    fromEmail: m.from,
    subject: m.subject,
    preview: m.preview,
    date: m.date,
    read: m.read,
    starred: m.starred,
    hasAttachments: !!m.hasAttachments,
    labels: [],
  }
}

type ViewMode = "list" | "compact"
type SortOption = "date" | "from" | "subject"

interface InboxPageProps {
  folder?: string
}

export function InboxPage({ folder = "inbox" }: InboxPageProps) {
  const navigate = useNavigate()
  // "starred" is a view over the inbox: the API has no starred-folder route.
  const apiFolder = folder === "starred" ? "inbox" : folder
  const { emails: fetched, loading, error, reload, removeLocal } = useMailFolder(apiFolder)
  const emails = fetched
    .map(toRow)
    .filter((e) => folder !== "starred" || e.starred)
  const [selectedEmails, setSelectedEmails] = useState<Set<string>>(new Set())
  const [activeFilter, setActiveFilter] = useState("all")
  const [viewMode, setViewMode] = useState<ViewMode>("list")
  const [sortBy, setSortBy] = useState<SortOption>("date")
  const mountedRef = useRef(true)
  useEffect(() => {
    mountedRef.current = true
    return () => { mountedRef.current = false }
  }, [])
  useEffect(() => { setSelectedEmails(new Set()) }, [apiFolder])
  const [showWelcome, setShowWelcome] = useState(true)

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

  // The server exposes no flag/read endpoint, so star and read state are
  // display-only here; only move/delete are real operations.
  const handleRefresh = () => {
    void reload()
  }

  const moveSelected = async (ids: string[], to: string, doneMsg: string) => {
    if (ids.length === 0) return
    try {
      if (to === "") {
        for (const id of ids) await api.deleteMail(id)
      } else {
        await api.moveMail(ids, apiFolder, to)
      }
      if (!mountedRef.current) return
      removeLocal(ids)
      setSelectedEmails(new Set())
      toast.success(doneMsg)
    } catch (err) {
      if (!mountedRef.current) return
      toast.error(err instanceof Error && err.message ? err.message : "Operation failed")
    }
  }

  const handleDelete = (ids: string[] = Array.from(selectedEmails)) => {
    const n = ids.length
    const label = `${n} message${n !== 1 ? "s" : ""}`
    if (apiFolder === "trash") return moveSelected(ids, "", `${label} deleted`)
    return moveSelected(ids, "trash", `${label} moved to trash`)
  }

  const filteredEmails = emails
    .filter((email) => {
      if (activeFilter === "unread") return !email.read
      if (activeFilter === "starred") return email.starred
      return true
    })
    .sort((a, b) => {
      if (sortBy === "date") return 0 // Keep original order for date
      if (sortBy === "from") return a.from.localeCompare(b.from)
      if (sortBy === "subject") return a.subject.localeCompare(b.subject)
      return 0
    })

  const unreadCount = emails.filter((e) => !e.read).length

  const EmailRow = ({ email }: { email: Email }) => (
    <div
      className={cn(
        "group flex cursor-pointer items-center gap-3 transition-all duration-200",
        viewMode === "list" ? "p-4 hover:bg-accent/50" : "p-2 hover:bg-accent/50",
        !email.read && viewMode === "list" && "bg-accent/5",
        selectedEmails.has(email.id) && "bg-primary/5"
      )}
      onClick={() => navigate(`/email/${encodeURIComponent(email.id)}`)}
    >
      <Checkbox
        checked={selectedEmails.has(email.id)}
        onCheckedChange={() => toggleSelect(email.id)}
        onClick={(e) => e.stopPropagation()}
      />

      <Star className={cn("h-4 w-4 shrink-0", email.starred ? "fill-current text-amber-500" : "text-muted-foreground/40")} />

      <div className={cn("flex-1 min-w-0", viewMode === "compact" && "flex items-center gap-4")}>
        <div className="flex items-center gap-2">
          <span className={cn("text-sm", !email.read ? "font-semibold" : "font-normal")}>
            {viewMode === "list" ? email.from : email.from.split(" ")[0]}
          </span>
          {email.labels.slice(0, viewMode === "compact" ? 0 : 1).map((label) => (
            <Badge key={label} variant="secondary" className="text-[10px] px-1.5 py-0">
              {label}
            </Badge>
          ))}
        </div>
        {viewMode === "list" && (
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <span className={cn(!email.read && "text-foreground font-medium")}>
              {email.subject}
            </span>
            <span className="truncate">— {email.preview}</span>
          </div>
        )}
      </div>

      <div className={cn("flex items-center gap-2 shrink-0", viewMode === "compact" && "flex-row-reverse")}>
        {email.hasAttachments && (
          <Paperclip className="h-4 w-4 text-muted-foreground" />
        )}
        {!email.read && viewMode === "list" && (
          <span className="h-2 w-2 rounded-full bg-primary" />
        )}
        <span className={cn(
          "text-xs text-muted-foreground whitespace-nowrap",
          viewMode === "compact" && "w-12 text-right"
        )}>
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
            <DropdownMenuItem className="text-destructive" onClick={(e) => { e.stopPropagation(); void handleDelete([email.id]) }}>
              <Trash2 className="mr-2 h-4 w-4" />
              Delete
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  )

  return (
    <div className="space-y-4">
      {showWelcome && folder === "inbox" && (
        <WelcomeBanner onDismiss={() => setShowWelcome(false)} />
      )}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-2">
          <Checkbox
            checked={selectedEmails.size === emails.length && emails.length > 0}
            onCheckedChange={toggleSelectAll}
          />

          {selectedEmails.size > 0 ? (
            <div className="flex items-center gap-2 animate-in fade-in slide-in-from-left-2">
              <span className="text-sm text-muted-foreground">
                {selectedEmails.size} selected
              </span>
              <Separator orientation="vertical" className="h-4" />
              <Button variant="ghost" size="icon" className="h-8 w-8 text-destructive" onClick={() => handleDelete()} title="Delete">
                <Trash2 className="h-4 w-4" />
              </Button>
            </div>
          ) : (
            <Button variant="ghost" size="icon" className="h-8 w-8" onClick={handleRefresh}>
              <RefreshCw className={cn("h-4 w-4", loading && "animate-spin")} />
            </Button>
          )}

          {unreadCount > 0 && activeFilter === "all" && (
            <Badge variant="secondary" className="ml-2">
              {unreadCount} unread
            </Badge>
          )}
        </div>

        <div className="flex items-center gap-2">
          <Tabs value={activeFilter} onValueChange={setActiveFilter}>
            <TabsList>
              <TabsTrigger value="all">All</TabsTrigger>
              <TabsTrigger value="unread">Unread</TabsTrigger>
              <TabsTrigger value="starred">Starred</TabsTrigger>
            </TabsList>
          </Tabs>

          <Separator orientation="vertical" className="h-6" />

          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="icon" className="h-8 w-8" title="Sort">
                <ArrowUpDown className="h-4 w-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onClick={() => setSortBy("date")}>
                Date {sortBy === "date" && "✓"}
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => setSortBy("from")}>
                Sender {sortBy === "from" && "✓"}
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => setSortBy("subject")}>
                Subject {sortBy === "subject" && "✓"}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>

          <div className="flex border rounded-md">
            <Button
              variant={viewMode === "list" ? "secondary" : "ghost"}
              size="icon"
              className="h-8 w-8 rounded-r-none"
              onClick={() => setViewMode("list")}
            >
              <List className="h-4 w-4" />
            </Button>
            <Button
              variant={viewMode === "compact" ? "secondary" : "ghost"}
              size="icon"
              className="h-8 w-8 rounded-l-none"
              onClick={() => setViewMode("compact")}
            >
              <LayoutGrid className="h-4 w-4" />
            </Button>
          </div>
        </div>
      </div>

      <div className={cn(
        "rounded-lg border bg-card",
        viewMode === "compact" && "divide-y"
      )}>
        {loading ? (
          <div className={cn(viewMode === "list" ? "divide-y" : "")}>
            {[1, 2, 3, 4, 5].map((i) => (
              <div key={i} className={cn("flex items-start gap-4", viewMode === "list" ? "p-4" : "p-2")}>
                <Skeleton className="h-4 w-4" />
                <Skeleton className="h-4 w-4" />
                <div className="flex-1 space-y-2">
                  <Skeleton className="h-4 w-32" />
                  {viewMode === "list" && <Skeleton className="h-3 w-full" />}
                </div>
              </div>
            ))}
          </div>
        ) : error ? (
          <div role="alert" className="flex flex-col items-center justify-center py-16 text-center">
            <h3 className="text-lg font-semibold">Could not load messages</h3>
            <p className="text-sm text-muted-foreground">{error}</p>
            <Button variant="outline" size="sm" className="mt-4" onClick={handleRefresh}>Retry</Button>
          </div>
        ) : filteredEmails.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-16 text-center">
            <div className="rounded-full bg-muted p-4">
              <Filter className="h-8 w-8 text-muted-foreground" />
            </div>
            <h3 className="mt-4 text-lg font-semibold">No emails</h3>
            <p className="text-sm text-muted-foreground">
              {activeFilter === "unread" ? "No unread messages." : activeFilter === "starred" ? "No starred messages." : "Your inbox is empty."}
            </p>
          </div>
        ) : (
          <div className={cn(viewMode === "list" ? "divide-y" : "")}>
            {filteredEmails.map((email) => (
              <EmailRow key={email.id} email={email} />
            ))}
          </div>
        )}
      </div>

      <div className="flex items-center justify-between">
        <span className="text-sm text-muted-foreground">
          {filteredEmails.length} message{filteredEmails.length !== 1 ? "s" : ""}
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
