import { BrowserRouter as Router, Routes, Route, Navigate } from 'react-router-dom'
import { AuthProvider } from './contexts/AuthContext'
import { EmailProvider } from './contexts/EmailContext'
import Login from './pages/Login'
import Inbox from './pages/Inbox'
import EmailReader from './pages/EmailReader'
import ProtectedRoute from './components/ProtectedRoute'
import './index.css'

function App() {
  return (
    <AuthProvider>
      <EmailProvider>
        <Router>
          <Routes>
            <Route path="/login" element={<Login />} />
            <Route
              path="/"
              element={
                <ProtectedRoute>
                  <Inbox />
                </ProtectedRoute>
              }
            />
            <Route
              path="/email/:id"
              element={
                <ProtectedRoute>
                  <EmailReader />
                </ProtectedRoute>
              }
            />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </Router>
      </EmailProvider>
    </AuthProvider>
  )
}

export default App
