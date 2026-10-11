import { FileText } from "lucide-react"
import { MailFolderList } from "@/components/mail-folder-list"

export function DraftsPage() {
  return (
    <MailFolderList folder="drafts" icon={FileText} showRecipients
      emptyTitle="No drafts" emptyText="Drafts you save will appear here." />
  )
}
