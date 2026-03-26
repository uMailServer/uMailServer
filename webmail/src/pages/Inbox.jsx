import { useEffect, useState } from 'react'
import Header from '../components/Header'
import Sidebar from '../components/Sidebar'
import EmailList from '../components/EmailList'
import Compose from '../components/Compose'
import { useEmail } from '../contexts/EmailContext'

function Inbox() {
  const { loadEmails } = useEmail()
  const [isComposeOpen, setIsComposeOpen] = useState(false)

  useEffect(() => {
    loadEmails()
  }, [loadEmails])

  return (
    <div className="min-h-screen flex flex-col bg-background">
      <Header onCompose={() => setIsComposeOpen(true)} />
      <div className="flex-1 flex overflow-hidden">
        <Sidebar />
        <main className="flex-1 flex flex-col overflow-hidden">
          <EmailList />
        </main>
      </div>
      {isComposeOpen && <Compose onClose={() => setIsComposeOpen(false)} />}
    </div>
  )
}

export default Inbox
