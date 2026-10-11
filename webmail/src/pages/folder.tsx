import { useParams, useNavigate, useLocation } from "react-router-dom"
import { FolderOpen, ChevronLeft } from "lucide-react"
import { Button } from "@/components/ui/button"

const folderConfig: Record<string, string> = { work: "Work", personal: "Personal" }
const tagConfig: Record<string, string> = { important: "Important" }

/**
 * Custom folders and tags have no backing API endpoint (only
 * inbox/sent/drafts/trash/spam exist), so this page never lists messages.
 */
export function FolderPage() {
  const { type } = useParams()
  const navigate = useNavigate()
  const { pathname } = useLocation()
  const isTag = pathname.startsWith("/tag")
  const label = (isTag ? tagConfig : folderConfig)[type || ""]

  return (
    <div className="flex flex-col items-center justify-center py-16 text-center">
      <FolderOpen className="h-12 w-12 text-muted-foreground" />
      <h3 className="mt-4 text-lg font-semibold">
        {label ? `${label} is not available` : isTag ? "Tag not found" : "Folder not found"}
      </h3>
      <p className="text-sm text-muted-foreground">
        {label
          ? "The server does not support custom folders or tags yet."
          : `"${type}" does not exist.`}
      </p>
      <Button variant="outline" className="mt-4" onClick={() => navigate("/inbox")}>
        <ChevronLeft className="h-4 w-4 mr-1" />
        Go Back
      </Button>
    </div>
  )
}
