import { useState, useRef, useEffect } from "react"
import { useNavigate, useSearchParams } from "react-router-dom"
import { ArrowLeft, Send, X, Maximize2, Minimize2, Save, Paperclip } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import { Textarea } from "@/components/ui/textarea"
import { toast } from "sonner"
import { cn } from "@/lib/utils"
import { quoteForReply, forwardBody, replyAllRecipients } from "@/utils/quote"
import api, { FLAG_ANSWERED } from "@/utils/api"
import { attachmentError, readFileBase64, type ComposeAttachment } from "@/utils/attachments"
import { suggestionsFor, type Suggestion } from "@/utils/recipients"

const EMAIL_RE = /^[^\s@<>,;]+@[^\s@<>,;]+\.[^\s@<>,;]+$/

const COMPOSE_BACKUP_KEY = "umail.compose.backup"

type Field = "to" | "cc" | "bcc"

function RecipientField({
  label, values, onChange, contacts = [],
}: { label: string; values: string[]; onChange: (v: string[]) => void; contacts?: Suggestion[] }) {
  const [text, setText] = useState("")
  const [open, setOpen] = useState(false)
  // The suggestion chosen with the arrow keys; -1 until the user moves.
  const [browsed, setBrowsed] = useState(-1)

  // The word being typed is the text after the last separator.
  const word = text.split(/[,;\s]+/).pop() ?? ""
  const matches = open ? suggestionsFor(contacts, word, values) : []
  // Enter takes the highlighted suggestion. Without arrow-key movement a
  // complete address is kept as typed (a substring match must not swap it
  // for another contact); otherwise the first suggestion is highlighted.
  const active = browsed >= 0 ? Math.min(browsed, matches.length - 1)
    : matches.length > 0 && !EMAIL_RE.test(word) ? 0 : -1
  const listId = `recipients-${label.toLowerCase()}`

  // Accept comma/semicolon/space separated addresses; keep invalid text so the
  // user can correct it instead of silently dropping it.
  const commit = () => {
    setOpen(false)
    const parts = text.split(/[,;\s]+/).filter(Boolean)
    if (parts.length === 0) return
    const bad = parts.filter((p) => !EMAIL_RE.test(p))
    const good = parts.filter((p) => EMAIL_RE.test(p) && !values.includes(p))
    if (good.length) onChange([...values, ...good])
    setText(bad.join(", "))
    if (bad.length) toast.error(`Invalid address: ${bad.join(", ")}`)
  }

  // pick replaces the word being typed with a contact's address.
  const pick = (s: Suggestion) => {
    const rest = text.slice(0, text.length - word.length).split(/[,;\s]+/).filter(Boolean)
    const good = rest.filter((p) => EMAIL_RE.test(p) && !values.includes(p))
    const bad = rest.filter((p) => !EMAIL_RE.test(p))
    onChange([...values, ...good.filter((p) => p !== s.email), ...(values.includes(s.email) ? [] : [s.email])])
    setText(bad.join(", "))
    setOpen(false)
    setBrowsed(-1)
  }

  return (
    <div className="flex items-center gap-2">
      <span className="w-12 text-sm text-muted-foreground">{label}:</span>
      <div className="flex flex-1 flex-wrap items-center gap-1 min-h-[32px]">
        {values.map((v) => (
          <Badge key={v} variant="secondary" className="gap-1 pr-1.5 py-1">
            {v}
            <button type="button" aria-label={`Remove ${v}`} onClick={() => onChange(values.filter((x) => x !== v))}
              className="ml-0.5 rounded-full hover:bg-muted p-0.5">
              <X className="h-3 w-3" />
            </button>
          </Badge>
        ))}
        <div className="relative min-w-[12rem] flex-1">
          <Input
            aria-label={label}
            role="combobox"
            aria-autocomplete="list"
            aria-expanded={matches.length > 0}
            aria-controls={listId}
            aria-activedescendant={active >= 0 ? `${listId}-${active}` : undefined}
            className="h-8 w-full border-0 shadow-none focus-visible:ring-0 px-1"
            placeholder="name@example.com"
            value={text}
            onChange={(e) => { setText(e.target.value); setOpen(true); setBrowsed(-1) }}
            onBlur={commit}
            onKeyDown={(e) => {
              // Keys that confirm an IME composition belong to the IME.
              if (e.nativeEvent.isComposing) return
              if (matches.length > 0) {
                if (e.key === "ArrowDown" || e.key === "ArrowUp") {
                  e.preventDefault()
                  const step = e.key === "ArrowDown" ? 1 : -1
                  setBrowsed(active < 0 ? (step > 0 ? 0 : matches.length - 1) : (active + step + matches.length) % matches.length)
                  return
                }
                // Tab takes only a suggestion chosen with the arrows; otherwise
                // focus moves on and blur commits the typed text.
                if ((e.key === "Enter" && active >= 0) || (e.key === "Tab" && browsed >= 0)) {
                  e.preventDefault()
                  pick(matches[active])
                  return
                }
                if (e.key === "Escape") {
                  e.preventDefault()
                  setOpen(false)
                  return
                }
              }
              if (e.key === "Enter" || e.key === "," || e.key === ";") {
                e.preventDefault()
                commit()
              }
            }}
          />
          {matches.length > 0 && (
            <ul id={listId} role="listbox" aria-label={`${label} suggestions`}
              className="absolute left-0 top-full z-20 mt-1 w-full min-w-[16rem] rounded-md border bg-popover p-1 text-popover-foreground shadow-md">
              {matches.map((s, i) => (
                <li key={s.email} id={`${listId}-${i}`} role="option" aria-selected={i === active}
                  className={cn("cursor-pointer rounded px-2 py-1 text-sm", i === active && "bg-accent text-accent-foreground")}
                  // mousedown, not click: the input's blur would commit the typed text first.
                  onMouseDown={(e) => { e.preventDefault(); pick(s) }}
                  onMouseEnter={() => setBrowsed(i)}>
                  {s.name ? <><span className="font-medium">{s.name}</span>{" "}<span className="text-muted-foreground">{s.email}</span></> : s.email}
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </div>
  )
}

export function ComposePage() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  // "to" prefills a new message (e.g. from Contacts); "replyTo" comes from a reply.
  const replyTo = searchParams.get("replyTo") || searchParams.get("to")
  const draftId = searchParams.get("draft")
  const [to, setTo] = useState<string[]>(() => (replyTo && EMAIL_RE.test(replyTo) ? [replyTo] : []))
  const [cc, setCc] = useState<string[]>([])
  const [bcc, setBcc] = useState<string[]>([])
  const [contacts, setContacts] = useState<Suggestion[]>([])

  // Recipient suggestions are a convenience: without contacts the fields
  // still take typed addresses, so a failed load is not reported.
  useEffect(() => {
    let cancelled = false
    api.getContacts().then((list) => {
      if (cancelled) return
      setContacts(list.flatMap((c) => (c.emails || []).map((email) => ({ name: c.name || "", email }))))
    }).catch(() => {})
    return () => { cancelled = true }
  }, [])
  const [subject, setSubject] = useState(() => searchParams.get("subject") || "")
  const [body, setBody] = useState("")
  const [attachments, setAttachments] = useState<ComposeAttachment[]>([])
  const attachmentsRef = useRef<ComposeAttachment[]>([])
  attachmentsRef.current = attachments
  const addFiles = async (files: FileList | null) => {
    if (!files) return
    let next = attachmentsRef.current
    for (const f of Array.from(files)) {
      const err = attachmentError(next, f)
      if (err) { toast.error(err); continue }
      try {
        const data = await readFileBase64(f)
        next = [...next, { filename: f.name, contentType: f.type || "application/octet-stream", data, size: f.size }]
      } catch { toast.error(`Could not read ${f.name}`) }
    }
    if (mounted.current) setAttachments(next)
  }
  const [isFullscreen, setIsFullscreen] = useState(false)
  const [sending, setSending] = useState(false)
  const sendingRef = useRef(false)
  const mounted = useRef(true)
  const latest = useRef({ to, cc, bcc, subject, body })
  latest.current = { to, cc, bcc, subject, body }

  // Session-expiry safety net: a 401 hard-redirects to /login, so mirror the
  // unsent text locally and restore it when a fresh compose page opens.
  useEffect(() => {
    if (draftId || origId) return
    try {
      const raw = sessionStorage.getItem(COMPOSE_BACKUP_KEY)
      if (!raw) return
      const b = JSON.parse(raw)
      if (Array.isArray(b.to)) setTo(b.to.filter((x: unknown) => typeof x === "string"))
      if (Array.isArray(b.cc)) setCc(b.cc.filter((x: unknown) => typeof x === "string"))
      if (typeof b.subject === "string") setSubject((c) => c || b.subject)
      if (typeof b.body === "string") setBody(b.body)
    } catch { /* storage unavailable or corrupt */ }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  const backupArmed = useRef(false)
  useEffect(() => {
    // Skip the mount pass so empty initial state cannot wipe the backup
    // before the restore effect's setState values render.
    if (!backupArmed.current) { backupArmed.current = true; return }
    try {
      if (sentRef.current) { sessionStorage.removeItem(COMPOSE_BACKUP_KEY); return }
      if (to.length || cc.length || subject || body) sessionStorage.setItem(COMPOSE_BACKUP_KEY, JSON.stringify({ to, cc, subject, body }))
      else sessionStorage.removeItem(COMPOSE_BACKUP_KEY)
    } catch { /* ignore */ }
  }, [to, cc, subject, body])

  useEffect(() => {
    mounted.current = true
    return () => { mounted.current = false }
  }, [])

  // Draft state. Edited drafts get a NEW id from the server (ids are content
  // hashes), so the id lives in a ref and is replaced by every save response.
  const draftIdRef = useRef<string | null>(draftId)
  const savedSnapshot = useRef<string>(JSON.stringify({ to, cc, bcc, subject, body }))
  const saveInFlight = useRef<Promise<void> | null>(null)
  const sentRef = useRef(false)
  const loadingDraftRef = useRef(!!draftId)
  const [saving, setSaving] = useState(false)
  const [savedAt, setSavedAt] = useState<boolean>(false)
  const origId = searchParams.get("origId")
  const origFolder = searchParams.get("origFolder") || undefined
  const mode = searchParams.get("mode")
  const inReplyTo = searchParams.get("inReplyTo") || ""

  // Reply/forward: prefill the quoted original once (never over user edits).
  useEffect(() => {
    if (draftId || !origId || (mode !== "reply" && mode !== "replyall" && mode !== "forward")) return
    let cancelled = false
    api.getMessage(origId, origFolder).then((m) => {
      if (cancelled || !m) return
      const who = m.fromName ? `${m.fromName} <${m.from}>` : m.from
      if (mode === "replyall") {
        const r = replyAllRecipients(m.from, m.to, m.cc, m.account)
        setTo((cur) => (cur.length ? cur : r.to))
        setCc((cur) => (cur.length ? cur : r.cc))
      }
      const quoted = mode !== "forward"
        ? quoteForReply(who, m.date, m.body || "")
        : forwardBody(who, m.date, m.subject, m.to || [], m.body || "")
      setBody((cur) => (cur === "" ? quoted : cur))
      savedSnapshot.current = JSON.stringify({ ...latest.current, body: "" })
    }).catch(() => toast.error("Could not load the original message for quoting"))
    return () => { cancelled = true }
  }, [draftId, origId, origFolder, mode])

  // Prefill from an existing draft via GET /mail/message.
  useEffect(() => {
    if (!draftId) return
    let cancelled = false
    draftIdRef.current = draftId
    loadingDraftRef.current = true
    api.getMessage(draftId, "drafts").then((d) => {
      if (cancelled) return
      if (!d) { toast.error("Draft not found"); return }
      const next = { to: d.to || [], cc: d.cc || [], bcc: d.bcc || [], subject: d.subject || "", body: d.body || "" }
      savedSnapshot.current = JSON.stringify(next)
      setTo(next.to)
      setCc(next.cc)
      setBcc(next.bcc)
      setSubject(next.subject)
      setBody(next.body)
      loadingDraftRef.current = false
    }).catch((err) => {
      if (!cancelled) toast.error(err instanceof Error && err.message ? err.message : "Failed to load draft")
    })
    return () => { cancelled = true }
  }, [draftId])

  // Saves the current content as a draft. Only one request is ever in flight;
  // a save requested meanwhile re-runs afterwards if content still differs.
  const saveDraft = async (manual = false): Promise<void> => {
    if (sentRef.current || loadingDraftRef.current) return
    if (saveInFlight.current) {
      await saveInFlight.current
      if (manual) return saveDraft(true)
      return
    }
    const cur = latest.current
    const snap = JSON.stringify(cur)
    if (snap === savedSnapshot.current) {
      if (manual) toast.info("No changes to save")
      return
    }
    if (!cur.to.length && !cur.cc.length && !cur.bcc.length && !cur.subject.trim() && !cur.body.trim()) return
    const run = (async () => {
      if (mounted.current) setSaving(true)
      try {
        const res = await api.saveDraft({
          ...(draftIdRef.current ? { id: draftIdRef.current } : {}),
          to: cur.to, cc: cur.cc, bcc: cur.bcc, subject: cur.subject, body: cur.body,
        })
        draftIdRef.current = res.id
        savedSnapshot.current = snap
        if (mounted.current) setSavedAt(true)
        if (manual && mounted.current) toast.success("Draft saved")
      } catch (err) {
        if (manual && mounted.current) toast.error(err instanceof Error && err.message ? err.message : "Failed to save draft")
      } finally {
        saveInFlight.current = null
        if (mounted.current) setSaving(false)
      }
    })()
    saveInFlight.current = run
    await run
  }
  const saveDraftRef = useRef(saveDraft)
  saveDraftRef.current = saveDraft

  // Autosave 2s after the last edit; the snapshot comparison skips unchanged content.
  useEffect(() => {
    const t = setTimeout(() => { void saveDraftRef.current() }, 2000)
    return () => clearTimeout(t)
  }, [to, cc, bcc, subject, body])

  const handleSend = async () => {
    if (sendingRef.current) return
    const cur = latest.current
    if (cur.to.length === 0) { toast.error("Please add a recipient"); return }
    if (!cur.subject.trim()) { toast.error("Please enter a subject"); return }
    sendingRef.current = true
    setSending(true)
    try {
      // Stop autosave from recreating the draft, and wait for a pending save
      // so that the id we delete below is the final one.
      sentRef.current = true
      if (saveInFlight.current) await saveInFlight.current
      await api.sendMail({
        to: cur.to,
        ...(cur.cc.length ? { cc: cur.cc } : {}),
        ...(cur.bcc.length ? { bcc: cur.bcc } : {}),
        subject: cur.subject,
        body: cur.body,
        ...(attachmentsRef.current.length
          ? { attachments: attachmentsRef.current.map(({ filename, contentType, data }) => ({ filename, contentType, data })) }
          : {}),
        ...((mode === "reply" || mode === "replyall") && inReplyTo ? { inReplyTo } : {}),
      })
      // Best-effort follow-ups: the message is already sent.
      const tasks: Promise<unknown>[] = []
      if (draftIdRef.current) tasks.push(api.deleteMail(draftIdRef.current).catch(() => undefined))
      if (origId) tasks.push(api.setFlags(origId, { add: [FLAG_ANSWERED], folder: origFolder }).catch(() => undefined))
      await Promise.all(tasks)
      if (!mounted.current) return
      try { sessionStorage.removeItem(COMPOSE_BACKUP_KEY) } catch { /* ignore */ }
      toast.success("Email sent")
      navigate("/sent")
    } catch (err) {
      sentRef.current = false
      if (mounted.current) toast.error(err instanceof Error && err.message ? err.message : "Failed to send email")
    } finally {
      sendingRef.current = false
      if (mounted.current) setSending(false)
    }
  }

  const handleSendRef = useRef(handleSend)
  handleSendRef.current = handleSend
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key === "Enter") {
        e.preventDefault()
        void handleSendRef.current()
      }
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [])

  const isDirty = () => {
    const c = latest.current
    return JSON.stringify(c) !== savedSnapshot.current &&
      !!(c.to.length || c.cc.length || c.bcc.length || c.subject.trim() || c.body.trim())
  }
  // Warn on tab close / reload while there are unsaved edits.
  useEffect(() => {
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      if (sentRef.current || !isDirty()) return
      e.preventDefault()
      e.returnValue = ""
    }
    window.addEventListener("beforeunload", onBeforeUnload)
    return () => window.removeEventListener("beforeunload", onBeforeUnload)
  }, [])

  const handleDiscard = () => {
    if (isDirty() && !confirm("Discard this message? Unsaved changes will be lost.")) return
    navigate("/inbox")
  }

  const set: Record<Field, (v: string[]) => void> = { to: setTo, cc: setCc, bcc: setBcc }

  return (
    <div className={cn("flex flex-col bg-background", isFullscreen ? "fixed inset-0 z-50" : "h-[calc(100vh-4rem)]")}>
      <div className="flex items-center justify-between border-b px-4 py-2">
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="icon" onClick={handleDiscard} aria-label="Back">
            <ArrowLeft className="h-5 w-5" />
          </Button>
          <span className="font-medium">New Message</span>
        </div>
        <div className="flex items-center gap-1">
          <Button variant="ghost" size="icon" onClick={() => setIsFullscreen(!isFullscreen)}
            title={isFullscreen ? "Exit fullscreen" : "Fullscreen"}>
            {isFullscreen ? <Minimize2 className="h-4 w-4" /> : <Maximize2 className="h-4 w-4" />}
          </Button>
          <Button variant="outline" className="gap-2" onClick={() => void saveDraft(true)} disabled={saving || sending}>
            <Save className="h-4 w-4" />
            {saving ? "Saving..." : "Save draft"}
          </Button>
          <Button className="gap-2" onClick={() => void handleSend()} disabled={sending}>
            <Send className="h-4 w-4" />
            {sending ? "Sending..." : "Send"}
          </Button>
        </div>
      </div>

      <div className="border-b px-4 py-2 space-y-2">
        <RecipientField label="To" values={to} onChange={set.to} contacts={contacts} />
        <RecipientField label="Cc" values={cc} onChange={set.cc} contacts={contacts} />
        <RecipientField label="Bcc" values={bcc} onChange={set.bcc} contacts={contacts} />
        <div className="flex items-center gap-2">
          <span className="w-12 text-sm text-muted-foreground">Sub:</span>
          <Input
            className="flex-1 border-0 shadow-none focus-visible:ring-0 px-0 py-1 h-8"
            placeholder="Subject"
            value={subject}
            onChange={(e) => setSubject(e.target.value)}
          />
        </div>
      </div>

      <div className="border-b px-4 py-2 flex flex-wrap items-center gap-1">
        <label className="inline-flex cursor-pointer items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <Paperclip className="h-4 w-4" />
          Attach files
          <input type="file" multiple aria-label="Attach files" className="sr-only"
            onChange={(e) => { const el = e.target; void addFiles(el.files).then(() => { el.value = "" }) }} />
        </label>
        {attachments.map((a, i) => (
          <Badge key={`${a.filename}-${i}`} variant="secondary" className="gap-1 pr-1.5 py-1">
            {a.filename} ({Math.max(1, Math.round(a.size / 1024))} KB)
            <button type="button" aria-label={`Remove attachment ${a.filename}`}
              onClick={() => setAttachments((cur) => cur.filter((_, j) => j !== i))}
              className="ml-0.5 rounded-full hover:bg-muted p-0.5">
              <X className="h-3 w-3" />
            </button>
          </Badge>
        ))}
      </div>

      <div className="flex-1 overflow-hidden">
        <Textarea
          className="h-full resize-none border-0 shadow-none focus-visible:ring-0 p-4"
          placeholder="Write your message (plain text)..."
          value={body}
          onChange={(e) => setBody(e.target.value)}
        />
      </div>

      <div className="flex items-center justify-end border-t px-4 py-2 text-sm text-muted-foreground">
        {savedAt ? "Draft autosaved. " : ""}Press Ctrl/⌘ + Enter to send
      </div>
    </div>
  )
}
