import { useState } from "react"
import { Moon, Sun, Bell, Shield, Palette } from "lucide-react"
import { useTheme } from "@/components/theme-provider"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Separator } from "@/components/ui/separator"
import { Switch } from "@/components/ui/switch"
import { toast } from "sonner"

export function SettingsPage() {
  const { theme, setTheme, resolvedTheme } = useTheme()
  const [settings, setSettings] = useState({
    emailNotifications: true,
    browserNotifications: false,
    soundNotifications: true,
    desktopNotifications: true,
    autoSaveDraft: true,
    readReceipts: false,
    deliveryReceipts: false,
  })

  const handleToggle = (key: keyof typeof settings) => {
    setSettings({ ...settings, [key]: !settings[key] })
    toast.success("Setting updated")
  }

  return (
    <div className="space-y-6 max-w-2xl">
      <div>
        <h2 className="text-2xl font-bold">Settings</h2>
        <p className="text-muted-foreground">
          Manage your email preferences.
        </p>
      </div>

      {/* Theme */}
      <div className="rounded-lg border bg-card p-6">
        <div className="flex items-center gap-4">
          <div className="rounded-full bg-muted p-2">
            <Palette className="h-5 w-5" />
          </div>
          <div className="flex-1">
            <h3 className="font-semibold">Appearance</h3>
            <p className="text-sm text-muted-foreground">
              Choose your preferred theme.
            </p>
          </div>
          <div className="flex gap-2">
            <Button
              variant={theme === "light" ? "default" : "outline"}
              size="icon"
              onClick={() => setTheme("light")}
            >
              <Sun className="h-4 w-4" />
            </Button>
            <Button
              variant={theme === "dark" ? "default" : "outline"}
              size="icon"
              onClick={() => setTheme("dark")}
            >
              <Moon className="h-4 w-4" />
            </Button>
          </div>
        </div>
      </div>

      {/* Notifications */}
      <div className="rounded-lg border bg-card p-6">
        <div className="flex items-center gap-4 mb-4">
          <div className="rounded-full bg-muted p-2">
            <Bell className="h-5 w-5" />
          </div>
          <div>
            <h3 className="font-semibold">Notifications</h3>
            <p className="text-sm text-muted-foreground">
              Configure notification preferences for new emails.
            </p>
          </div>
        </div>

        <div className="space-y-4">
          <div className="flex items-center justify-between">
            <div>
              <p className="font-medium">Email notifications</p>
              <p className="text-sm text-muted-foreground">
                Receive notifications for new emails.
              </p>
            </div>
            <Switch
              checked={settings.emailNotifications}
              onCheckedChange={() => handleToggle("emailNotifications")}
            />
          </div>
          <Separator />
          <div className="flex items-center justify-between">
            <div>
              <p className="font-medium">Browser notifications</p>
              <p className="text-sm text-muted-foreground">
                Show notifications in your browser.
              </p>
            </div>
            <Switch
              checked={settings.browserNotifications}
              onCheckedChange={() => handleToggle("browserNotifications")}
            />
          </div>
          <Separator />
          <div className="flex items-center justify-between">
            <div>
              <p className="font-medium">Sound notifications</p>
              <p className="text-sm text-muted-foreground">
                Play a sound for new messages.
              </p>
            </div>
            <Switch
              checked={settings.soundNotifications}
              onCheckedChange={() => handleToggle("soundNotifications")}
            />
          </div>
          <Separator />
          <div className="flex items-center justify-between">
            <div>
              <p className="font-medium">Desktop notifications</p>
              <p className="text-sm text-muted-foreground">
                Show desktop notifications when the app is in background.
              </p>
            </div>
            <Switch
              checked={settings.desktopNotifications}
              onCheckedChange={() => handleToggle("desktopNotifications")}
            />
          </div>
        </div>
      </div>

      {/* Email Settings */}
      <div className="rounded-lg border bg-card p-6">
        <div className="flex items-center gap-4 mb-4">
          <div className="rounded-full bg-muted p-2">
            <Shield className="h-5 w-5" />
          </div>
          <div>
            <h3 className="font-semibold">Email Settings</h3>
            <p className="text-sm text-muted-foreground">
              Configure email behavior.
            </p>
          </div>
        </div>

        <div className="space-y-4">
          <div className="flex items-center justify-between">
            <div>
              <p className="font-medium">Auto-save drafts</p>
              <p className="text-sm text-muted-foreground">
                Automatically save drafts while composing.
              </p>
            </div>
            <Switch
              checked={settings.autoSaveDraft}
              onCheckedChange={() => handleToggle("autoSaveDraft")}
            />
          </div>
          <Separator />
          <div className="flex items-center justify-between">
            <div>
              <p className="font-medium">Read receipts</p>
              <p className="text-sm text-muted-foreground">
                Request read receipts for sent emails.
              </p>
            </div>
            <Switch
              checked={settings.readReceipts}
              onCheckedChange={() => handleToggle("readReceipts")}
            />
          </div>
          <Separator />
          <div className="flex items-center justify-between">
            <div>
              <p className="font-medium">Delivery receipts</p>
              <p className="text-sm text-muted-foreground">
                Request delivery confirmations for sent emails.
              </p>
            </div>
            <Switch
              checked={settings.deliveryReceipts}
              onCheckedChange={() => handleToggle("deliveryReceipts")}
            />
          </div>
        </div>
      </div>
    </div>
  )
}
