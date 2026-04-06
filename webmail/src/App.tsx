import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom"
import { ThemeProvider } from "@/components/theme-provider"
import { Layout } from "@/components/layout/layout"
import { InboxPage } from "@/pages/inbox"
import { EmailDetailPage } from "@/pages/email-detail"
import { ComposePage } from "@/pages/compose"
import { SentPage } from "@/pages/sent"
import { DraftsPage } from "@/pages/drafts"
import { TrashPage } from "@/pages/trash"
import { Toaster } from "@/components/ui/sonner"

function App() {
  return (
    <ThemeProvider defaultTheme="system" storageKey="webmail-theme">
      <BrowserRouter>
        <Routes>
          <Route path="/" element={<Layout />}>
            <Route index element={<Navigate to="/inbox" replace />} />
            <Route path="compose" element={<ComposePage />} />
            <Route path="inbox" element={<InboxPage folder="inbox" />} />
            <Route path="starred" element={<InboxPage folder="starred" />} />
            <Route path="sent" element={<SentPage />} />
            <Route path="drafts" element={<DraftsPage />} />
            <Route path="trash" element={<TrashPage />} />
            <Route path="email/:id" element={<EmailDetailPage />} />
          </Route>
        </Routes>
      </BrowserRouter>
      <Toaster />
    </ThemeProvider>
  )
}

export default App
