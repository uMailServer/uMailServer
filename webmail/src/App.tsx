import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom"
import { ThemeProvider } from "@/components/theme-provider"
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

function AppContent() {
  useKeyboardShortcuts()

  return (
    <>
      <Routes>
        <Route path="/" element={<Layout />}>
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
      </Routes>
      <ShortcutsDialog />
    </>
  )
}

function App() {
  return (
    <ThemeProvider defaultTheme="system" storageKey="webmail-theme">
      <BrowserRouter>
        <AppContent />
      </BrowserRouter>
      <Toaster />
    </ThemeProvider>
  )
}

export default App
