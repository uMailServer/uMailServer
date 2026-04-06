import { useState } from "react"
import { useNavigate } from "react-router-dom"
import { Mail, ArrowRight, CheckCircle2, X, ExternalLink } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"

interface WelcomeBannerProps {
  onDismiss?: () => void
}

export function WelcomeBanner({ onDismiss }: WelcomeBannerProps) {
  const navigate = useNavigate()
  const [dismissed, setDismissed] = useState(false)

  if (dismissed) return null

  const features = [
    "Send and receive emails with SMTP/IMAP",
    "Organize with folders, labels and filters",
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

export function SetupGuide() {
  const navigate = useNavigate()
  const [dismissed, setDismissed] = useState(false)

  if (dismissed) return null

  const steps = [
    { num: 1, title: "Configure your domain", desc: "Set up DNS records for your domain", done: true },
    { num: 2, title: "Add email accounts", desc: "Create user accounts in the admin panel", done: true },
    { num: 3, title: "Connect email client", desc: "Use IMAP/SMTP to connect your favorite email app", done: false },
    { num: 4, title: "Start using email", desc: "Send and receive emails securely", done: false },
  ]

  return (
    <div className="rounded-lg border bg-card p-6">
      <div className="flex items-center justify-between mb-4">
        <h3 className="font-semibold flex items-center gap-2">
          Quick Setup Guide
          <Badge variant="secondary">Getting Started</Badge>
        </h3>
        <Button variant="ghost" size="icon" className="h-8 w-8" onClick={() => setDismissed(true)}>
          <X className="h-4 w-4" />
        </Button>
      </div>
      <div className="space-y-3">
        {steps.map((step) => (
          <div key={step.num} className="flex items-center gap-3">
            <div className={cn(
              "flex h-8 w-8 items-center justify-center rounded-full text-sm font-medium",
              step.done ? "bg-primary text-primary-foreground" : "bg-muted text-muted-foreground"
            )}>
              {step.done ? <CheckCircle2 className="h-4 w-4" /> : step.num}
            </div>
            <div className="flex-1">
              <p className={cn("text-sm", step.done && "text-muted-foreground line-through")}>
                {step.title}
              </p>
              <p className="text-xs text-muted-foreground">{step.desc}</p>
            </div>
          </div>
        ))}
      </div>
      <div className="mt-4 pt-4 border-t flex gap-2">
        <Button variant="outline" size="sm" className="gap-2" onClick={() => navigate("/settings")}>
          Documentation
          <ExternalLink className="h-3 w-3" />
        </Button>
        <Button variant="outline" size="sm" className="gap-2">
          Admin Panel
          <ArrowRight className="h-3 w-3" />
        </Button>
      </div>
    </div>
  )
}

function cn(...classes: (string | boolean | undefined)[]) {
  return classes.filter(Boolean).join(" ")
}
