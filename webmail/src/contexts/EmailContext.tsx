import { createContext, useContext, useState, useCallback, useEffect } from 'react'
import api from '../utils/api'

interface Email {
  id: string
  from: string
  fromName?: string
  to: string[]
  subject: string
  body: string
  preview: string
  date: string
  read: boolean
  starred: boolean
  folder: string
  hasAttachments?: boolean
  size?: number
}

interface EmailContextType {
  emails: Email[]
  folders: string[]
  currentFolder: string
  selectedEmail: Email | null
  loading: boolean
  composeOpen: boolean
  setComposeOpen: (open: boolean) => void
  loadEmails: (folder?: string) => Promise<void>
  selectEmail: (email: Email | null) => void
  changeFolder: (folder: string) => void
  sendEmail: (to: string[], subject: string, body: string) => Promise<boolean>
  getUnreadCount: (folder: string) => number
  refreshEmails: () => Promise<void>
}

const EmailContext = createContext<EmailContextType | null>(null)

const FOLDER_MAP: Record<string, string> = {
  'Inbox': 'inbox',
  'Sent': 'sent',
  'Drafts': 'drafts',
  'Trash': 'trash',
  'Spam': 'spam',
}

const REVERSE_FOLDER_MAP: Record<string, string> = {
  'inbox': 'Inbox',
  'sent': 'Sent',
  'drafts': 'Drafts',
  'trash': 'Trash',
  'spam': 'Spam',
}

export function EmailProvider({ children }: { children: React.ReactNode }) {
  const [emails, setEmails] = useState<Email[]>([])
  const [currentFolder, setCurrentFolder] = useState('Inbox')
  const [selectedEmail, setSelectedEmail] = useState<Email | null>(null)
  const [loading, setLoading] = useState(false)
  const [composeOpen, setComposeOpen] = useState(false)
  const folders = ['Inbox', 'Sent', 'Drafts', 'Trash', 'Spam']

  const loadEmails = useCallback(async (folder = currentFolder) => {
    setLoading(true)
    try {
      const apiFolder = FOLDER_MAP[folder] || folder.toLowerCase()
      const data = await api.get(`/mail/${apiFolder}`)

      if (data && data.emails) {
        const mappedEmails = data.emails.map((email: any) => ({
          ...email,
          folder: REVERSE_FOLDER_MAP[email.folder] || email.folder,
        }))
        setEmails(mappedEmails)
      } else {
        setEmails([])
      }
    } catch (err) {
      console.error('Failed to load emails:', err)
      setEmails([])
    } finally {
      setLoading(false)
    }
  }, [currentFolder])

  const selectEmail = useCallback((email: Email | null) => {
    setSelectedEmail(email)
    if (email && !email.read) {
      // Mark as read
      setEmails(prev => prev.map(e =>
        e.id === email.id ? { ...e, read: true } : e
      ))
    }
  }, [])

  const changeFolder = useCallback((folder: string) => {
    setCurrentFolder(folder)
    setSelectedEmail(null)
    loadEmails(folder)
  }, [loadEmails])

  const sendEmail = useCallback(async (to: string[], subject: string, body: string) => {
    try {
      await api.post('/mail/send', { to, subject, body })
      return true
    } catch (err) {
      console.error('Failed to send email:', err)
      return false
    }
  }, [])

  const getUnreadCount = useCallback((folder: string) => {
    if (folder === 'Inbox') {
      return emails.filter(e => !e.read && e.folder === 'Inbox').length
    }
    return 0
  }, [emails])

  const refreshEmails = useCallback(async () => {
    await loadEmails(currentFolder)
  }, [currentFolder, loadEmails])

  // Load emails on mount
  useEffect(() => {
    loadEmails()
  }, [])

  const value: EmailContextType = {
    emails,
    folders,
    currentFolder,
    selectedEmail,
    loading,
    composeOpen,
    setComposeOpen,
    loadEmails,
    selectEmail,
    changeFolder,
    sendEmail,
    getUnreadCount,
    refreshEmails,
  }

  return (
    <EmailContext.Provider value={value}>
      {children}
    </EmailContext.Provider>
  )
}

export function useEmail() {
  const context = useContext(EmailContext)
  if (!context) {
    throw new Error('useEmail must be used within an EmailProvider')
  }
  return context
}
