import { useAuth } from '../contexts/AuthContext'

function Header({ onCompose }) {
  const { user, logout } = useAuth()

  return (
    <header className="header">
      <h1>uMailServer</h1>
      <div className="header-right">
        <span className="user-email">{user?.email || 'User'}</span>
        <button className="btn btn-secondary" onClick={onCompose}>
          + Compose
        </button>
        <button className="btn btn-danger" onClick={logout}>
          Logout
        </button>
      </div>
    </header>
  )
}

export default Header
