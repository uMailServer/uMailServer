import { MailOpen } from "lucide-react"
import { MailFolderList } from "@/components/mail-folder-list"

export function SentPage() {
  return (
    <MailFolderList folder="sent" icon={MailOpen} showRecipients
      emptyTitle="No sent emails" emptyText="Emails you send will appear here." />
  )
}
