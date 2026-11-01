import { useEffect, useState, type ReactNode } from "react";
import { Settings, Shield, Bell, Server, Mail, AlertCircle, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useApi } from "@/hooks/useApi";
import type { ServerSettings } from "@/types";

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-4 py-2 border-b last:border-b-0">
      <span className="text-sm text-muted-foreground">{label}</span>
      <span className="text-sm font-medium text-right break-all">{children}</span>
    </div>
  );
}

function OnOff({ on }: { on: boolean }) {
  return <Badge variant={on ? "default" : "secondary"}>{on ? "On" : "Off"}</Badge>;
}

function formatBytes(n: number): string {
  if (n >= 1024 * 1024) return `${Math.round(n / (1024 * 1024))} MB`;
  if (n >= 1024) return `${Math.round(n / 1024)} KB`;
  return `${n} B`;
}

interface JwtStatus {
  currentKid?: string;
  activeKeys?: number;
}

// Session signing keys: rotating issues a new key; tokens signed with older
// keys stay valid until they expire (the server keeps the last few keys).
function SigningKeys() {
  const status = useApi<JwtStatus>();
  const rotate = useApi<unknown>();
  const [confirming, setConfirming] = useState(false);
  const [rotated, setRotated] = useState(false);
  const loadStatus = () => status.execute("/admin/jwt/status").catch(() => {
    // surfaced through status.error
  });

  useEffect(() => {
    loadStatus();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const doRotate = async () => {
    setConfirming(false);
    setRotated(false);
    try {
      await rotate.execute("/admin/jwt/rotate", { method: "POST" });
      setRotated(true);
      await loadStatus();
    } catch {
      // surfaced through rotate.error
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>Session signing keys</CardTitle>
        <CardDescription>
          Rotating issues a new signing key. Sessions signed with older keys stay valid until they expire.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        {status.error && (
          <Alert variant="destructive">
            <AlertCircle className="h-4 w-4" />
            <AlertDescription>Could not load key status: {status.error.message}</AlertDescription>
          </Alert>
        )}
        {rotate.error && (
          <Alert variant="destructive">
            <AlertCircle className="h-4 w-4" />
            <AlertDescription>Rotation failed: {rotate.error.message}</AlertDescription>
          </Alert>
        )}
        {rotated && !rotate.error && (
          <Alert>
            <AlertDescription>Signing key rotated.</AlertDescription>
          </Alert>
        )}
        {status.data?.currentKid && (
          <>
            <Row label="Current key">{status.data.currentKid}</Row>
            <Row label="Active keys">{status.data.activeKeys}</Row>
          </>
        )}
        {confirming ? (
          <div className="flex items-center gap-2">
            <span className="text-sm">Rotate the signing key now?</span>
            <Button size="sm" onClick={doRotate} disabled={rotate.loading}>Confirm rotate</Button>
            <Button size="sm" variant="outline" onClick={() => setConfirming(false)}>Cancel</Button>
          </div>
        ) : (
          <Button size="sm" variant="outline" onClick={() => setConfirming(true)} disabled={rotate.loading}>
            Rotate signing key
          </Button>
        )}
      </CardContent>
    </Card>
  );
}

const RATE_FIELDS: { key: string; label: string }[] = [
  { key: "ip_per_minute", label: "Inbound messages per IP per minute" },
  { key: "ip_per_hour", label: "Inbound messages per IP per hour" },
  { key: "ip_per_day", label: "Inbound messages per IP per day" },
  { key: "ip_connections", label: "Concurrent connections per IP" },
  { key: "user_per_minute", label: "Outbound messages per user per minute" },
  { key: "user_per_hour", label: "Outbound messages per user per hour" },
  { key: "user_per_day", label: "Outbound messages per user per day" },
  { key: "user_max_recipients", label: "Recipients per user message" },
  { key: "global_per_minute", label: "All messages per minute" },
  { key: "global_per_hour", label: "All messages per hour" },
];

// Live mail rate limits. Changes apply immediately but are held in memory
// only: the config file value returns after a restart.
function RateLimits() {
  const current = useApi<Record<string, number>>();
  const update = useApi<unknown>();
  const [draft, setDraft] = useState<Record<string, string>>({});
  const [applied, setApplied] = useState(false);
  const load = () =>
    current
      .execute("/admin/ratelimits/config")
      .then((cfg) => {
        const next: Record<string, string> = {};
        for (const f of RATE_FIELDS) next[f.key] = String(cfg?.[f.key] ?? "");
        setDraft(next);
      })
      .catch(() => {
        // surfaced through current.error
      });

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const apply = async () => {
    setApplied(false);
    const body: Record<string, number> = {};
    for (const f of RATE_FIELDS) body[f.key] = Number(draft[f.key]);
    try {
      await update.execute("/admin/ratelimits/config", { method: "PUT", body: JSON.stringify(body) });
      setApplied(true);
    } catch {
      // surfaced through update.error
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>Live rate limits</CardTitle>
        <CardDescription>
          Applies immediately but is not written to the config file, so a restart restores the configured values.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        {current.error && (
          <Alert variant="destructive">
            <AlertCircle className="h-4 w-4" />
            <AlertDescription>Could not load rate limits: {current.error.message}</AlertDescription>
          </Alert>
        )}
        {update.error && (
          <Alert variant="destructive">
            <AlertCircle className="h-4 w-4" />
            <AlertDescription>Update failed: {update.error.message}</AlertDescription>
          </Alert>
        )}
        {applied && !update.error && (
          <Alert>
            <AlertDescription>Rate limits updated.</AlertDescription>
          </Alert>
        )}
        {Object.keys(draft).length > 0 && (
          <>
            {RATE_FIELDS.map((f) => (
              <label key={f.key} className="flex items-center justify-between gap-4 text-sm">
                <span className="text-muted-foreground">{f.label}</span>
                <input
                  type="number"
                  min={1}
                  aria-label={f.label}
                  className="w-32 rounded-md border bg-background px-2 py-1 text-right"
                  value={draft[f.key]}
                  onChange={(e) => setDraft({ ...draft, [f.key]: e.target.value })}
                />
              </label>
            ))}
            <Button size="sm" onClick={apply} disabled={update.loading}>
              Apply limits
            </Button>
          </>
        )}
      </CardContent>
    </Card>
  );
}

export function SettingsPage() {
  // GET /api/v1/admin/settings serves the running configuration. Nothing
  // here is editable: settings change in the config file and apply on restart.
  const { data: settings, loading, error, execute } = useApi<ServerSettings>();
  const load = () => execute("/admin/settings").catch(() => {
    // surfaced through `error`
  });

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Settings</h1>
          <p className="text-muted-foreground mt-1">The configuration the server is running with</p>
        </div>
        <Button variant="outline" size="sm" onClick={load} disabled={loading}>
          <RefreshCw className={`h-4 w-4 mr-2 ${loading ? "animate-spin" : ""}`} />
          Refresh
        </Button>
      </div>

      <Alert>
        <AlertCircle className="h-4 w-4" />
        <AlertDescription>
          Settings are read-only here. To change them, edit{" "}
          <code>{settings?.config_file || "umailserver.yaml"}</code> and restart the server.
        </AlertDescription>
      </Alert>

      {error && (
        <Alert variant="destructive">
          <AlertCircle className="h-4 w-4" />
          <AlertDescription>Could not load settings: {error.message}</AlertDescription>
        </Alert>
      )}

      {settings && (
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
            <TabsTrigger value="filtering">
              <Mail className="h-4 w-4 mr-2" />
              Filtering
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
                  Server
                </CardTitle>
              </CardHeader>
              <CardContent>
                <Row label="Hostname">{settings.hostname}</Row>
                <Row label="Data directory">{settings.data_dir}</Row>
                <Row label="Config file">{settings.config_file || "built-in defaults"}</Row>
                <Row label="Max message size">{formatBytes(settings.max_message_size)}</Row>
                <Row label="Max recipients per message">{settings.max_recipients}</Row>
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle>Listeners</CardTitle>
                <CardDescription>Protocol ports as configured</CardDescription>
              </CardHeader>
              <CardContent>
                {settings.listeners.map((l) => (
                  <Row key={l.name} label={l.name}>
                    {l.enabled ? `${l.bind || "*"}:${l.port}` : <OnOff on={false} />}
                  </Row>
                ))}
              </CardContent>
            </Card>
          </TabsContent>

          <TabsContent value="security" className="space-y-6">
            <Card>
              <CardHeader>
                <CardTitle>TLS</CardTitle>
              </CardHeader>
              <CardContent>
                <Row label="Minimum TLS version">{settings.tls.min_version}</Row>
                <Row label="Let's Encrypt (ACME)"><OnOff on={settings.tls.acme} /></Row>
                <Row label="Certificate files"><OnOff on={settings.tls.manual_cert} /></Row>
                <Row label="Client certificates (mTLS)"><OnOff on={settings.tls.client_auth} /></Row>
                <Row label="Submission requires STARTTLS"><OnOff on={settings.security.submission_require_tls} /></Row>
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle>Login protection and rate limits</CardTitle>
              </CardHeader>
              <CardContent>
                <Row label="Failed logins before lockout">{settings.security.max_login_attempts}</Row>
                <Row label="Lockout duration">{settings.security.lockout_duration}</Row>
                <Row label="Inbound messages per IP per hour">{settings.security.ip_per_hour}</Row>
                <Row label="Outbound messages per user per hour">{settings.security.user_per_hour}</Row>
                <Row label="API requests per IP per minute">{settings.security.http_requests_per_minute}</Row>
                <Row label="Audit log"><OnOff on={settings.security.audit_log} /></Row>
              </CardContent>
            </Card>
            <RateLimits />
            <SigningKeys />
          </TabsContent>

          <TabsContent value="filtering" className="space-y-6">
            <Card>
              <CardHeader>
                <CardTitle>Spam and malware</CardTitle>
              </CardHeader>
              <CardContent>
                <Row label="Spam filtering"><OnOff on={settings.spam.enabled} /></Row>
                <Row label="Junk threshold">{settings.spam.junk_threshold}</Row>
                <Row label="Reject threshold">{settings.spam.reject_threshold}</Row>
                <Row label="Greylisting"><OnOff on={settings.spam.greylisting} /></Row>
                <Row label="Bayesian classifier"><OnOff on={settings.spam.bayesian} /></Row>
                <Row label="Learn from Junk moves"><OnOff on={settings.spam.auto_train} /></Row>
                <Row label="DNS blocklists">
                  {settings.spam.rbl_servers?.length ? settings.spam.rbl_servers.join(", ") : "none"}
                </Row>
                <Row label="Antivirus (ClamAV)"><OnOff on={settings.spam.antivirus} /></Row>
                <Row label="DMARC aggregate reports"><OnOff on={settings.spam.dmarc_reports} /></Row>
              </CardContent>
            </Card>
          </TabsContent>

          <TabsContent value="notifications" className="space-y-6">
            <Card>
              <CardHeader>
                <CardTitle>Notifications</CardTitle>
              </CardHeader>
              <CardContent>
                <Row label="Operator alerts"><OnOff on={settings.notifications.alerts} /></Row>
                <Row label="Alerts by email"><OnOff on={settings.notifications.alert_email} /></Row>
                <Row label="Alerts by webhook"><OnOff on={settings.notifications.alert_webhook} /></Row>
                <Row label="Browser push for users"><OnOff on={settings.notifications.push} /></Row>
              </CardContent>
            </Card>
          </TabsContent>
        </Tabs>
      )}
    </div>
  );
}
