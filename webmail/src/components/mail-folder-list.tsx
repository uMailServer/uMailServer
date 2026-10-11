import { useEffect, useRef, useState } from "react"
import { useNavigate } from "react-router-dom"
import { RefreshCw, Trash2, RotateCcw, Paperclip, type LucideIcon } from "lucide-react"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Skeleton } from "@/components/ui/skeleton"
import { toast } from "sonner"
import api from "@/utils/api"
import { useMailFolder } from "@/hooks/useMailFolder"

interface Props {
  folder: "sent" | "drafts" | "trash"
  icon: LucideIcon
  emptyTitle: string
  emptyText: string
  /** Show recipients instead of the sender (sent / drafts). */
  showRecipients?: boolean
  /** Restore-to-inbox control (trash only). */
  restoreTo?: string
}

const label = (n: number) => `${n} message${n !== 1 ? "s" : ""}`
const errMsg = (e: unknown) => (e instanceof Error && e.message ? e.message : "Operation failed")

/** Real-API folder list shared by Sent, Drafts and Trash. */
export function MailFolderList({ folder, icon: Icon, emptyTitle, emptyText, showRecipients, restoreTo }: Props) {
  const navigate = useNavigate()
  const { emails, total, hasMore, loading, loadingMore, error, reload, loadMore, removeLocal } = useMailFolder(folder)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [busy, setBusy] = useState(false)
  const mounted = useRef(true)
  useEffect(() => {
    mounted.current = true
    return () => { mounted.current = false }
  }, [])
  // Drop selections that no longer exist after a reload.
  useEffect(() => {
    setSelected((prev) => {
      const ids = new Set(emails.map((e) => e.id))
      const next = new Set(Array.from(prev).filter((id) => ids.has(id)))
      return next.size === prev.size ? prev : next
    })
  }, [emails])

  const toggle = (id: string) =>
    setSelected((prev) => {
      const n = new Set(prev)
      if (n.has(id)) n.delete(id)
      else n.add(id)
      return n
    })

  const run = async (ids: string[], op: (id: string, done: string[]) => Promise<void>, ok: string) => {
    if (ids.length === 0 || busy) return
    setBusy(true)
    const done: string[] = []
    let failure: unknown = null
    try {
      await op("", done)
    } catch (e) {
      failure = e
    }
    if (!mounted.current) return
    if (done.length) removeLocal(done)
    setSelected(new Set())
    setBusy(false)
    if (failure) toast.error(`${errMsg(failure)}${done.length ? ` (${done.length} of ${ids.length} done)` : ""}`)
    else toast.success(ok)
  }

  const del = (ids: string[]) =>
    run(ids, async (_x, done) => { for (const id of ids) { await api.deleteMail(id); done.push(id) } },
      `${label(ids.length)} permanently deleted`)
  const restore = (ids: string[]) =>
    run(ids, async (_x, done) => { await api.moveMail(ids, folder, restoreTo!); done.push(...ids) },
      `${label(ids.length)} restored`)

  const sel = Array.from(selected)
  const allSelected = emails.length > 0 && selected.size === emails.length

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <Checkbox
            checked={allSelected}
            disabled={emails.length === 0}
            onCheckedChange={() => setSelected(allSelected ? new Set() : new Set(emails.map((e) => e.id)))}
          />
          {sel.length > 0 && (
            <>
              <span className="text-sm text-muted-foreground">{sel.length} selected</span>
              {restoreTo && (
                <Button variant="ghost" size="icon" className="h-8 w-8" disabled={busy} title="Restore selected" onClick={() => void restore(sel)}>
                  <RotateCcw className="h-4 w-4" />
                </Button>
              )}
              <Button variant="ghost" size="icon" className="h-8 w-8 text-destructive" disabled={busy} title="Delete selected permanently" onClick={() => void del(sel)}>
                <Trash2 className="h-4 w-4" />
              </Button>
            </>
          )}
        </div>
        <div className="flex items-center gap-1">
          {restoreTo && (
            <Button variant="ghost" size="sm" disabled={emails.length === 0 || busy || loading}
              onClick={() => { if (confirm("Permanently delete all messages in Trash?")) void del(emails.map((e) => e.id)) }}>
              Empty trash
            </Button>
          )}
          <Button variant="ghost" size="icon" className="h-8 w-8" title="Refresh" disabled={loading} onClick={() => void reload()}>
            <RefreshCw className={cn("h-4 w-4", loading && "animate-spin")} />
          </Button>
        </div>
      </div>

      <div className="rounded-lg border bg-card">
        {loading ? (
          <div className="divide-y">
            {[1, 2, 3].map((i) => (
              <div key={i} className="flex items-center gap-4 p-4">
                <Skeleton className="h-4 w-4" />
                <div className="flex-1 space-y-2">
                  <Skeleton className="h-4 w-32" />
                  <Skeleton className="h-3 w-full" />
                </div>
              </div>
            ))}
          </div>
        ) : error ? (
          <div role="alert" className="flex flex-col items-center justify-center py-16 text-center">
            <h3 className="text-lg font-semibold">Could not load messages</h3>
            <p className="text-sm text-muted-foreground">{error}</p>
            <Button variant="outline" size="sm" className="mt-4" onClick={() => void reload()}>Retry</Button>
          </div>
        ) : emails.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-16 text-center">
            <div className="rounded-full bg-muted p-4"><Icon className="h-8 w-8 text-muted-foreground" /></div>
            <h3 className="mt-4 text-lg font-semibold">{emptyTitle}</h3>
            <p className="text-sm text-muted-foreground">{emptyText}</p>
          </div>
        ) : (
          <div className="divide-y">
            {emails.map((email) => (
              <div
                key={email.id}
                className="group flex cursor-pointer items-start gap-3 p-4 transition-colors hover:bg-accent/50"
                onClick={() =>
                  navigate(folder === "drafts" ? `/compose?draft=${encodeURIComponent(email.id)}` : `/email/${encodeURIComponent(email.id)}`)}
              >
                <Checkbox
                  checked={selected.has(email.id)}
                  onCheckedChange={() => toggle(email.id)}
                  onClick={(e) => e.stopPropagation()}
                />
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="truncate font-medium">
                      {showRecipients
                        ? `To: ${(email.to || []).join(", ") || "No recipient"}`
                        : email.fromName || email.from}
                    </span>
                    {email.hasAttachments && <Paperclip className="h-3 w-3 text-muted-foreground" />}
                  </div>
                  <div className="flex items-center gap-2 text-sm text-muted-foreground">
                    <span className="truncate font-medium">{email.subject || "No subject"}</span>
                    <span className="truncate">— {email.preview}</span>
                  </div>
                </div>
                <span className="whitespace-nowrap text-sm text-muted-foreground">{email.date}</span>
              </div>
            ))}
          </div>
        )}
      </div>

      {!loading && !error && (
        <div className="flex items-center justify-between">
          <span className="text-sm text-muted-foreground">
            {label(emails.length)}{hasMore && ` of ${total}`}
          </span>
          {hasMore && (
            <Button variant="outline" size="sm" disabled={loadingMore} onClick={() => void loadMore()}>
              {loadingMore ? "Loading..." : "Load more"}
            </Button>
          )}
        </div>
      )}
    </div>
  )
}
