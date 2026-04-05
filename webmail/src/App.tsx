import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom"
import { ThemeProvider } from "@/components/theme-provider"
import { Layout } from "@/components/layout/layout"
import { InboxPage } from "@/pages/inbox"
import { EmailDetailPage } from "@/pages/email-detail"
import { Toaster } from "@/components/ui/sonner"

function App() {
  return (
    <ThemeProvider defaultTheme="system" storageKey="webmail-theme">
      <BrowserRouter>
        <Routes>
          <Route path="/" element={<Layout />}>
            <Route index element={<Navigate to="/inbox" replace />} />
            <Route path="inbox" element={<InboxPage />} />
            <Route path="starred" element={<InboxPage />} />
            <Route path="sent" element={<InboxPage />} />
            <Route path="drafts" element={<InboxPage />} />
            <Route path="archive" element={<InboxPage />} />
            <Route path="trash" element={<InboxPage />} />
            <Route path="spam" element={<InboxPage />} />
            <Route path="email/:id" element={<EmailDetailPage />} />
          </Route>
        </Routes>
      </BrowserRouter>
      <Toaster />
    </ThemeProvider>
  )
}

export default App
