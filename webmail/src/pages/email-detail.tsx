import { useEffect, useRef, useState } from "react"
import { useParams, useNavigate } from "react-router-dom"
import {
  ArrowLeft,
  Reply,
  ReplyAll,
  Paperclip,
  Forward,
  Trash2,
  Star,
  Printer,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { Separator } from "@/components/ui/separator"
import { toast } from "sonner"
import { sanitizeHTML } from "@/utils/sanitize"
import { replySubject, forwardSubject } from "@/utils/quote"
import api, { Mail, ApiError, FLAG_SEEN, FLAG_FLAGGED } from "@/utils/api"

const LOOKS_LIKE_HTML = /<\s*(?:p|div|br|html|body|table|span|a|img|ul|ol|h[1-6]|b|i|strong|em)\b/i

type LoadState =
  | { status: "loading" }
  | { status: "error"; message: string }
  | { status: "notfound" }
  | { status: "ready"; mail: Mail; folder: string }

export function EmailDetailPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const [state, setState] = useState<LoadState>({ status: "loading" })
  const [busy, setBusy] = useState(false)
  const mountedRef = useRef(true)
  const starBusyRef = useRef(false)

  useEffect(() => {
    mountedRef.current = true
    return () => { mountedRef.current = false }
  }, [])

  // GET /mail/message is read-only; mark read explicitly via /mail/flags.
  // `cancelled` discards results of a superseded id/unmounted page.
  useEffect(() => {
    let cancelled = false
    setState({ status: "loading" })
    if (!id) { setState({ status: "notfound" }); return }
    ;(async () => {
      try {
        const mail = await api.getMessage(id)
        if (cancelled) return
        if (!mail || typeof mail !== "object") { setState({ status: "notfound" }); return }
        const folder = String(mail.folder || "inbox").toLowerCase()
        setState({ status: "ready", mail, folder })
        if (!mail.read) {
          try {
            await api.setFlags(mail.id, { add: [FLAG_SEEN], folder })
            if (!cancelled) setState((s) => (s.status === "ready" && s.mail.id === mail.id ? { ...s, mail: { ...s.mail, read: true } } : s))
          } catch {
            // Best effort: the message stays displayed as unread on the server.
          }
        }
      } catch (err) {
        if (cancelled) return
        if (err instanceof ApiError && err.status === 404) { setState({ status: "notfound" }); return }
        setState({ status: "error", message: err instanceof Error && err.message ? err.message : "Failed to load message" })
      }
    })()
    return () => { cancelled = true }
  }, [id])

  if (state.status === "loading") {
    return <div className="p-6 text-sm text-muted-foreground" role="status">Loading message…</div>
  }
  if (state.status === "error" || state.status === "notfound") {
    return (
      <div role="alert" className="space-y-4 p-6">
        <Button variant="ghost" size="icon" onClick={() => navigate(-1)}>
          <ArrowLeft className="h-5 w-5" />
        </Button>
        <p className="text-sm text-muted-foreground">
          {state.status === "error" ? state.message : "Message not found."}
        </p>
      </div>
    )
  }

  const { mail, folder } = state
  const email = {
    from: mail.fromName || mail.from,
    fromEmail: mail.from,
    to: mail.to ?? [],
    subject: mail.subject,
    date: mail.date,
    content: mail.body ?? "",
  }
  const isStarred = mail.starred
  const toggleStar = async () => {
    if (starBusyRef.current) return
    starBusyRef.current = true
    const next = !mail.starred
    const patch = (v: boolean) => setState((s) => (s.status === "ready" && s.mail.id === mail.id ? { ...s, mail: { ...s.mail, starred: v } } : s))
    patch(next)
    try {
      await api.setFlags(mail.id, next ? { add: [FLAG_FLAGGED], folder } : { remove: [FLAG_FLAGGED], folder })
    } catch (err) {
      if (mountedRef.current) {
        patch(!next)
        toast.error(err instanceof Error && err.message ? err.message : "Could not update star")
      }
    } finally {
      starBusyRef.current = false
    }
  }

  const handleDelete = async () => {
    if (busy) return
    setBusy(true)
    try {
      if (folder === "trash") await api.deleteMail(mail.id)
      else await api.moveMail(mail.id, folder, "trash")
      if (!mountedRef.current) return
      toast.success(folder === "trash" ? "Email deleted" : "Email moved to trash")
      navigate("/trash")
    } catch (err) {
      if (!mountedRef.current) return
      toast.error(err instanceof Error && err.message ? err.message : "Operation failed")
      setBusy(false)
    }
  }

  return (
    <div className="space-y-4">
      {/* Toolbar */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-1">
          <Button variant="ghost" size="icon" onClick={() => navigate(-1)}>
            <ArrowLeft className="h-5 w-5" />
          </Button>
          <Button variant="ghost" size="icon" className="text-destructive" onClick={handleDelete} disabled={busy} title="Delete">
            <Trash2 className="h-5 w-5" />
          </Button>
        </div>

        <div className="flex items-center gap-1">
          <Button variant="ghost" size="icon" aria-label={isStarred ? "Starred" : "Not starred"} aria-pressed={isStarred} onClick={() => void toggleStar()}>
            <Star className={cn("h-5 w-5", isStarred ? "fill-amber-400 text-amber-400" : "text-muted-foreground/40")} />
          </Button>
          <Button variant="ghost" size="icon" title="Print" onClick={() => window.print()}>
            <Printer className="h-5 w-5" />
          </Button>
        </div>
      </div>

      {/* Email Content */}
      <div className="rounded-lg border bg-card">
        {/* Header */}
        <div className="p-6 pb-0">
          <div className="flex items-start justify-between gap-4">
            <h1 className="text-2xl font-semibold leading-tight">{email.subject}</h1>
          </div>

          <div className="flex items-start gap-4 mt-6">
            <Avatar className="h-12 w-12 ring-2 ring-primary/10">
              <AvatarFallback className="bg-gradient-to-br from-primary to-primary/80 text-primary-foreground font-semibold text-lg">
                {(email.from.trim().split(/\s+/).map((n) => n[0]).join("").slice(0, 2) || "?").toUpperCase()}
              </AvatarFallback>
            </Avatar>

            <div className="flex-1 min-w-0">
              <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                <span className="font-semibold text-lg">{email.from}</span>
                <span className="text-sm text-muted-foreground">
                  &lt;{email.fromEmail}&gt;
                </span>
              </div>

              <div className="mt-1 flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-muted-foreground">
                <span>
                  <span className="font-medium text-foreground">To:</span> {email.to.join(", ")}
                </span>
                {(mail.cc?.length ?? 0) > 0 && (
                  <span>
                    <span className="font-medium text-foreground">Cc:</span> {mail.cc!.join(", ")}
                  </span>
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
        <div className="px-6 pb-6">
          {LOOKS_LIKE_HTML.test(email.content) ? (
            <div
              className="prose prose-neutral dark:prose-invert max-w-none prose-headings:font-semibold prose-p:leading-relaxed prose-ul:leading-relaxed"
              dangerouslySetInnerHTML={{ __html: sanitizeHTML(email.content) }}
            />
          ) : (
            <div className="whitespace-pre-wrap break-words text-sm">{email.content}</div>
          )}
        </div>

        {(mail.attachments?.length ?? 0) > 0 && (
          <div className="border-t px-6 py-4" aria-label="Attachments">
            <ul className="flex flex-wrap gap-2">
              {mail.attachments!.map((a) => (
                <li key={a.index}>
                  <a
                    href={api.attachmentUrl(mail.id, folder, a.index)}
                    download={a.filename}
                    className="inline-flex items-center gap-2 rounded-md border px-3 py-1.5 text-sm hover:bg-accent"
                  >
                    <Paperclip className="h-4 w-4" />
                    {a.filename} <span className="text-muted-foreground">({formatSize(a.size)})</span>
                  </a>
                </li>
              ))}
            </ul>
          </div>
        )}

      </div>

      {/* Reply Actions */}
      <div className="flex items-center gap-2 pl-2">
        <Button
          className="gap-2"
          onClick={() =>
            navigate(`/compose?replyTo=${encodeURIComponent(email.fromEmail)}&mode=reply&inReplyTo=${encodeURIComponent(mail.messageId || "")}&subject=${encodeURIComponent(replySubject(email.subject))}&origId=${encodeURIComponent(mail.id)}&origFolder=${encodeURIComponent(folder)}`)
          }
        >
          <Reply className="h-4 w-4" />
          Reply
        </Button>
        <Button
          variant="outline"
          className="gap-2"
          onClick={() =>
            navigate(`/compose?mode=replyall&inReplyTo=${encodeURIComponent(mail.messageId || "")}&subject=${encodeURIComponent(replySubject(email.subject))}&origId=${encodeURIComponent(mail.id)}&origFolder=${encodeURIComponent(folder)}`)
          }
        >
          <ReplyAll className="h-4 w-4" />
          Reply all
        </Button>
        <Button
          variant="outline"
          className="gap-2"
          onClick={() =>
            navigate(`/compose?mode=forward&subject=${encodeURIComponent(forwardSubject(email.subject))}&origId=${encodeURIComponent(mail.id)}&origFolder=${encodeURIComponent(folder)}`)
          }
        >
          <Forward className="h-4 w-4" />
          Forward
        </Button>
      </div>
    </div>
  )
}

function formatSize(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

function cn(...classes: (string | boolean | undefined)[]) {
  return classes.filter(Boolean).join(" ")
}
