import { useState } from "react"
import { Moon, Sun, Bell, Shield, Palette } from "lucide-react"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Separator } from "@/components/ui/separator"
import { Switch } from "@/components/ui/switch"

export function SettingsPage() {
  const [theme, setTheme] = useState<"light" | "dark" | "system">("system")
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
  }

  return (
    <div className="space-y-6 max-w-2xl">
      <div>
        <h2 className="text-2xl font-bold">Ayarlar</h2>
        <p className="text-muted-foreground">
          E-posta ayarlarınızı yönetin.
        </p>
      </div>

      {/* Theme */}
      <div className="rounded-lg border bg-card p-6">
        <div className="flex items-center gap-4">
          <div className="rounded-full bg-muted p-2">
            <Palette className="h-5 w-5" />
          </div>
          <div className="flex-1">
            <h3 className="font-semibold">Görünüm</h3>
            <p className="text-sm text-muted-foreground">
              Tema ve görsel tercihlerinizi seçin.
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
            <h3 className="font-semibold">Bildirimler</h3>
            <p className="text-sm text-muted-foreground">
              Yeni e-postalar için bildirim ayarları.
            </p>
          </div>
        </div>

        <div className="space-y-4">
          <div className="flex items-center justify-between">
            <div>
              <p className="font-medium">E-posta bildirimleri</p>
              <p className="text-sm text-muted-foreground">
                Yeni e-posta geldiğinde bildirim al.
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
              <p className="font-medium">Tarayıcı bildirimleri</p>
              <p className="text-sm text-muted-foreground">
                Tarayıcı üzerinden bildirim al.
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
              <p className="font-medium">Sesli bildirimler</p>
              <p className="text-sm text-muted-foreground">
                Yeni e-posta için ses çal.
              </p>
            </div>
            <Switch
              checked={settings.soundNotifications}
              onCheckedChange={() => handleToggle("soundNotifications")}
            />
          </div>
        </div>
      </div>

      {/* Privacy */}
      <div className="rounded-lg border bg-card p-6">
        <div className="flex items-center gap-4 mb-4">
          <div className="rounded-full bg-muted p-2">
            <Shield className="h-5 w-5" />
          </div>
          <div>
            <h3 className="font-semibold">Gizlilik ve Güvenlik</h3>
            <p className="text-sm text-muted-foreground">
              Okuma ve teslim raporları.
            </p>
          </div>
        </div>

        <div className="space-y-4">
          <div className="flex items-center justify-between">
            <div>
              <p className="font-medium">Otomatik taslak kaydetme</p>
              <p className="text-sm text-muted-foreground">
                Her 30 saniyede taslakları otomatik kaydet.
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
              <p className="font-medium">Okuma raporları</p>
              <p className="text-sm text-muted-foreground">
                Alıcı mesajı okuduğunda bilgi al.
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
              <p className="font-medium">Teslim raporları</p>
              <p className="text-sm text-muted-foreground">
                Mesaj teslim edildiğinde bilgi al.
              </p>
            </div>
            <Switch
              checked={settings.deliveryReceipts}
              onCheckedChange={() => handleToggle("deliveryReceipts")}
            />
          </div>
        </div>
      </div>

      {/* Account */}
      <div className="rounded-lg border bg-card p-6">
        <h3 className="font-semibold mb-4">Hesap</h3>
        <div className="space-y-2">
          <Button variant="outline" className="w-full justify-start">
            Profili Düzenle
          </Button>
          <Button variant="outline" className="w-full justify-start">
            Şifre Değiştir
          </Button>
          <Button variant="outline" className="w-full justify-start">
            İki Aşamalı Doğrulama
          </Button>
          <Separator />
          <Button variant="outline" className="w-full justify-start text-destructive">
            Oturumu Kapat
          </Button>
        </div>
      </div>
    </div>
  )
}
