import { useEffect } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { useEmail } from '../contexts/EmailContext'
import { formatFullDate } from '../utils/date'

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
    return <div className="loading">Loading...</div>
  }

  return (
    <div className="app">
      <div className="main-container" style={{ paddingTop: '1rem' }}>
        <main className="content">
          <button className="btn btn-secondary btn-back" onClick={handleBack}>
            ← Back
          </button>
          <div className="email-reader">
            <div className="email-header">
              <h2 className="email-subject-line">{selectedEmail.subject}</h2>
              <div className="email-meta">
                <span>From: {selectedEmail.from}</span>
                <span>To: {selectedEmail.to.join(', ')}</span>
                <span>{formatFullDate(selectedEmail.date)}</span>
              </div>
            </div>
            <div className="email-actions">
              <button className="btn btn-primary">Reply</button>
              <button className="btn btn-secondary">Forward</button>
              <button className="btn btn-danger">Delete</button>
            </div>
            <div className="email-body">
              {selectedEmail.body}
            </div>
          </div>
        </main>
      </div>
    </div>
  )
}

export default EmailReader
