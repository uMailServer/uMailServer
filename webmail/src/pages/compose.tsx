import { useState, useRef, useEffect } from "react"
import { useNavigate, useSearchParams } from "react-router-dom"
import {
  ArrowLeft,
  Send,
  Save,
  Paperclip,
  X,
  ChevronDown,
  Plus,
  Trash2,
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
  { id: "1", name: "Ahmet Yılmaz", email: "ahmet@example.com" },
  { id: "2", name: "Ayşe Demir", email: "ayse.demir@company.com" },
  { id: "3", name: "Mehmet Kaya", email: "mehmet.k@gmail.com" },
  { id: "4", name: "Zeynep Ak", email: "zeynep@outlook.com" },
]

export function ComposePage() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const [to, setTo] = useState<Recipient[]>([])
  const [cc, setCc] = useState<Recipient[]>([])
  const [bcc, setBcc] = useState<Recipient[]>([])

  // Handle reply/forward from URL params
  useEffect(() => {
    const replyTo = searchParams.get("replyTo")
    const subject = searchParams.get("subject")
    const forward = searchParams.get("forward")

    if (replyTo) {
      const contact = mockContacts.find((c) => c.email === replyTo)
      if (contact) {
        setTo([contact])
      } else {
        setTo([{ id: "reply", name: replyTo, email: replyTo }])
      }
    }
    if (subject) {
      // Subject is already set from searchParams
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

  const handleSend = () => {
    if (to.length === 0) {
      toast.error("Lütfen alıcı seçin")
      return
    }
    setSending(true)
    toast.success("E-posta gönderiliyor...")
    // Simulate sending
    setTimeout(() => {
      setSending(false)
      toast.success("E-posta gönderildi")
      navigate("/sent")
    }, 1500)
  }

  const handleSaveDraft = () => {
    toast.success("Taslak kaydedildi")
    navigate("/drafts")
  }

  const handleDiscard = () => {
    toast.info("E-posta iptal edildi")
    navigate("/inbox")
  }

  return (
    <div className="flex h-full flex-col">
      {/* Header */}
      <div className="flex items-center justify-between border-b px-4 py-3">
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="icon" onClick={handleDiscard}>
            <ArrowLeft className="h-5 w-5" />
          </Button>
          <span className="font-medium">Yeni İleti</span>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="icon" onClick={handleSaveDraft} title="Taslak olarak kaydet">
            <Save className="h-5 w-5" />
          </Button>
          <Button
            className="gap-2"
            onClick={handleSend}
            disabled={sending || to.length === 0}
          >
            <Send className="h-4 w-4" />
            {sending ? "Gönderiliyor..." : "Gönder"}
          </Button>
        </div>
      </div>

      {/* Compose Form */}
      <div className="flex-1 overflow-auto p-4">
        <div className="space-y-3">
          {/* To */}
          <div className="flex items-center gap-2">
            <span className="w-12 text-sm text-muted-foreground">Kime:</span>
            <div className="flex flex-1 flex-wrap items-center gap-1 rounded border px-2 py-1.5 min-h-[40px]">
              {to.map((r) => (
                <Badge key={r.id} variant="secondary" className="gap-1 pr-1">
                  {r.name}
                  <button
                    onClick={() => removeRecipient(r.id, "to")}
                    className="ml-1 rounded-full hover:bg-muted p-0.5"
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
                <DropdownMenuContent align="start" className="w-64">
                  <div className="p-2">
                    <Input
                      placeholder="Kişi ara..."
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
              className="text-xs"
              onClick={() => setShowCc(!showCc)}
            >
              Cc
            </Button>
            <Button
              variant="ghost"
              size="sm"
              className="text-xs"
              onClick={() => setShowBcc(!showBcc)}
            >
              Bcc
            </Button>
          </div>

          {/* Cc */}
          {showCc && (
            <div className="flex items-center gap-2">
              <span className="w-12 text-sm text-muted-foreground">Cc:</span>
              <div className="flex flex-1 flex-wrap items-center gap-1 rounded border px-2 py-1.5 min-h-[40px]">
                {cc.map((r) => (
                  <Badge key={r.id} variant="secondary" className="gap-1 pr-1">
                    {r.name}
                    <button
                      onClick={() => removeRecipient(r.id, "cc")}
                      className="ml-1 rounded-full hover:bg-muted p-0.5"
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
                  <DropdownMenuContent align="start" className="w-64">
                    <div className="p-2">
                      <Input
                        placeholder="Kişi ara..."
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

          {/* Bcc */}
          {showBcc && (
            <div className="flex items-center gap-2">
              <span className="w-12 text-sm text-muted-foreground">Bcc:</span>
              <div className="flex flex-1 flex-wrap items-center gap-1 rounded border px-2 py-1.5 min-h-[40px]">
                {bcc.map((r) => (
                  <Badge key={r.id} variant="secondary" className="gap-1 pr-1">
                    {r.name}
                    <button
                      onClick={() => removeRecipient(r.id, "bcc")}
                      className="ml-1 rounded-full hover:bg-muted p-0.5"
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
                  <DropdownMenuContent align="start" className="w-64">
                    <div className="p-2">
                      <Input
                        placeholder="Kişi ara..."
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

          {/* Subject */}
          <div className="flex items-center gap-2">
            <span className="w-12 text-sm text-muted-foreground">Konu:</span>
            <Input
              className="flex-1 border-0 shadow-none focus-visible:ring-0 px-0"
              placeholder="Konu yazın..."
              value={subject}
              onChange={(e) => setSubject(e.target.value)}
            />
          </div>

          <Separator />

          {/* Body */}
          <Textarea
            className="min-h-[300px] resize-none border-0 shadow-none focus-visible:ring-0"
            placeholder="İletinizi yazın..."
            value={body}
            onChange={(e) => setBody(e.target.value)}
          />

          {/* Attachments */}
          {attachments.length > 0 && (
            <div className="flex flex-wrap gap-2">
              {attachments.map((att) => (
                <div
                  key={att.id}
                  className="flex items-center gap-2 rounded border px-3 py-1.5"
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
          )}
        </div>
      </div>

      {/* Footer */}
      <div className="flex items-center justify-between border-t px-4 py-3">
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
          <kbd className="rounded border px-1.5 py-0.5 text-xs">Ctrl</kbd>
          <span>+</span>
          <kbd className="rounded border px-1.5 py-0.5 text-xs">Enter</kbd>
          <span>Gönder</span>
        </div>
      </div>
    </div>
  )
}
