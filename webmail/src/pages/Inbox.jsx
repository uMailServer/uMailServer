import { useEffect, useState } from 'react'
import Header from '../components/Header'
import Sidebar from '../components/Sidebar'
import EmailList from '../components/EmailList'
import Compose from '../components/Compose'
import { useEmail } from '../contexts/EmailContext'

function Inbox() {
  const { loadEmails, composeOpen, setComposeOpen } = useEmail()
  const [isComposeOpen, setIsComposeOpen] = useState(false)

  useEffect(() => {
    loadEmails()
  }, [loadEmails])

  return (
    <div className="app">
      <Header onCompose={() => setIsComposeOpen(true)} />
      <div className="main-container">
        <Sidebar />
        <main className="content">
          <EmailList />
        </main>
      </div>
      {isComposeOpen && <Compose onClose={() => setIsComposeOpen(false)} />}
    </div>
  )
}

export default Inbox
