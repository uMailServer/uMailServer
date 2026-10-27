import { useState } from "react"
import { useNavigate } from "react-router-dom"
import { Mail, CheckCircle2, X } from "lucide-react"
import { Button } from "@/components/ui/button"

interface WelcomeBannerProps {
  onDismiss?: () => void
}

export function WelcomeBanner({ onDismiss }: WelcomeBannerProps) {
  const navigate = useNavigate()
  const [dismissed, setDismissed] = useState(false)

  if (dismissed) return null

  const features = [
    "Send and receive emails with SMTP/IMAP",
    "Organize with folders and filters",
    "Powerful search across all messages",
    "Keyboard shortcuts for fast navigation",
  ]

  return (
    <div className="rounded-lg border bg-gradient-to-r from-primary/5 via-primary/10 to-primary/5 p-6">
      <div className="flex items-start justify-between gap-4">
        <div className="flex gap-4">
          <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-xl bg-gradient-to-br from-primary to-primary/80 shadow-lg shadow-primary/25">
            <Mail className="h-6 w-6 text-primary-foreground" />
          </div>
          <div>
            <h2 className="text-xl font-bold">Welcome to uMail Webmail</h2>
            <p className="text-muted-foreground mt-1">
              Your secure, self-hosted email solution
            </p>
            <div className="mt-4 grid gap-2 sm:grid-cols-2">
              {features.map((feature, index) => (
                <div key={index} className="flex items-center gap-2 text-sm">
                  <CheckCircle2 className="h-4 w-4 text-primary shrink-0" />
                  <span>{feature}</span>
                </div>
              ))}
            </div>
            <div className="flex gap-2 mt-4">
              <Button onClick={() => navigate("/compose")}>
                <Mail className="h-4 w-4 mr-2" />
                Compose Email
              </Button>
              <Button variant="outline" onClick={() => navigate("/settings")}>
                Customize
              </Button>
            </div>
          </div>
        </div>
        <Button
          variant="ghost"
          size="icon"
          className="shrink-0"
          onClick={() => {
            setDismissed(true)
            onDismiss?.()
          }}
        >
          <X className="h-4 w-4" />
        </Button>
      </div>
    </div>
  )
}
