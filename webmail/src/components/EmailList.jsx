import { useNavigate } from 'react-router-dom'
import { useEmail } from '../contexts/EmailContext'
import { formatDate } from '../utils/date'

function EmailList() {
  const { emails, loading, selectEmail, currentFolder } = useEmail()
  const navigate = useNavigate()

  const handleEmailClick = (email) => {
    selectEmail(email)
    navigate(`/email/${email.id}`)
  }

  if (loading) {
    return <div className="loading">Loading...</div>
  }

  const folderEmails = emails.filter(e => e.folder === currentFolder)

  if (folderEmails.length === 0) {
    return (
      <div className="empty-state">
        <h3>No emails</h3>
        <p>Your {currentFolder.toLowerCase()} is empty</p>
      </div>
    )
  }

  return (
    <div className="email-list">
      {folderEmails.map(email => (
        <div
          key={email.id}
          className={`email-item ${email.read ? '' : 'unread'}`}
          onClick={() => handleEmailClick(email)}
        >
          <div className="email-sender">{email.from}</div>
          <div className="email-info">
            <div className="email-subject">{email.subject}</div>
            <div className="email-preview">{email.preview}</div>
          </div>
          <div className="email-date">{formatDate(email.date)}</div>
        </div>
      ))}
    </div>
  )
}

export default EmailList
