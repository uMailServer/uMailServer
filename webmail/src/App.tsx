import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom"
import { ThemeProvider } from "@/components/theme-provider"
import { AuthProvider, useAuth } from "@/contexts/AuthContext"
import { Layout } from "@/components/layout/layout"
import { InboxPage } from "@/pages/inbox"
import { EmailDetailPage } from "@/pages/email-detail"
import { ComposePage } from "@/pages/compose"
import { SentPage } from "@/pages/sent"
import { DraftsPage } from "@/pages/drafts"
import { TrashPage } from "@/pages/trash"
import { ContactsPage } from "@/pages/contacts"
import { SettingsPage } from "@/pages/settings"
import { SearchPage } from "@/pages/search"
import { SpamPage } from "@/pages/spam"
import { FolderPage } from "@/pages/folder"
import { ShortcutsDialog } from "@/components/shortcuts-dialog"
import { Toaster } from "@/components/ui/sonner"
import { useKeyboardShortcuts } from "@/hooks/useKeyboardShortcuts"
import { LoginPage } from "@/pages/login"

function ProtectedRoute({ children }) {
  const { isAuthenticated, isLoading } = useAuth()

  if (isLoading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-indigo-600"></div>
      </div>
    )
  }

  if (!isAuthenticated) {
    return <Navigate to="/login" replace />
  }

  return children
}

function AppContent() {
  useKeyboardShortcuts()

  return (
    <>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route
          path="/"
          element={
            <ProtectedRoute>
              <Layout />
            </ProtectedRoute>
          }
        >
          <Route index element={<Navigate to="/inbox" replace />} />
          <Route path="compose" element={<ComposePage />} />
          <Route path="inbox" element={<InboxPage folder="inbox" />} />
          <Route path="starred" element={<InboxPage folder="starred" />} />
          <Route path="sent" element={<SentPage />} />
          <Route path="drafts" element={<DraftsPage />} />
          <Route path="trash" element={<TrashPage />} />
          <Route path="contacts" element={<ContactsPage />} />
          <Route path="settings" element={<SettingsPage />} />
          <Route path="search" element={<SearchPage />} />
          <Route path="spam" element={<SpamPage />} />
          <Route path="folder/:type" element={<FolderPage />} />
          <Route path="tag/:type" element={<FolderPage />} />
          <Route path="email/:id" element={<EmailDetailPage />} />
        </Route>
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
      <ShortcutsDialog />
    </>
  )
}

function App() {
  return (
    <ThemeProvider defaultTheme="system" storageKey="webmail-theme">
      <AuthProvider>
        <BrowserRouter>
          <AppContent />
        </BrowserRouter>
        <Toaster />
      </AuthProvider>
    </ThemeProvider>
  )
}

export default App
