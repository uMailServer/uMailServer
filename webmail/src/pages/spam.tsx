import { useEffect, useRef, useState } from "react"
import { useNavigate } from "react-router-dom"
import {
  AlertCircle,
  Trash2,
  Archive,
  MoreHorizontal,
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
import api from "@/utils/api"
import { useMailFolder } from "@/hooks/useMailFolder"

export function SpamPage() {
  const navigate = useNavigate()
  const { emails, loading, error, reload, removeLocal } = useMailFolder("spam")
  const [busy, setBusy] = useState(false)
  const mountedRef = useRef(true)
  useEffect(() => {
    mountedRef.current = true
    return () => { mountedRef.current = false }
  }, [])
  const [selected, setSelected] = useState<Set<string>>(new Set())

  const toggleSelect = (id: string) => {
    const newSelected = new Set(selected)
    if (newSelected.has(id)) newSelected.delete(id)
    else newSelected.add(id)
    setSelected(newSelected)
  }

  const run = async (ids: string[], op: () => Promise<void>, msg: string) => {
    if (ids.length === 0 || busy) return
    setBusy(true)
    try {
      await op()
      if (!mountedRef.current) return
      removeLocal(ids)
      setSelected(new Set())
      toast.success(msg)
    } catch (err) {
      if (!mountedRef.current) return
      toast.error(err instanceof Error && err.message ? err.message : "Operation failed")
    } finally {
      if (mountedRef.current) setBusy(false)
    }
  }

  const label = (n: number) => `${n} message${n !== 1 ? "s" : ""}`

  const handleDelete = (ids: string[] = Array.from(selected)) =>
    run(ids, async () => { for (const id of ids) await api.deleteMail(id) }, `${label(ids.length)} permanently deleted`)

  const handleNotSpam = (ids: string[] = Array.from(selected)) =>
    run(ids, () => api.moveMail(ids, "spam", "inbox"), `${label(ids.length)} marked as not spam`)

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <AlertCircle className="h-5 w-5 text-red-500" />
          <h1 className="text-xl font-semibold">Spam</h1>
          {!loading && !error && <Badge variant="destructive">{emails.length}</Badge>}
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => handleNotSpam()}
            disabled={selected.size === 0 || loading || busy}
          >
            <Archive className="h-4 w-4 mr-1" />
            Not Spam
          </Button>
          <Button
            variant="outline"
            size="sm"
            className="text-destructive"
            onClick={() => handleDelete()}
            disabled={selected.size === 0 || loading || busy}
          >
            <Trash2 className="h-4 w-4 mr-1" />
            Delete
          </Button>
        </div>
      </div>

      <div className="rounded-lg border border-destructive/20 bg-destructive/10 p-4">
        <p className="text-sm text-destructive">
          Messages filtered as spam appear here.
          Use "Not Spam" to rescue legitimate emails.
        </p>
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
      ) : error ? (
        <div role="alert" className="flex flex-col items-center justify-center py-16 text-center">
          <h3 className="text-lg font-semibold">Could not load spam</h3>
          <p className="text-sm text-muted-foreground">{error}</p>
          <Button variant="outline" size="sm" className="mt-4" onClick={() => void reload()}>Retry</Button>
        </div>
      ) : emails.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-16 text-center">
          <div className="rounded-full bg-muted p-4">
            <AlertCircle className="h-8 w-8 text-muted-foreground" />
          </div>
          <h3 className="mt-4 text-lg font-semibold">No spam</h3>
          <p className="text-sm text-muted-foreground">
            Your spam folder is empty. All spam messages appear here.
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
              onClick={() => navigate(`/email/${encodeURIComponent(email.id)}`)}
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
                  <span className="font-medium">{email.fromName || email.from}</span>
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
                  <DropdownMenuItem onClick={(e) => { e.stopPropagation(); void handleNotSpam([email.id]) }}>
                    <Archive className="h-4 w-4 mr-2" />
                    Not Spam
                  </DropdownMenuItem>
                  <DropdownMenuItem onClick={(e) => { e.stopPropagation(); void handleDelete([email.id]) }} className="text-destructive">
                    <Trash2 className="h-4 w-4 mr-2" />
                    Delete
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
