import { Trash2 } from "lucide-react"
import { MailFolderList } from "@/components/mail-folder-list"

export function TrashPage() {
  return (
    <MailFolderList folder="trash" icon={Trash2} restoreTo="inbox"
      emptyTitle="Trash is empty" emptyText="Deleted messages will appear here." />
  )
}
