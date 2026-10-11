import { useState, useRef, useEffect } from "react"
import { useNavigate, useSearchParams } from "react-router-dom"
import { ArrowLeft, Send, X, Maximize2, Minimize2, Save } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import { Textarea } from "@/components/ui/textarea"
import { toast } from "sonner"
import { cn } from "@/lib/utils"
import api, { FLAG_ANSWERED } from "@/utils/api"

const EMAIL_RE = /^[^\s@<>,;]+@[^\s@<>,;]+\.[^\s@<>,;]+$/

type Field = "to" | "cc" | "bcc"

function RecipientField({
  label, values, onChange,
}: { label: string; values: string[]; onChange: (v: string[]) => void }) {
  const [text, setText] = useState("")

  // Accept comma/semicolon/space separated addresses; keep invalid text so the
  // user can correct it instead of silently dropping it.
  const commit = () => {
    const parts = text.split(/[,;\s]+/).filter(Boolean)
    if (parts.length === 0) return
    const bad = parts.filter((p) => !EMAIL_RE.test(p))
    const good = parts.filter((p) => EMAIL_RE.test(p) && !values.includes(p))
    if (good.length) onChange([...values, ...good])
    setText(bad.join(", "))
    if (bad.length) toast.error(`Invalid address: ${bad.join(", ")}`)
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
        <Input
          aria-label={label}
          className="h-8 min-w-[12rem] flex-1 border-0 shadow-none focus-visible:ring-0 px-1"
          placeholder="name@example.com"
          value={text}
          onChange={(e) => setText(e.target.value)}
          onBlur={commit}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === "," || e.key === ";") {
              e.preventDefault()
              commit()
            }
          }}
        />
      </div>
    </div>
  )
}

export function ComposePage() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const replyTo = searchParams.get("replyTo")
  const draftId = searchParams.get("draft")
  const [to, setTo] = useState<string[]>(() => (replyTo && EMAIL_RE.test(replyTo) ? [replyTo] : []))
  const [cc, setCc] = useState<string[]>([])
  const [bcc, setBcc] = useState<string[]>([])
  const [subject, setSubject] = useState(() => searchParams.get("subject") || "")
  const [body, setBody] = useState("")
  const [isFullscreen, setIsFullscreen] = useState(false)
  const [sending, setSending] = useState(false)
  const sendingRef = useRef(false)
  const mounted = useRef(true)
  const latest = useRef({ to, cc, bcc, subject, body })
  latest.current = { to, cc, bcc, subject, body }

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
      })
      // Best-effort follow-ups: the message is already sent.
      const tasks: Promise<unknown>[] = []
      if (draftIdRef.current) tasks.push(api.deleteMail(draftIdRef.current).catch(() => undefined))
      if (origId) tasks.push(api.setFlags(origId, { add: [FLAG_ANSWERED], folder: origFolder }).catch(() => undefined))
      await Promise.all(tasks)
      if (!mounted.current) return
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

  const handleDiscard = () => {
    if (JSON.stringify(latest.current) !== savedSnapshot.current && (subject || body || to.length > 0) && !confirm("Discard this message? Unsaved changes will be lost.")) return
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
        <RecipientField label="To" values={to} onChange={set.to} />
        <RecipientField label="Cc" values={cc} onChange={set.cc} />
        <RecipientField label="Bcc" values={bcc} onChange={set.bcc} />
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
