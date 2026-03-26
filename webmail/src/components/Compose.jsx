import { useState } from 'react'
import { useEmail } from '../contexts/EmailContext'

function Compose({ onClose }) {
  const { sendEmail } = useEmail()
  const [to, setTo] = useState('')
  const [subject, setSubject] = useState('')
  const [body, setBody] = useState('')
  const [sending, setSending] = useState(false)

  const handleSubmit = async (e) => {
    e.preventDefault()
    setSending(true)
    const success = await sendEmail(to, subject, body)
    setSending(false)
    if (success) {
      onClose()
    }
  }

  return (
    <div className="compose-modal" onClick={onClose}>
      <div className="compose-box" onClick={e => e.stopPropagation()}>
        <div className="compose-header">
          <h3>New Message</h3>
          <button className="btn btn-secondary" onClick={onClose}>×</button>
        </div>
        <form onSubmit={handleSubmit}>
          <div className="compose-form">
            <div className="form-group">
              <input
                type="email"
                placeholder="To"
                value={to}
                onChange={e => setTo(e.target.value)}
                required
              />
            </div>
            <div className="form-group">
              <input
                type="text"
                placeholder="Subject"
                value={subject}
                onChange={e => setSubject(e.target.value)}
                required
              />
            </div>
            <textarea
              placeholder="Message body..."
              value={body}
              onChange={e => setBody(e.target.value)}
              required
            />
          </div>
          <div className="compose-actions">
            <button type="button" className="btn btn-secondary" onClick={onClose}>
              Cancel
            </button>
            <button type="submit" className="btn btn-primary" disabled={sending}>
              {sending ? 'Sending...' : 'Send'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

export default Compose
