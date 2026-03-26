import { createContext, useContext, useState, useCallback } from 'react'
import api from '../utils/api'

const EmailContext = createContext(null)

const FOLDERS = ['Inbox', 'Sent', 'Drafts', 'Trash', 'Junk']

export function EmailProvider({ children }) {
  const [emails, setEmails] = useState([])
  const [currentFolder, setCurrentFolder] = useState('Inbox')
  const [selectedEmail, setSelectedEmail] = useState(null)
  const [loading, setLoading] = useState(false)
  const [composeOpen, setComposeOpen] = useState(false)
  const [folders] = useState(FOLDERS)

  const loadEmails = useCallback(async (folder = currentFolder) => {
    setLoading(true)
    try {
      // This would fetch from IMAP via API
      // For now, using mock data
      const mockEmails = [
        {
          id: 1,
          from: 'john@example.com',
          to: ['user@example.com'],
          subject: 'Welcome to uMailServer',
          preview: 'Welcome to your new mail server...',
          body: 'Welcome to uMailServer! This is your new email server.',
          date: new Date().toISOString(),
          read: false,
          folder: 'Inbox'
        },
        {
          id: 2,
          from: 'admin@example.com',
          to: ['user@example.com'],
          subject: 'Getting Started Guide',
          preview: 'Here is how to configure your server...',
          body: 'Here is how to configure your server. First, set up your DNS records...',
          date: new Date(Date.now() - 86400000).toISOString(),
          read: true,
          folder: 'Inbox'
        }
      ]
      setEmails(mockEmails)
    } catch (err) {
      console.error('Failed to load emails:', err)
    } finally {
      setLoading(false)
    }
  }, [currentFolder])

  const selectEmail = useCallback((email) => {
    setSelectedEmail(email)
    if (email && !email.read) {
      // Mark as read
      setEmails(prev => prev.map(e =>
        e.id === email.id ? { ...e, read: true } : e
      ))
    }
  }, [])

  const changeFolder = useCallback((folder) => {
    setCurrentFolder(folder)
    setSelectedEmail(null)
    loadEmails(folder)
  }, [loadEmails])

  const sendEmail = useCallback(async (to, subject, body) => {
    try {
      // This would send via SMTP API
      console.log('Sending email:', { to, subject, body })
      return true
    } catch (err) {
      console.error('Failed to send email:', err)
      return false
    }
  }, [])

  const getUnreadCount = useCallback((folder) => {
    if (folder === 'Inbox') {
      return emails.filter(e => !e.read && e.folder === 'Inbox').length
    }
    return 0
  }, [emails])

  const value = {
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
    getUnreadCount
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
