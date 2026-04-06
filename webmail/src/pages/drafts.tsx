import { useState } from "react"
import { useNavigate } from "react-router-dom"
import {
  FileText,
  Trash2,
  Mail,
  RefreshCw,
  ChevronLeft,
  ChevronRight,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Skeleton } from "@/components/ui/skeleton"
import { Separator } from "@/components/ui/separator"

interface Draft {
  id: string
  to: string
  subject: string
  preview: string
  date: string
}

const mockDrafts: Draft[] = [
  {
    id: "d1",
    to: "mehmet@example.com",
    subject: "Toplantı Notları",
    preview: "Yarınki toplantı için hazırladığım notlar...",
    date: "15:30",
  },
  {
    id: "d2",
    to: "",
    subject: "",
    preview: "Taslak...",
    date: "Dün",
  },
]

export function DraftsPage() {
  const navigate = useNavigate()
  const [drafts] = useState<Draft[]>(mockDrafts)
  const [selectedDrafts, setSelectedDrafts] = useState<Set<string>>(new Set())
  const [loading, setLoading] = useState(false)

  const toggleSelectAll = () => {
    if (selectedDrafts.size === drafts.length) {
      setSelectedDrafts(new Set())
    } else {
      setSelectedDrafts(new Set(drafts.map((d) => d.id)))
    }
  }

  const toggleSelect = (id: string) => {
    const newSelected = new Set(selectedDrafts)
    if (newSelected.has(id)) {
      newSelected.delete(id)
    } else {
      newSelected.add(id)
    }
    setSelectedDrafts(newSelected)
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-2">
          <Checkbox
            checked={selectedDrafts.size === drafts.length && drafts.length > 0}
            onCheckedChange={toggleSelectAll}
          />
          {selectedDrafts.size > 0 && (
            <>
              <span className="text-sm text-muted-foreground">
                {selectedDrafts.size} seçildi
              </span>
              <Separator orientation="vertical" className="h-4" />
              <Button variant="ghost" size="icon" className="h-8 w-8 text-destructive">
                <Trash2 className="h-4 w-4" />
              </Button>
            </>
          )}
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
            {[1, 2].map((i) => (
              <div key={i} className="flex items-center gap-4 p-4">
                <Skeleton className="h-4 w-4" />
                <div className="flex-1 space-y-2">
                  <Skeleton className="h-4 w-32" />
                  <Skeleton className="h-3 w-full" />
                </div>
              </div>
            ))}
          </div>
        ) : drafts.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-16 text-center">
            <div className="rounded-full bg-muted p-4">
              <FileText className="h-8 w-8 text-muted-foreground" />
            </div>
            <h3 className="mt-4 text-lg font-semibold">Taslak yok</h3>
            <p className="text-sm text-muted-foreground">
              Kaydettiğiniz taslaklar burada görünür.
            </p>
          </div>
        ) : (
          <div className="divide-y">
            {drafts.map((draft) => (
              <div
                key={draft.id}
                className={cn(
                  "group flex cursor-pointer items-start gap-3 p-4 transition-colors hover:bg-accent/50"
                )}
                onClick={() => navigate("/compose")}
              >
                <Checkbox
                  checked={selectedDrafts.has(draft.id)}
                  onCheckedChange={() => toggleSelect(draft.id)}
                  onClick={(e) => e.stopPropagation()}
                />
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 text-sm">
                    <span className="text-muted-foreground">
                      {draft.to || "(Alıcı yok)"} →
                    </span>
                    <span className={cn("truncate", !draft.subject && "text-muted-foreground italic")}>
                      {draft.subject || "Konu yok"}
                    </span>
                  </div>
                  <div className="truncate text-sm text-muted-foreground">
                    {draft.preview}
                  </div>
                </div>
                <span className="whitespace-nowrap text-sm text-muted-foreground">
                  {draft.date}
                </span>
              </div>
            ))}
          </div>
        )}
      </div>

      <div className="flex items-center justify-between">
        <span className="text-sm text-muted-foreground">
          {drafts.length} taslak
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
