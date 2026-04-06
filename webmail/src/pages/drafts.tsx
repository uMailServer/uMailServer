import { useState } from "react"
import { useNavigate } from "react-router-dom"
import {
  FileText,
  Trash2,
  Mail,
  RefreshCw,
  ChevronLeft,
  ChevronRight,
  Edit,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Skeleton } from "@/components/ui/skeleton"
import { Separator } from "@/components/ui/separator"
import { toast } from "sonner"

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
    to: "colleague@company.com",
    subject: "Meeting Notes",
    preview: "Notes I prepared for tomorrow's meeting...",
    date: "15:30",
  },
  {
    id: "d2",
    to: "",
    subject: "",
    preview: "Draft...",
    date: "Yesterday",
  },
]

export function DraftsPage() {
  const navigate = useNavigate()
  const [drafts, setDrafts] = useState<Draft[]>(mockDrafts)
  const [selectedDrafts, setSelectedDrafts] = useState<Set<string>>(new Set())
  const [loading, setLoading] = useState(false)

  const toggleSelectAll = () => {
    if (selectedDrafts.size === drafts.length) {
      setSelectedDrafts(new Set())
    } else {
      setSelectedDrafts(new Set(drafts.map((e) => e.id)))
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

  const handleDelete = () => {
    toast.success(`${selectedDrafts.size} draft${selectedDrafts.size !== 1 ? "s" : ""} deleted`)
    setDrafts(drafts.filter((d) => !selectedDrafts.has(d.id)))
    setSelectedDrafts(new Set())
  }

  const handleEdit = (id: string) => {
    navigate(`/compose?draft=${id}`)
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-2">
          <Checkbox
            checked={selectedDrafts.size === drafts.length && drafts.length > 0}
            onCheckedChange={toggleSelectAll}
          />
          {selectedDrafts.size > 0 ? (
            <>
              <span className="text-sm text-muted-foreground">
                {selectedDrafts.size} selected
              </span>
              <Separator orientation="vertical" className="h-4" />
              <Button
                variant="ghost"
                size="icon"
                className="h-8 w-8 text-destructive"
                onClick={handleDelete}
              >
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
        ) : drafts.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-16 text-center">
            <div className="rounded-full bg-muted p-4">
              <FileText className="h-8 w-8 text-muted-foreground" />
            </div>
            <h3 className="mt-4 text-lg font-semibold">No drafts</h3>
            <p className="text-sm text-muted-foreground">
              Drafts you save will appear here.
            </p>
          </div>
        ) : (
          <div className="divide-y">
            {drafts.map((draft) => (
              <div
                key={draft.id}
                className="group flex cursor-pointer items-center gap-3 p-4 transition-colors hover:bg-accent/50"
                onClick={() => handleEdit(draft.id)}
              >
                <Checkbox
                  checked={selectedDrafts.has(draft.id)}
                  onCheckedChange={() => toggleSelect(draft.id)}
                  onClick={(e) => e.stopPropagation()}
                />
                <FileText className="h-4 w-4 text-muted-foreground" />
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="truncate font-medium">
                      {draft.subject || "No subject"}
                    </span>
                  </div>
                  <div className="flex items-center gap-2 text-sm text-muted-foreground">
                    <span className="truncate">To: {draft.to || "No recipient"}</span>
                    <span className="truncate">— {draft.preview}</span>
                  </div>
                </div>
                <span className="whitespace-nowrap text-sm text-muted-foreground">
                  {draft.date}
                </span>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-8 w-8 opacity-0 group-hover:opacity-100"
                  onClick={(e) => {
                    e.stopPropagation()
                    handleEdit(draft.id)
                  }}
                >
                  <Edit className="h-4 w-4" />
                </Button>
              </div>
            ))}
          </div>
        )}
      </div>

      <div className="flex items-center justify-between">
        <span className="text-sm text-muted-foreground">
          {drafts.length} draft{drafts.length !== 1 ? "s" : ""}
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
