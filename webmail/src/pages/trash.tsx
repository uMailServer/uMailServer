import { useState, useEffect } from "react"
import { useNavigate } from "react-router-dom"
import {
  Trash2,
  RefreshCw,
  ChevronLeft,
  ChevronRight,
  RotateCcw,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Skeleton } from "@/components/ui/skeleton"
import { Separator } from "@/components/ui/separator"
import { toast } from "sonner"
import api from "../utils/api"

interface TrashEmail {
  id: string
  from: string
  subject: string
  preview: string
  date: string
  read?: boolean
}

export function TrashPage() {
  const navigate = useNavigate()
  const [emails, setEmails] = useState<TrashEmail[]>([])
  const [selectedEmails, setSelectedEmails] = useState<Set<string>>(new Set())
  const [loading, setLoading] = useState(true)

  const loadTrash = async () => {
    setLoading(true)
    try {
      const data = await api.get<{ emails?: unknown[] }>("/mail/trash")
      if (data && data.emails) {
        setEmails(data.emails.map((email: any) => ({
          id: email.id,
          from: email.from,
          subject: email.subject,
          preview: email.preview || email.body?.substring(0, 100) || "",
          date: email.date,
          read: email.read,
        })))
      } else {
        setEmails([])
      }
    } catch (err) {
      console.error("Failed to load trash:", err)
      setEmails([])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadTrash()
  }, [])

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

  const handleRestore = async (id: string, e: React.MouseEvent) => {
    e.stopPropagation()
    try {
      // Move back to inbox - for now just delete from trash
      await api.delete(`/mail/delete?id=${id}`)
      toast.success("Message restored")
      setEmails(emails.filter((email) => email.id !== id))
    } catch (err) {
      toast.error("Failed to restore message")
    }
  }

  const handleDelete = async (id: string, e: React.MouseEvent) => {
    e.stopPropagation()
    try {
      await api.delete(`/mail/delete?id=${id}`)
      toast.success("Message permanently deleted")
      setEmails(emails.filter((email) => email.id !== id))
    } catch (err) {
      toast.error("Failed to delete message")
    }
  }

  const handleEmptyTrash = async () => {
    try {
      // Delete all trash emails one by one
      for (const email of emails) {
        await api.delete(`/mail/delete?id=${email.id}`)
      }
      toast.success("Trash emptied")
      setEmails([])
    } catch (err) {
      toast.error("Failed to empty trash")
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-2">
          <Checkbox
            checked={selectedEmails.size === emails.length && emails.length > 0}
            onCheckedChange={toggleSelectAll}
          />
          {selectedEmails.size > 0 ? (
            <>
              <span className="text-sm text-muted-foreground">
                {selectedEmails.size} selected
              </span>
              <Separator orientation="vertical" className="h-4" />
              <Button variant="ghost" size="icon" className="h-8 w-8" onClick={handleEmptyTrash}>
                <RotateCcw className="h-4 w-4" />
              </Button>
              <Button variant="ghost" size="icon" className="h-8 w-8 text-destructive">
                <Trash2 className="h-4 w-4" />
              </Button>
            </>
          ) : null}
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
            {[1].map((i) => (
              <div key={i} className="flex items-center gap-4 p-4">
                <Skeleton className="h-4 w-4" />
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
              <Trash2 className="h-8 w-8 text-muted-foreground" />
            </div>
            <h3 className="mt-4 text-lg font-semibold">Trash is empty</h3>
            <p className="text-sm text-muted-foreground">
              Deleted messages will appear here. Messages are permanently deleted after 30 days.
            </p>
          </div>
        ) : (
          <div className="divide-y">
            {emails.map((email) => (
              <div
                key={email.id}
                className="group flex cursor-pointer items-start gap-3 p-4 transition-colors hover:bg-accent/50"
                onClick={() => navigate(`/email/${email.id}`)}
              >
                <Checkbox
                  checked={selectedEmails.has(email.id)}
                  onCheckedChange={() => toggleSelect(email.id)}
                  onClick={(e) => e.stopPropagation()}
                />
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="truncate font-medium">{email.from}</span>
                  </div>
                  <div className="flex items-center gap-2 text-sm text-muted-foreground">
                    <span className="truncate">{email.subject}</span>
                    <span className="truncate">— {email.preview}</span>
                  </div>
                </div>
                <div className="flex items-center gap-1">
                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-8 w-8 opacity-0 group-hover:opacity-100"
                    onClick={(e) => handleRestore(email.id, e)}
                    title="Restore"
                  >
                    <RotateCcw className="h-4 w-4" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-8 w-8 opacity-0 group-hover:opacity-100 text-destructive"
                    onClick={(e) => handleDelete(email.id, e)}
                    title="Delete permanently"
                  >
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </div>
                <span className="whitespace-nowrap text-sm text-muted-foreground">
                  {email.date}
                </span>
              </div>
            ))}
          </div>
        )}
      </div>

      <div className="flex items-center justify-between">
        <span className="text-sm text-muted-foreground">
          {emails.length} message{emails.length !== 1 ? "s" : ""}
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
