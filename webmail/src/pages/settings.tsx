import { useEffect, useState } from "react"
import { Moon, Sun, Bell, Palette, Keyboard, Mail, Globe, Lock } from "lucide-react"
import { useTheme } from "@/components/theme-provider"
import { Button } from "@/components/ui/button"
import { Separator } from "@/components/ui/separator"
import { Switch } from "@/components/ui/switch"
import { toast } from "sonner"
import { readPreferences, writePreferences, type Preferences } from "@/utils/preferences"
import { disablePush, enablePush, getPushState, type PushState } from "@/utils/push"

const PUSH_HINT: Record<PushState, string> = {
  unsupported: "This browser does not support push notifications",
  denied: "Notifications are blocked for this site; allow them in the browser settings",
  off: "Get a notification for new mail, even when this tab is closed",
  on: "This browser receives a notification for new mail",
}

export function SettingsPage() {
  const { theme, setTheme, resolvedTheme } = useTheme()
  const [prefs, setPrefs] = useState<Preferences>(readPreferences)
  const [push, setPush] = useState<PushState | null>(null)
  const [pushBusy, setPushBusy] = useState(false)

  useEffect(() => {
    let cancelled = false
    getPushState().then((st) => { if (!cancelled) setPush(st) }).catch(() => { if (!cancelled) setPush("off") })
    return () => { cancelled = true }
  }, [])

  // These preferences are stored only in this browser (the server has no
  // settings endpoint); say so and report a failed write honestly.
  const handleToggle = (key: keyof Preferences) => {
    const next = { ...prefs, [key]: !prefs[key] }
    setPrefs(next)
    try {
      writePreferences(next)
      toast.success("Setting saved on this device")
    } catch {
      toast.error("Could not save setting; it will reset on reload")
    }
  }

  const togglePush = async () => {
    if (pushBusy || push === null) return
    setPushBusy(true)
    try {
      if (push === "on") {
        await disablePush()
        toast.success("Push notifications turned off for this browser")
      } else {
        await enablePush()
        toast.success("Push notifications turned on for this browser")
      }
    } catch (err) {
      toast.error(err instanceof Error && err.message ? err.message : "Could not change push notifications")
    } finally {
      setPush(await getPushState().catch(() => "off" as PushState))
      setPushBusy(false)
    }
  }

  const SettingSection = ({
    icon: Icon,
    title,
    description,
    children
  }: {
    icon: React.ElementType
    title: string
    description: string
    children: React.ReactNode
  }) => (
    <div className="rounded-lg border bg-card">
      <div className="flex items-center gap-4 p-6 pb-4">
        <div className="rounded-full bg-muted p-2">
          <Icon className="h-5 w-5" />
        </div>
        <div>
          <h3 className="font-semibold">{title}</h3>
          <p className="text-sm text-muted-foreground">{description}</p>
        </div>
      </div>
      <div className="px-6 pb-6">
        {children}
      </div>
    </div>
  )

  const SettingRow = ({
    title,
    description,
    checked,
    onChange,
    disabled = false,
  }: {
    title: string
    description: string
    checked: boolean
    onChange: () => void
    disabled?: boolean
  }) => (
    <div className="flex items-center justify-between py-3">
      <div>
        <p className="font-medium">{title}</p>
        <p className="text-sm text-muted-foreground">{description}</p>
      </div>
      <Switch aria-label={title} checked={checked} onCheckedChange={onChange} disabled={disabled} />
    </div>
  )

  return (
    <div className="space-y-6 max-w-3xl">
      <div>
        <h2 className="text-2xl font-bold">Settings</h2>
        <p className="text-muted-foreground">
          Manage your email preferences and account settings.
        </p>
      </div>

      {/* Appearance */}
      <SettingSection
        icon={Palette}
        title="Appearance"
        description="Customize how uMail looks on your device"
      >
        <div className="space-y-4">
          <div className="flex items-center justify-between">
            <div>
              <p className="font-medium">Theme</p>
              <p className="text-sm text-muted-foreground">Choose your preferred color scheme</p>
            </div>
            <div className="flex gap-2">
              <Button
                variant={theme === "light" ? "default" : "outline"}
                size="icon"
                onClick={() => setTheme("light")}
                title="Light mode"
              >
                <Sun className="h-4 w-4" />
              </Button>
              <Button
                variant={theme === "dark" ? "default" : "outline"}
                size="icon"
                onClick={() => setTheme("dark")}
                title="Dark mode"
              >
                <Moon className="h-4 w-4" />
              </Button>
              <Button
                variant={theme === "system" ? "default" : "outline"}
                size="icon"
                onClick={() => setTheme("system")}
                title="System default"
              >
                <Globe className="h-4 w-4" />
              </Button>
            </div>
          </div>
          <Separator />
          <SettingRow
            title="Dark mode"
            description="Use dark theme"
            checked={theme === "dark"}
            onChange={() => setTheme(resolvedTheme === "dark" ? "light" : "dark")}
          />
        </div>
      </SettingSection>

      {/* Notifications */}
      <SettingSection
        icon={Bell}
        title="Notifications"
        description="Configure how you receive alerts for new messages"
      >
        <SettingRow
          title="Push notifications"
          description={push ? PUSH_HINT[push] : "Checking this browser..."}
          checked={push === "on"}
          onChange={() => void togglePush()}
          disabled={push === null || push === "unsupported" || push === "denied" || pushBusy}
        />
      </SettingSection>

      {/* Composition */}
      <SettingSection
        icon={Mail}
        title="Email Composition"
        description="Settings for composing emails, saved on this device"
      >
        <div className="space-y-1">
          <SettingRow
            title="Auto-save drafts"
            description="Save the message to Drafts while you type"
            checked={prefs.autoSaveDraft}
            onChange={() => handleToggle("autoSaveDraft")}
          />
          <Separator />
          <SettingRow
            title="Spell check"
            description="Let the browser check spelling while you type"
            checked={prefs.spellCheck}
            onChange={() => handleToggle("spellCheck")}
          />
        </div>
      </SettingSection>

      {/* Keyboard Shortcuts */}
      <SettingSection
        icon={Keyboard}
        title="Keyboard Shortcuts"
        description="View and manage keyboard shortcuts"
      >
        <div className="space-y-3">
          <p className="text-sm text-muted-foreground">
            Keyboard shortcuts help you navigate and perform actions faster.
          </p>
          <Button
            variant="outline"
            onClick={() => document.dispatchEvent(new CustomEvent("toggle-shortcuts"))}
          >
            View Keyboard Shortcuts
          </Button>
        </div>
      </SettingSection>

      {/* Account */}
      <div className="rounded-lg border bg-card p-6">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-4">
            <div className="rounded-full bg-destructive/10 p-2">
              <Lock className="h-5 w-5 text-destructive" />
            </div>
            <div>
              <h3 className="font-semibold">Account Security</h3>
              <p className="text-sm text-muted-foreground">
                Manage your password and security settings
              </p>
            </div>
          </div>
          <Button variant="outline" asChild>
            <a href="/account/">Manage Account</a>
          </Button>
        </div>
      </div>

      <div className="text-center text-sm text-muted-foreground pb-8">
        <p>uMail Server v1.0.0</p>
        <p className="mt-1">Built with care for your privacy</p>
      </div>
    </div>
  )
}
