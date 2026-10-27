import { useParams } from "react-router-dom"
import { InboxPage } from "./inbox"

/** A mailbox of the user's own (created over IMAP, by a filter, ...). */
export function FolderPage() {
  const { type } = useParams()
  return <InboxPage key={type} folder={type ?? ""} />
}
