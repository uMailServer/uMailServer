import { useState } from "react"
import { useParams, useNavigate } from "react-router-dom"
import {
  FolderOpen,
  Trash2,
  MoreHorizontal,
  Star,
  ChevronLeft,
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
import { toast } from "sonner"

interface FolderEmail {
  id: string
  from: string
  fromEmail: string
  subject: string
  preview: string
  date: string
  read: boolean
  starred: boolean
}

const folderConfig: Record<string, { label: string; icon: string; color: string }> = {
  work: { label: "Work", icon: "💼", color: "text-blue-500" },
  personal: { label: "Personal", icon: "🏠", color: "text-green-500" },
}

const tagConfig: Record<string, { label: string; icon: string; color: string }> = {
  important: { label: "Important", icon: "⭐", color: "text-amber-500" },
}

const mockFolderEmails: FolderEmail[] = [
  {
    id: "f1",
    from: "HR Department",
    fromEmail: "hr@company.com",
    subject: "Annual Leave Planning",
    preview: "We need to plan your annual leave...",
    date: "Today",
    read: false,
    starred: true,
  },
  {
    id: "f2",
    from: "Accounting",
    fromEmail: "accounting@company.com",
    subject: "March Statement",
    preview: "Your March account statement is attached...",
    date: "Yesterday",
    read: true,
    starred: false,
  },
]

export function FolderPage() {
  const { type } = useParams()
  const navigate = useNavigate()
  const [loading, _setLoading] = useState(false)
  const [emails] = useState<FolderEmail[]>(mockFolderEmails)
  const [selected, setSelected] = useState<Set<string>>(new Set())

  const isTag = window.location.pathname.startsWith("/tag")
  const config = isTag ? tagConfig[type || ""] : folderConfig[type || ""]
  const pageTitle = config?.label || (isTag ? "Tag" : "Folder")
  const pageColor = config?.color || "text-muted-foreground"

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
    toast.success(`${selected.size} message${selected.size !== 1 ? "s" : ""} removed from folder`)
    setSelected(new Set())
  }

  if (!config) {
    return (
      <div className="flex flex-col items-center justify-center py-16 text-center">
        <FolderOpen className="h-12 w-12 text-muted-foreground" />
        <h3 className="mt-4 text-lg font-semibold">Folder not found</h3>
        <p className="text-sm text-muted-foreground">
          Folder "{type}" does not exist.
        </p>
        <Button variant="outline" className="mt-4" onClick={() => navigate("/inbox")}>
          <ChevronLeft className="h-4 w-4 mr-1" />
          Go Back
        </Button>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <FolderOpen className={cn("h-5 w-5", pageColor)} />
          <h1 className="text-xl font-semibold">{pageTitle}</h1>
          {config?.icon && <span className="text-xl">{config.icon}</span>}
          <Badge variant="secondary">{emails.length}</Badge>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            className="text-destructive"
            onClick={handleDelete}
            disabled={selected.size === 0 || loading}
          >
            <Trash2 className="h-4 w-4 mr-1" />
            Remove
          </Button>
        </div>
      </div>

      {loading ? (
        <div className="space-y-4">
          {[1, 2].map((i) => (
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
            <FolderOpen className="h-8 w-8 text-muted-foreground" />
          </div>
          <h3 className="mt-4 text-lg font-semibold">{pageTitle} is empty</h3>
          <p className="text-sm text-muted-foreground">
            No messages in this folder yet.
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
                  {email.starred && <Star className="h-4 w-4 fill-amber-400 text-amber-400" />}
                  {!email.read && (
                    <span className="h-2 w-2 rounded-full bg-primary shrink-0" />
                  )}
                  <span className="font-medium">{email.from}</span>
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
                  <DropdownMenuItem className="text-destructive">
                    <Trash2 className="h-4 w-4 mr-2" />
                    Remove from Folder
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
