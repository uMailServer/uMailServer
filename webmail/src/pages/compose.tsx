import { useState, useRef, useEffect, useCallback } from "react"
import { useNavigate, useSearchParams } from "react-router-dom"
import {
  ArrowLeft,
  Send,
  Save,
  Paperclip,
  X,
  Plus,
  Trash2,
  Bold,
  Italic,
  Underline,
  Link,
  List,
  Image,
  Minimize2,
  Maximize2,
  Clock,
  Check,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import { Separator } from "@/components/ui/separator"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Checkbox } from "@/components/ui/checkbox"
import { Textarea } from "@/components/ui/textarea"
import { toast } from "sonner"
import { cn } from "@/lib/utils"

interface Attachment {
  id: string
  name: string
  size: number
  file?: File
}

interface Recipient {
  id: string
  name: string
  email: string
}

const mockContacts: Recipient[] = [
  { id: "1", name: "John Smith", email: "john@example.com" },
  { id: "2", name: "Sarah Johnson", email: "sarah.johnson@company.com" },
  { id: "3", name: "Mike Wilson", email: "mike.wilson@gmail.com" },
  { id: "4", name: "Emily Brown", email: "emily.brown@outlook.com" },
]

export function ComposePage() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const [to, setTo] = useState<Recipient[]>([])
  const [cc, setCc] = useState<Recipient[]>([])
  const [bcc, setBcc] = useState<Recipient[]>([])
  const [isFullscreen, setIsFullscreen] = useState(false)
  const [lastSaved, setLastSaved] = useState<Date | null>(null)
  const [isSaving, setIsSaving] = useState(false)

  useEffect(() => {
    const replyTo = searchParams.get("replyTo")
    if (replyTo) {
      const contact = mockContacts.find((c) => c.email === replyTo)
      if (contact) {
        setTo([contact])
      } else {
        setTo([{ id: "reply", name: replyTo, email: replyTo }])
      }
    }
  }, [searchParams])

  const [subject, setSubject] = useState("")
  const [body, setBody] = useState("")
  const [attachments, setAttachments] = useState<Attachment[]>([])
  const [searchQuery, setSearchQuery] = useState("")
  const [showCc, setShowCc] = useState(false)
  const [showBcc, setShowBcc] = useState(false)
  const [sending, setSending] = useState(false)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const autoSaveTimerRef = useRef<NodeJS.Timeout>()

  const filteredContacts = mockContacts.filter(
    (c) =>
      c.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
      c.email.toLowerCase().includes(searchQuery.toLowerCase())
  )

  const addRecipient = (contact: Recipient, field: "to" | "cc" | "bcc") => {
    if (field === "to") {
      if (!to.find((r) => r.id === contact.id)) {
        setTo([...to, contact])
      }
    } else if (field === "cc") {
      if (!cc.find((r) => r.id === contact.id)) {
        setCc([...cc, contact])
      }
    } else {
      if (!bcc.find((r) => r.id === contact.id)) {
        setBcc([...bcc, contact])
      }
    }
    setSearchQuery("")
  }

  const removeRecipient = (id: string, field: "to" | "cc" | "bcc") => {
    if (field === "to") {
      setTo(to.filter((r) => r.id !== id))
    } else if (field === "cc") {
      setCc(cc.filter((r) => r.id !== id))
    } else {
      setBcc(bcc.filter((r) => r.id !== id))
    }
  }

  const handleAttach = (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = e.target.files
    if (files) {
      const newAttachments = Array.from(files).map((file) => ({
        id: Math.random().toString(36).substr(2, 9),
        name: file.name,
        size: file.size,
        file,
      }))
      setAttachments([...attachments, ...newAttachments])
      toast.success(`${files.length} file${files.length > 1 ? "s" : ""} attached`)
    }
  }

  const removeAttachment = (id: string) => {
    setAttachments(attachments.filter((a) => a.id !== id))
  }

  const formatSize = (bytes: number) => {
    if (bytes < 1024) return bytes + " B"
    if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + " KB"
    return (bytes / (1024 * 1024)).toFixed(1) + " MB"
  }

  const handleAutoSave = useCallback(() => {
    if (subject || body || to.length > 0 || attachments.length > 0) {
      setIsSaving(true)
      setTimeout(() => {
        setLastSaved(new Date())
        setIsSaving(false)
      }, 500)
    }
  }, [subject, body, to, attachments])

  useEffect(() => {
    if (autoSaveTimerRef.current) {
      clearTimeout(autoSaveTimerRef.current)
    }
    autoSaveTimerRef.current = setTimeout(handleAutoSave, 3000)
    return () => {
      if (autoSaveTimerRef.current) {
        clearTimeout(autoSaveTimerRef.current)
      }
    }
  }, [subject, body, to, attachments, handleAutoSave])

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key === "Enter") {
        e.preventDefault()
        handleSend()
      }
    }
    window.addEventListener("keydown", handleKeyDown)
    return () => window.removeEventListener("keydown", handleKeyDown)
  }, [to, subject, body])

  const handleSend = () => {
    if (to.length === 0) {
      toast.error("Please select a recipient")
      return
    }
    if (!subject.trim()) {
      toast.error("Please enter a subject")
      return
    }
    setSending(true)
    toast.success("Sending email...")
    setTimeout(() => {
      setSending(false)
      toast.success("Email sent successfully")
      navigate("/sent")
    }, 1500)
  }

  const handleSaveDraft = () => {
    handleAutoSave()
    toast.success("Draft saved")
    navigate("/drafts")
  }

  const handleDiscard = () => {
    if (subject || body || to.length > 0) {
      if (confirm("Discard this email? Your draft will be saved.")) {
        handleSaveDraft()
      }
    } else {
      navigate("/inbox")
    }
  }

  const formatLastSaved = () => {
    if (!lastSaved) return null
    const now = new Date()
    const diff = Math.floor((now.getTime() - lastSaved.getTime()) / 1000)
    if (diff < 60) return "Just now"
    if (diff < 3600) return `${Math.floor(diff / 60)}m ago`
    return lastSaved.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
  }

  return (
    <div className={cn(
      "flex flex-col bg-background transition-all duration-200",
      isFullscreen ? "fixed inset-0 z-50" : "h-[calc(100vh-4rem)]"
    )}>
      {/* Header */}
      <div className="flex items-center justify-between border-b px-4 py-2">
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="icon" onClick={handleDiscard}>
            <ArrowLeft className="h-5 w-5" />
          </Button>
          <span className="font-medium">New Message</span>
          {lastSaved && (
            <span className="flex items-center gap-1 text-xs text-muted-foreground ml-2">
              {isSaving ? (
                <>
                  <Clock className="h-3 w-3 animate-pulse" />
                  Saving...
                </>
              ) : (
                <>
                  <Check className="h-3 w-3" />
                  Saved {formatLastSaved()}
                </>
              )}
            </span>
          )}
        </div>
        <div className="flex items-center gap-1">
          <Button
            variant="ghost"
            size="icon"
            onClick={() => setIsFullscreen(!isFullscreen)}
            title={isFullscreen ? "Exit fullscreen" : "Fullscreen"}
          >
            {isFullscreen ? (
              <Minimize2 className="h-4 w-4" />
            ) : (
              <Maximize2 className="h-4 w-4" />
            )}
          </Button>
          <Button variant="ghost" size="icon" onClick={handleSaveDraft} title="Save draft (⌘S)">
            <Save className="h-4 w-4" />
          </Button>
          <Button
            className="gap-2"
            onClick={handleSend}
            disabled={sending || to.length === 0}
          >
            <Send className="h-4 w-4" />
            {sending ? "Sending..." : "Send"}
          </Button>
        </div>
      </div>

      {/* Recipients */}
      <div className="border-b px-4 py-2 space-y-2">
        <div className="flex items-center gap-2">
          <span className="w-12 text-sm text-muted-foreground">To:</span>
          <div className="flex flex-1 flex-wrap items-center gap-1 min-h-[32px]">
            {to.map((r) => (
              <Badge key={r.id} variant="secondary" className="gap-1 pr-1.5 py-1">
                {r.name}
                <button
                  onClick={() => removeRecipient(r.id, "to")}
                  className="ml-0.5 rounded-full hover:bg-muted p-0.5"
                >
                  <X className="h-3 w-3" />
                </button>
              </Badge>
            ))}
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="icon" className="h-6 w-6">
                  <Plus className="h-4 w-4" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start" className="w-72">
                <div className="p-2">
                  <Input
                    placeholder="Search contacts..."
                    value={searchQuery}
                    onChange={(e) => setSearchQuery(e.target.value)}
                  />
                </div>
                <Separator />
                <div className="max-h-48 overflow-auto">
                  {filteredContacts.map((contact) => (
                    <DropdownMenuItem
                      key={contact.id}
                      onClick={() => addRecipient(contact, "to")}
                      className="flex flex-col items-start py-2"
                    >
                      <span className="font-medium">{contact.name}</span>
                      <span className="text-xs text-muted-foreground">
                        {contact.email}
                      </span>
                    </DropdownMenuItem>
                  ))}
                </div>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
          <Button
            variant="ghost"
            size="sm"
            className="text-xs h-7"
            onClick={() => setShowCc(!showCc)}
          >
            Cc
          </Button>
          <Button
            variant="ghost"
            size="sm"
            className="text-xs h-7"
            onClick={() => setShowBcc(!showBcc)}
          >
            Bcc
          </Button>
        </div>

        {showCc && (
          <div className="flex items-center gap-2">
            <span className="w-12 text-sm text-muted-foreground">Cc:</span>
            <div className="flex flex-1 flex-wrap items-center gap-1 min-h-[32px]">
              {cc.map((r) => (
                <Badge key={r.id} variant="secondary" className="gap-1 pr-1.5 py-1">
                  {r.name}
                  <button
                    onClick={() => removeRecipient(r.id, "cc")}
                    className="ml-0.5 rounded-full hover:bg-muted p-0.5"
                  >
                    <X className="h-3 w-3" />
                  </button>
                </Badge>
              ))}
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="ghost" size="icon" className="h-6 w-6">
                    <Plus className="h-4 w-4" />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="start" className="w-72">
                  <div className="p-2">
                    <Input
                      placeholder="Search contacts..."
                      value={searchQuery}
                      onChange={(e) => setSearchQuery(e.target.value)}
                    />
                  </div>
                  <Separator />
                  <div className="max-h-48 overflow-auto">
                    {filteredContacts.map((contact) => (
                      <DropdownMenuItem
                        key={contact.id}
                        onClick={() => addRecipient(contact, "cc")}
                        className="flex flex-col items-start py-2"
                      >
                        <span className="font-medium">{contact.name}</span>
                        <span className="text-xs text-muted-foreground">
                          {contact.email}
                        </span>
                      </DropdownMenuItem>
                    ))}
                  </div>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          </div>
        )}

        {showBcc && (
          <div className="flex items-center gap-2">
            <span className="w-12 text-sm text-muted-foreground">Bcc:</span>
            <div className="flex flex-1 flex-wrap items-center gap-1 min-h-[32px]">
              {bcc.map((r) => (
                <Badge key={r.id} variant="secondary" className="gap-1 pr-1.5 py-1">
                  {r.name}
                  <button
                    onClick={() => removeRecipient(r.id, "bcc")}
                    className="ml-0.5 rounded-full hover:bg-muted p-0.5"
                  >
                    <X className="h-3 w-3" />
                  </button>
                </Badge>
              ))}
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="ghost" size="icon" className="h-6 w-6">
                    <Plus className="h-4 w-4" />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="start" className="w-72">
                  <div className="p-2">
                    <Input
                      placeholder="Search contacts..."
                      value={searchQuery}
                      onChange={(e) => setSearchQuery(e.target.value)}
                    />
                  </div>
                  <Separator />
                  <div className="max-h-48 overflow-auto">
                    {filteredContacts.map((contact) => (
                      <DropdownMenuItem
                        key={contact.id}
                        onClick={() => addRecipient(contact, "bcc")}
                        className="flex flex-col items-start py-2"
                      >
                        <span className="font-medium">{contact.name}</span>
                        <span className="text-xs text-muted-foreground">
                          {contact.email}
                        </span>
                      </DropdownMenuItem>
                    ))}
                  </div>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          </div>
        )}

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

      {/* Formatting Toolbar */}
      <div className="flex items-center gap-1 border-b px-4 py-1 bg-muted/30">
        <Button variant="ghost" size="icon" className="h-8 w-8" title="Bold (⌘B)">
          <Bold className="h-4 w-4" />
        </Button>
        <Button variant="ghost" size="icon" className="h-8 w-8" title="Italic (⌘I)">
          <Italic className="h-4 w-4" />
        </Button>
        <Button variant="ghost" size="icon" className="h-8 w-8" title="Underline (⌘U)">
          <Underline className="h-4 w-4" />
        </Button>
        <Separator orientation="vertical" className="h-6" />
        <Button variant="ghost" size="icon" className="h-8 w-8" title="Insert link">
          <Link className="h-4 w-4" />
        </Button>
        <Button variant="ghost" size="icon" className="h-8 w-8" title="Bullet list">
          <List className="h-4 w-4" />
        </Button>
        <Button variant="ghost" size="icon" className="h-8 w-8" title="Insert image">
          <Image className="h-4 w-4" />
        </Button>
        <span className="text-xs text-muted-foreground ml-2">
          Tip: Press ⌘+Enter to send
        </span>
      </div>

      {/* Body */}
      <div className="flex-1 overflow-hidden">
        <Textarea
          className="h-full resize-none border-0 shadow-none focus-visible:ring-0 p-4"
          placeholder="Write your message..."
          value={body}
          onChange={(e) => setBody(e.target.value)}
        />
      </div>

      {/* Attachments & Footer */}
      {attachments.length > 0 && (
        <div className="border-t px-4 py-2 bg-muted/30">
          <div className="flex flex-wrap gap-2">
            {attachments.map((att) => (
              <div
                key={att.id}
                className="flex items-center gap-2 rounded border bg-background px-3 py-1.5"
              >
                <Paperclip className="h-4 w-4 text-muted-foreground" />
                <span className="text-sm">{att.name}</span>
                <span className="text-xs text-muted-foreground">
                  ({formatSize(att.size)})
                </span>
                <button
                  onClick={() => removeAttachment(att.id)}
                  className="ml-1 rounded-full hover:bg-muted p-0.5"
                >
                  <X className="h-3 w-3" />
                </button>
              </div>
            ))}
          </div>
        </div>
      )}

      <div className="flex items-center justify-between border-t px-4 py-2">
        <div className="flex items-center gap-2">
          <Button variant="outline" size="icon" onClick={() => fileInputRef.current?.click()}>
            <Paperclip className="h-5 w-5" />
          </Button>
          <input
            type="file"
            multiple
            ref={fileInputRef}
            className="hidden"
            onChange={handleAttach}
          />
        </div>
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <kbd className="rounded border px-1.5 py-0.5 text-xs bg-muted">⌘</kbd>
          <span>+</span>
          <kbd className="rounded border px-1.5 py-0.5 text-xs bg-muted">Enter</kbd>
          <span>to send</span>
        </div>
      </div>
    </div>
  )
}
