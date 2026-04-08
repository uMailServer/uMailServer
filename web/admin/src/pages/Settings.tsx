import { useState } from "react";
import {
  Settings,
  Shield,
  Bell,
  Server,
  Database,
  Save,
  AlertCircle,
  Check,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Separator } from "@/components/ui/separator";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

export function SettingsPage() {
  const [saved, setSaved] = useState(false);
  const [error] = useState("");

  const handleSave = () => {
    setSaved(true);
    setTimeout(() => setSaved(false), 3000);
  };

  return (
    <div className="space-y-6">
      {/* Header */}
      <div>
        <h1 className="text-3xl font-bold tracking-tight">Settings</h1>
        <p className="text-muted-foreground mt-1">
          Configure your email server settings
        </p>
      </div>

      {saved && (
        <Alert className="bg-emerald-500/10 text-emerald-500 border-emerald-500/20">
          <Check className="h-4 w-4" />
          <AlertDescription>Settings saved successfully</AlertDescription>
        </Alert>
      )}

      {error && (
        <Alert variant="destructive">
          <AlertCircle className="h-4 w-4" />
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      <Tabs defaultValue="general" className="space-y-6">
        <TabsList>
          <TabsTrigger value="general">
            <Settings className="h-4 w-4 mr-2" />
            General
          </TabsTrigger>
          <TabsTrigger value="security">
            <Shield className="h-4 w-4 mr-2" />
            Security
          </TabsTrigger>
          <TabsTrigger value="notifications">
            <Bell className="h-4 w-4 mr-2" />
            Notifications
          </TabsTrigger>
        </TabsList>

        <TabsContent value="general" className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <Server className="h-5 w-5" />
                Server Configuration
              </CardTitle>
              <CardDescription>
                Basic server settings and hostname configuration
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="hostname">Server Hostname</Label>
                  <Input
                    id="hostname"
                    placeholder="mail.example.com"
                    defaultValue="mail.example.com"
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="data-dir">Data Directory</Label>
                  <Input
                    id="data-dir"
                    placeholder="/var/lib/umailserver"
                    defaultValue="./data"
                  />
                </div>
              </div>

              <Separator />

              <div className="space-y-4">
                <h4 className="text-sm font-medium">Port Configuration</h4>
                <div className="grid gap-4 sm:grid-cols-3">
                  <div className="space-y-2">
                    <Label htmlFor="smtp-port">SMTP Port</Label>
                    <Input
                      id="smtp-port"
                      type="number"
                      defaultValue={25}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="submission-port">Submission Port</Label>
                    <Input
                      id="submission-port"
                      type="number"
                      defaultValue={587}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="imap-port">IMAP Port</Label>
                    <Input
                      id="imap-port"
                      type="number"
                      defaultValue={993}
                    />
                  </div>
                </div>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <Database className="h-5 w-5" />
                Storage & Limits
              </CardTitle>
              <CardDescription>
                Configure storage limits and message handling
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="max-message-size">Max Message Size (MB)</Label>
                  <Input
                    id="max-message-size"
                    type="number"
                    defaultValue={50}
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="max-recipients">Max Recipients</Label>
                  <Input
                    id="max-recipients"
                    type="number"
                    defaultValue={100}
                  />
                </div>
              </div>
              <div className="flex items-center justify-between pt-2">
                <div className="space-y-0.5">
                  <Label>Enable Greylisting</Label>
                  <p className="text-xs text-muted-foreground">
                    Temporarily reject unknown senders to reduce spam
                  </p>
                </div>
                <Switch defaultChecked />
              </div>
            </CardContent>
          </Card>

          <div className="flex justify-end">
            <Button onClick={handleSave}>
              <Save className="mr-2 h-4 w-4" />
              Save Changes
            </Button>
          </div>
        </TabsContent>

        <TabsContent value="security" className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle>TLS & Encryption</CardTitle>
              <CardDescription>
                Configure TLS certificates and encryption settings
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label>Auto TLS (Let's Encrypt)</Label>
                  <p className="text-xs text-muted-foreground">
                    Automatically obtain and renew certificates
                  </p>
                </div>
                <Switch defaultChecked />
              </div>
              <Separator />
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label>Require TLS for SMTP</Label>
                  <p className="text-xs text-muted-foreground">
                    Only accept encrypted connections
                  </p>
                </div>
                <Switch defaultChecked />
              </div>
              <Separator />
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label>DKIM Signing</Label>
                  <p className="text-xs text-muted-foreground">
                    Sign outgoing emails with DKIM
                  </p>
                </div>
                <Switch defaultChecked />
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Rate Limiting</CardTitle>
              <CardDescription>
                Configure rate limits to prevent abuse
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="rate-limit">Max Emails per Hour</Label>
                  <Input
                    id="rate-limit"
                    type="number"
                    defaultValue={100}
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="auth-attempts">Max Auth Attempts</Label>
                  <Input
                    id="auth-attempts"
                    type="number"
                    defaultValue={5}
                  />
                </div>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="notifications" className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle>Email Notifications</CardTitle>
              <CardDescription>
                Configure when and how you receive notifications
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label>Queue Alerts</Label>
                  <p className="text-xs text-muted-foreground">
                    Notify when emails fail to send
                  </p>
                </div>
                <Switch defaultChecked />
              </div>
              <Separator />
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label>Security Alerts</Label>
                  <p className="text-xs text-muted-foreground">
                    Notify on suspicious login attempts
                  </p>
                </div>
                <Switch defaultChecked />
              </div>
              <Separator />
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label>Weekly Reports</Label>
                  <p className="text-xs text-muted-foreground">
                    Receive weekly email statistics
                  </p>
                </div>
                <Switch />
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  );
}
