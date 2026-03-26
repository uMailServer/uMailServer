import { useEffect } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { useEmail } from '../contexts/EmailContext'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Separator } from '@/components/ui/separator'
import { formatFullDate } from '../utils/date'
import {
  ArrowLeft,
  Reply,
  Forward,
  Trash2,
  Archive,
  MoreVertical,
  Mail,
  User,
  Clock
} from 'lucide-react'

function EmailReader() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { emails, selectedEmail, selectEmail } = useEmail()

  useEffect(() => {
    if (!selectedEmail || selectedEmail.id !== parseInt(id)) {
      const email = emails.find(e => e.id === parseInt(id))
      if (email) {
        selectEmail(email)
      } else {
        navigate('/')
      }
    }
  }, [id, emails, selectedEmail, selectEmail, navigate])

  const handleBack = () => {
    selectEmail(null)
    navigate('/')
  }

  if (!selectedEmail) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <div className="animate-spin h-8 w-8 border-4 border-primary border-t-transparent rounded-full" />
      </div>
    )
  }

  return (
    <div className="flex-1 flex flex-col bg-background">
      <div className="flex items-center gap-2 p-4 border-b">
        <Button variant="ghost" size="icon" onClick={handleBack}>
          <ArrowLeft className="h-4 w-4" />
        </Button>
        <div className="flex-1" />
        <Button variant="ghost" size="icon">
          <Archive className="h-4 w-4" />
        </Button>
        <Button variant="ghost" size="icon">
          <Trash2 className="h-4 w-4" />
        </Button>
        <Button variant="ghost" size="icon">
          <MoreVertical className="h-4 w-4" />
        </Button>
      </div>

      <ScrollArea className="flex-1">
        <div className="max-w-4xl mx-auto p-6">
          <h1 className="text-2xl font-semibold mb-6">{selectedEmail.subject}</h1>

          <Card className="mb-6">
            <CardHeader className="pb-4">
              <div className="flex items-start gap-4">
                <div className="bg-gradient-to-br from-violet-600 to-indigo-700 p-3 rounded-full">
                  <User className="h-6 w-6 text-white" />
                </div>
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 mb-1">
                    <span className="font-semibold">{selectedEmail.from}</span>
                  </div>
                  <div className="flex items-center gap-4 text-sm text-muted-foreground">
                    <div className="flex items-center gap-1">
                      <span>to:</span>
                      <span>{selectedEmail.to.join(', ')}</span>
                    </div>
                  </div>
                  <div className="flex items-center gap-2 mt-2 text-sm text-muted-foreground">
                    <Clock className="h-3 w-3" />
                    <span>{formatFullDate(selectedEmail.date)}</span>
                  </div>
                </div>
              </div>
            </CardHeader>
          </Card>

          <div className="flex gap-2 mb-6">
            <Button className="gap-2">
              <Reply className="h-4 w-4" />
              Reply
            </Button>
            <Button variant="outline" className="gap-2">
              <Forward className="h-4 w-4" />
              Forward
            </Button>
          </div>

          <Separator className="mb-6" />

          <div className="prose max-w-none">
            <p className="text-lg leading-relaxed whitespace-pre-wrap">
              {selectedEmail.body}
            </p>
          </div>
        </div>
      </ScrollArea>
    </div>
  )
}

export default EmailReader
