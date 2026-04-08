import { useState } from "react"
import { Moon, Sun, Bell, Shield, Palette, Keyboard, Mail, Globe, Lock } from "lucide-react"
import { useTheme } from "@/components/theme-provider"
import { Button } from "@/components/ui/button"
import { Separator } from "@/components/ui/separator"
import { Switch } from "@/components/ui/switch"
import { toast } from "sonner"

export function SettingsPage() {
  const { theme, setTheme, resolvedTheme } = useTheme()
  const [settings, setSettings] = useState({
    // Notifications
    emailNotifications: true,
    browserNotifications: false,
    soundNotifications: true,
    desktopNotifications: true,
    // Email
    autoSaveDraft: true,
    readReceipts: false,
    deliveryReceipts: true,
    // Privacy
    showOnlineStatus: false,
    allowReadReceipts: true,
    // Composition
    richTextMode: true,
    autoCorrect: true,
    spellCheck: true,
  })

  const handleToggle = (key: keyof typeof settings) => {
    setSettings({ ...settings, [key]: !settings[key] })
    toast.success("Setting updated")
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
    onChange
  }: {
    title: string
    description: string
    checked: boolean
    onChange: () => void
  }) => (
    <div className="flex items-center justify-between py-3">
      <div>
        <p className="font-medium">{title}</p>
        <p className="text-sm text-muted-foreground">{description}</p>
      </div>
      <Switch checked={checked} onCheckedChange={onChange} />
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
        <div className="space-y-1">
          <SettingRow
            title="Email notifications"
            description="Receive notifications for new emails"
            checked={settings.emailNotifications}
            onChange={() => handleToggle("emailNotifications")}
          />
          <Separator />
          <SettingRow
            title="Browser notifications"
            description="Show notifications in your browser"
            checked={settings.browserNotifications}
            onChange={() => handleToggle("browserNotifications")}
          />
          <Separator />
          <SettingRow
            title="Sound notifications"
            description="Play a sound for new messages"
            checked={settings.soundNotifications}
            onChange={() => handleToggle("soundNotifications")}
          />
          <Separator />
          <SettingRow
            title="Desktop notifications"
            description="Show desktop notifications when app is in background"
            checked={settings.desktopNotifications}
            onChange={() => handleToggle("desktopNotifications")}
          />
        </div>
      </SettingSection>

      {/* Email Settings */}
      <SettingSection
        icon={Mail}
        title="Email Composition"
        description="Settings for composing and sending emails"
      >
        <div className="space-y-1">
          <SettingRow
            title="Auto-save drafts"
            description="Automatically save drafts while composing"
            checked={settings.autoSaveDraft}
            onChange={() => handleToggle("autoSaveDraft")}
          />
          <Separator />
          <SettingRow
            title="Rich text mode"
            description="Use rich text editor with formatting"
            checked={settings.richTextMode}
            onChange={() => handleToggle("richTextMode")}
          />
          <Separator />
          <SettingRow
            title="Auto-correct"
            description="Automatically correct spelling"
            checked={settings.autoCorrect}
            onChange={() => handleToggle("autoCorrect")}
          />
          <Separator />
          <SettingRow
            title="Spell check"
            description="Check spelling while typing"
            checked={settings.spellCheck}
            onChange={() => handleToggle("spellCheck")}
          />
        </div>
      </SettingSection>

      {/* Privacy & Security */}
      <SettingSection
        icon={Shield}
        title="Privacy & Security"
        description="Control your privacy and security settings"
      >
        <div className="space-y-1">
          <SettingRow
            title="Read receipts"
            description="Request read receipts for sent emails"
            checked={settings.readReceipts}
            onChange={() => handleToggle("readReceipts")}
          />
          <Separator />
          <SettingRow
            title="Delivery receipts"
            description="Request delivery confirmations for sent emails"
            checked={settings.deliveryReceipts}
            onChange={() => handleToggle("deliveryReceipts")}
          />
          <Separator />
          <SettingRow
            title="Show online status"
            description="Let others see when you're online"
            checked={settings.showOnlineStatus}
            onChange={() => handleToggle("showOnlineStatus")}
          />
          <Separator />
          <SettingRow
            title="Allow read receipts"
            description="Send read receipts when others request them"
            checked={settings.allowReadReceipts}
            onChange={() => handleToggle("allowReadReceipts")}
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
          <Button variant="outline">Manage Account</Button>
        </div>
      </div>

      <div className="text-center text-sm text-muted-foreground pb-8">
        <p>uMail Server v1.0.0</p>
        <p className="mt-1">Built with care for your privacy</p>
      </div>
    </div>
  )
}
