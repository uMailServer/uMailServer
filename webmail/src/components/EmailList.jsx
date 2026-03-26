import { useNavigate } from 'react-router-dom'
import { useEmail } from '../contexts/EmailContext'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { ScrollArea } from '@/components/ui/scroll-area'
import { formatDate } from '../utils/date'
import { Loader2, Mail, MailOpen, AlertCircle } from 'lucide-react'

function EmailList() {
  const { emails, loading, selectEmail, currentFolder } = useEmail()
  const navigate = useNavigate()

  const handleEmailClick = (email) => {
    selectEmail(email)
    navigate(`/email/${email.id}`)
  }

  if (loading) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    )
  }

  const folderEmails = emails.filter(e => e.folder === currentFolder)

  if (folderEmails.length === 0) {
    return (
      <div className="flex-1 flex flex-col items-center justify-center text-muted-foreground p-8">
        <MailOpen className="h-16 w-16 mb-4 opacity-50" />
        <h3 className="text-lg font-medium mb-1">No emails</h3>
        <p>Your {currentFolder.toLowerCase()} is empty</p>
      </div>
    )
  }

  return (
    <ScrollArea className="flex-1">
      <div className="divide-y">
        {folderEmails.map((email) => (
          <div
            key={email.id}
            onClick={() => handleEmailClick(email)}
            className={`
              flex items-start gap-4 p-4 cursor-pointer transition-colors
              hover:bg-muted/50
              ${!email.read ? 'bg-blue-50/50' : ''}
            `}
          >
            <div className="mt-1">
              {!email.read ? (
                <Mail className="h-5 w-5 text-blue-600" />
              ) : (
                <MailOpen className="h-5 w-5 text-muted-foreground" />
              )}
            </div>

            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-2 mb-1">
                <span className={`font-medium truncate ${!email.read ? 'text-foreground' : 'text-muted-foreground'}`}>
                  {email.from}
                </span>
                {!email.read && (
                  <Badge variant="default" className="h-5 px-1.5 text-xs">New</Badge>
                )}
              </div>
              <h4 className={`text-sm truncate mb-1 ${!email.read ? 'font-semibold' : ''}`}>
                {email.subject}
              </h4>
              <p className="text-sm text-muted-foreground truncate">
                {email.preview}
              </p>
            </div>

            <div className="text-xs text-muted-foreground whitespace-nowrap">
              {formatDate(email.date)}
            </div>
          </div>
        ))}
      </div>
    </ScrollArea>
  )
}

export default EmailList
